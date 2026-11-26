package automation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/models"

	"github.com/fsnotify/fsnotify"
)

var (
	errWatcherStopped = errors.New("automation: watcher stopped")
	errRunTimeout     = errors.New("automation: recipe run timed out")
)

// watcherQueueCapacity bounds the accepted files waiting behind the single
// per-rule worker. A full queue drops new paths instead of blocking fsnotify.
const watcherQueueCapacity = 64

// maxWatchedDirectories bounds how many directories one recursive rule
// watches, including the rule's own folder. inotify watches are a finite
// per-user resource, so a very deep tree is capped instead of exhausting the
// budget silently. Reaching the cap is logged once per rule and the deepest
// folders beyond it are simply not watched.
const maxWatchedDirectories = 256

// fileFingerprint identifies a file version by size and modification time. A
// source whose fingerprint changed during a run is never deleted.
type fileFingerprint struct {
	size       int64
	modifiedAt int64
}

func fingerprintFile(path string) (fileFingerprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFingerprint{}, err
	}

	return fingerprintInfo(info), nil
}

func fingerprintInfo(info os.FileInfo) fileFingerprint {
	return fileFingerprint{size: info.Size(), modifiedAt: info.ModTime().UnixNano()}
}

// queuedPath is one stable file waiting for the per-rule worker. The
// fingerprint captured when the stability window closed is carried along so an
// organize rule can prove the file did not change before it moves it.
type queuedPath struct {
	path           string
	fingerprint    fileFingerprint
	hasFingerprint bool
}

// ruleWatcher owns one fsnotify watcher for one enabled rule.
type ruleWatcher struct {
	service *Service
	rule    models.AutomationRule

	filter ruleFilter

	// maxDirs caps the watched directory count in recursive mode.
	maxDirs int

	watcher *fsnotify.Watcher
	ctx     context.Context
	cancel  context.CancelFunc

	mu           sync.Mutex
	timers       map[string]*time.Timer
	fingerprints map[string]fileFingerprint
	inFlight     map[string]struct{}
	produced     map[string]time.Time
	// watchedDirs tracks every directory this rule watches so the cap is
	// counted in one place and removed folders are forgotten.
	watchedDirs    map[string]struct{}
	capLogged      bool
	addErrorLogged bool
	stopping       bool
	dead           bool
	wg             sync.WaitGroup

	// slots caps this rule at one concurrent run.
	slots chan struct{}

	// queue carries accepted, stable paths to the single per-rule worker.
	queue chan queuedPath
}

func newRuleWatcher(service *Service, rule models.AutomationRule, parentCtx context.Context) (*ruleWatcher, error) {
	filter := newRuleFilter(rule)

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsWatcher.Add(rule.WatchDir); err != nil {
		_ = fsWatcher.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(parentCtx)

	watcher := &ruleWatcher{
		service:      service,
		rule:         rule,
		filter:       filter,
		maxDirs:      maxWatchedDirectories,
		watcher:      fsWatcher,
		ctx:          ctx,
		cancel:       cancel,
		timers:       make(map[string]*time.Timer),
		fingerprints: make(map[string]fileFingerprint),
		inFlight:     make(map[string]struct{}),
		produced:     make(map[string]time.Time),
		watchedDirs:  map[string]struct{}{rule.WatchDir: {}},
		slots:        make(chan struct{}, 1),
		queue:        make(chan queuedPath, watcherQueueCapacity),
	}

	if rule.Recursive {
		watcher.addDescendants(rule.WatchDir)
	}

	return watcher, nil
}

func (w *ruleWatcher) start() {
	w.spawn(w.loop)
	w.spawn(w.worker)
}

// worker drains the per-rule path queue sequentially so a burst of files never
// turns into one goroutine per file.
func (w *ruleWatcher) worker() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case queued := <-w.queue:
			w.process(queued)
		}
	}
}

// enqueue hands one stable path to the rule worker without blocking the
// fsnotify loop. A full queue drops the path; the next fsnotify event or a
// later retry can pick the file up again.
func (w *ruleWatcher) enqueue(path string) bool {
	return w.enqueueStable(queuedPath{path: path})
}

// enqueueStable carries the fingerprint captured when the stability window
// closed along with the path.
func (w *ruleWatcher) enqueueStable(queued queuedPath) bool {
	select {
	case <-w.ctx.Done():
		return false
	default:
	}

	select {
	case w.queue <- queued:
		return true
	default:
		log.Printf("automation.watcher.queue_full ruleId=%s path=%s capacity=%d", w.rule.ID, queued.path, cap(w.queue))
		return false
	}
}

// stop cancels the watcher, closes fsnotify, stops pending timers and waits
// for the run loop and any in-flight process goroutines until deadline.
func (w *ruleWatcher) stop(deadline time.Time) {
	w.mu.Lock()
	w.stopping = true
	timers := make([]*time.Timer, 0, len(w.timers))
	for _, timer := range w.timers {
		timers = append(timers, timer)
	}
	w.timers = make(map[string]*time.Timer)
	w.mu.Unlock()

	w.cancel()
	for _, timer := range timers {
		timer.Stop()
	}
	w.closeWatcher()

	waited := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(waited)
	}()

	timeout := time.Until(deadline)
	if timeout < 0 {
		timeout = 0
	}
	select {
	case <-waited:
	case <-time.After(timeout):
	}
}

func (w *ruleWatcher) closeWatcher() {
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
}

// spawn runs fn in a tracked goroutine unless the watcher is stopping. The
// mutex guard keeps WaitGroup.Add ordered against stop's Wait.
func (w *ruleWatcher) spawn(fn func()) bool {
	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		return false
	}
	w.wg.Add(1)
	w.mu.Unlock()

	go func() {
		defer w.wg.Done()
		fn()
	}()

	return true
}

func (w *ruleWatcher) loop() {
	defer w.closeWatcher()

	for {
		select {
		case <-w.ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				w.loopExited()
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				w.loopExited()
				return
			}
			if err == nil {
				continue
			}
			log.Printf("automation.watcher.error ruleId=%s dir=%s err=%v", w.rule.ID, w.rule.WatchDir, err)
			// fsnotify reports an unrecoverable backend problem: stop taking
			// events and surface the rule as degraded. Re-enabling the rule
			// installs a fresh watcher.
			w.markDead(fmt.Sprintf("The watch folder reported an error: %v", err))
			return
		}
	}
}

// loopExited marks the watcher degraded when fsnotify stops on its own, not
// because the service asked it to stop.
func (w *ruleWatcher) loopExited() {
	if w.ctx.Err() != nil {
		return
	}

	w.markDead("The watch folder stopped reporting changes.")
}

// markDead retires the watcher without waiting for the service. Pending timers
// are dropped and no new run goroutines are spawned.
func (w *ruleWatcher) markDead(reason string) {
	w.mu.Lock()
	w.dead = true
	w.stopping = true
	timers := make([]*time.Timer, 0, len(w.timers))
	for _, timer := range w.timers {
		timers = append(timers, timer)
	}
	w.timers = make(map[string]*time.Timer)
	w.mu.Unlock()

	for _, timer := range timers {
		timer.Stop()
	}
	// Cancel in-flight runs too: a dead fsnotify backend must not keep
	// processing files through an old watcher while a fresh one is installed.
	w.cancel()
	w.service.setRuntimeError(w.rule.ID, reason)
}

func (w *ruleWatcher) handleEvent(event fsnotify.Event) {
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		// A recursive rule watches folders that appear after it started, so a
		// created directory is adopted before the file path filter runs.
		if w.rule.Recursive && event.Has(fsnotify.Create) && w.adoptCreatedDirectory(event.Name) {
			return
		}
		w.handlePath(event.Name)

		return
	}

	if w.rule.Recursive && (event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)) {
		w.forgetDirectoryTree(filepath.Clean(event.Name))
	}
}

// adoptCreatedDirectory watches a directory that appeared after the rule
// started. It reports true when the event named a directory, so the caller can
// stop treating it as a candidate file.
//
// Directories that already contain files (for example a folder moved into the
// watch tree) are scanned and their files enter the normal stability window, so
// a rename-into-place never loses work.
func (w *ruleWatcher) adoptCreatedDirectory(rawPath string) bool {
	path := filepath.Clean(rawPath)
	if w.ctx.Err() != nil {
		return false
	}

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if w.shouldSkipDirectory(path) {
		return true
	}

	w.adoptDirectory(path)

	return true
}

// adoptDirectory watches dir, its descendants and any files already inside it.
func (w *ruleWatcher) adoptDirectory(dir string) {
	if !w.addDirectory(dir) {
		return
	}
	w.addDescendants(dir)
	w.scheduleExistingFiles(dir)
}

// addDescendants watches every eligible directory below root without
// scheduling files: startup must keep the old behavior of ignoring files that
// were already there.
func (w *ruleWatcher) addDescendants(root string) {
	if !w.rule.Recursive {
		return
	}

	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == root || !entry.IsDir() {
			return nil
		}
		if w.shouldSkipDirectory(path) {
			return filepath.SkipDir
		}
		if !w.addDirectory(path) {
			return filepath.SkipDir
		}

		return nil
	})
}

// scheduleExistingFiles walks a freshly adopted directory and sends its files
// through the normal event path. Directories beyond the cap are skipped so the
// watcher never schedules files it cannot watch.
func (w *ruleWatcher) scheduleExistingFiles(root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if w.shouldSkipDirectory(path) || !w.watchesDirectory(path) {
				return filepath.SkipDir
			}

			return nil
		}
		w.handlePath(path)

		return nil
	})
}

// addDirectory adds one fsnotify watch and records it. It enforces the
// per-rule directory cap and logs the first Add failure instead of failing the
// whole rule.
func (w *ruleWatcher) addDirectory(dir string) bool {
	w.mu.Lock()
	if w.stopping || w.watchedDirs == nil {
		w.mu.Unlock()
		return false
	}
	if _, exists := w.watchedDirs[dir]; exists {
		w.mu.Unlock()
		return true
	}
	if w.maxDirs > 0 && len(w.watchedDirs) >= w.maxDirs {
		logCap := !w.capLogged
		w.capLogged = true
		w.mu.Unlock()
		if logCap {
			log.Printf(
				"automation.watcher.directory_cap ruleId=%s dir=%s limit=%d: deeper folders are not watched",
				w.rule.ID,
				dir,
				w.maxDirs,
			)
		}

		return false
	}
	w.watchedDirs[dir] = struct{}{}
	w.mu.Unlock()

	if err := w.watcher.Add(dir); err != nil {
		w.mu.Lock()
		delete(w.watchedDirs, dir)
		logError := !w.addErrorLogged
		w.addErrorLogged = true
		w.mu.Unlock()
		if logError {
			log.Printf("automation.watcher.add_dir.failed ruleId=%s dir=%s err=%v", w.rule.ID, dir, err)
		}

		return false
	}

	return true
}

// forgetDirectoryTree drops bookkeeping for a removed or renamed directory
// subtree. fsnotify discards the kernel watches by itself; the map must follow
// so the cap counts real watches and a recreated folder is adopted again.
func (w *ruleWatcher) forgetDirectoryTree(root string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for dir := range w.watchedDirs {
		if dir == root || pathWithin(root, dir) {
			delete(w.watchedDirs, dir)
		}
	}
}

// shouldSkipDirectory rejects hidden/temp folders and the excluded subtree in
// recursive mode.
func (w *ruleWatcher) shouldSkipDirectory(path string) bool {
	return w.filter.skipsDirectory(path)
}

func (w *ruleWatcher) watchesDirectory(dir string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, exists := w.watchedDirs[dir]

	return exists
}

// watchedDirectoryCount reports how many directories this rule watches. It is
// primarily a test seam for the recursive cap.
func (w *ruleWatcher) watchedDirectoryCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.watchedDirs)
}

func (w *ruleWatcher) directoryCapReached() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.capLogged
}

func (w *ruleWatcher) handlePath(rawPath string) {
	if w.ctx.Err() != nil {
		return
	}

	path := filepath.Clean(rawPath)
	info, ok := w.acceptsFile(path)
	if !ok {
		return
	}
	if w.isProduced(path) {
		return
	}

	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		return
	}
	if _, busy := w.inFlight[path]; busy {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	w.schedule(path, info)
}

// acceptsFile applies the path, hidden/temp, extension, pattern and
// output-folder filters. Recursive rules accept any depth; other rules only
// accept direct children. It returns the stat info so the caller does not
// repeat the filesystem call.
func (w *ruleWatcher) acceptsFile(path string) (os.FileInfo, bool) {
	return w.filter.acceptsPath(path)
}

func (w *ruleWatcher) schedule(path string, info os.FileInfo) {
	fingerprint := fingerprintInfo(info)

	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		return
	}
	w.fingerprints[path] = fingerprint
	if timer, exists := w.timers[path]; exists {
		timer.Stop()
	}
	delay := w.service.stabilityDelay
	w.timers[path] = time.AfterFunc(delay, func() {
		w.onStable(path)
	})
	w.mu.Unlock()
}

// onStable runs when a file has not changed for the stability window. It
// reschedules while the size or modification time keep moving.
func (w *ruleWatcher) onStable(path string) {
	if w.ctx.Err() != nil {
		return
	}

	w.mu.Lock()
	delete(w.timers, path)
	if w.stopping {
		w.mu.Unlock()
		return
	}
	if _, busy := w.inFlight[path]; busy {
		w.mu.Unlock()
		return
	}
	expected, tracked := w.fingerprints[path]
	w.mu.Unlock()
	if !tracked {
		return
	}
	if w.isProduced(path) {
		return
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	current := fingerprintInfo(info)
	if current != expected {
		// More writes arrived between the event and the timer. Reset the
		// window with the newest fingerprint.
		w.mu.Lock()
		if !w.stopping {
			w.fingerprints[path] = current
			w.timers[path] = time.AfterFunc(w.service.stabilityDelay, func() {
				w.onStable(path)
			})
		}
		w.mu.Unlock()
		return
	}

	w.mu.Lock()
	if w.stopping {
		w.mu.Unlock()
		return
	}
	if _, busy := w.inFlight[path]; busy {
		w.mu.Unlock()
		return
	}
	w.inFlight[path] = struct{}{}
	delete(w.fingerprints, path)
	w.mu.Unlock()

	if !w.enqueueStable(queuedPath{path: path, fingerprint: current, hasFingerprint: true}) {
		w.endProcess(path)
	}
}

func (w *ruleWatcher) endProcess(path string) {
	w.mu.Lock()
	delete(w.inFlight, path)
	w.mu.Unlock()
}

func (w *ruleWatcher) process(queued queuedPath) {
	defer w.endProcess(queued.path)

	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	case <-w.ctx.Done():
		return
	}
	if w.ctx.Err() != nil {
		return
	}

	if w.rule.Action == models.AutomationActionOrganize {
		w.runOrganize(queued)

		return
	}

	w.runRecipe(queued.path)
}

// runOrganize moves one stable file to the destination the planner resolves.
// The safety order is: re-stat, plan with the same code the preview uses,
// re-stat immediately before the move, then move without ever overwriting or
// deleting the source before its copy is verified.
func (w *ruleWatcher) runOrganize(queued queuedPath) {
	path := queued.path

	baseline, err := fingerprintFile(path)
	if err != nil {
		w.finish(path, models.JobStatusFailed, "The file is no longer available.", nil)

		return
	}
	if queued.hasFingerprint && queued.fingerprint != baseline {
		w.finish(
			path,
			models.JobStatusPartialSuccess,
			"The file changed after the stability window, so it was not moved.",
			nil,
		)

		return
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		w.finish(path, models.JobStatusFailed, "The file is no longer available.", nil)

		return
	}

	planner, err := newOrganizePlanner(w.service, w.rule)
	if err != nil {
		w.finish(path, models.JobStatusFailed, err.Error(), nil)

		return
	}
	move := planner.planPath(path, info)
	if move.Action != models.OrganizeMoveAction {
		w.finish(path, models.JobStatusPartialSuccess, move.Reason, nil)

		return
	}

	w.service.emitRun(models.AutomationRunEvent{
		RuleID:   w.rule.ID,
		RuleName: w.rule.Name,
		FilePath: path,
		Status:   models.JobStatusRunning,
		Message:  fmt.Sprintf("Organizing %s", filepath.Base(path)),
		At:       time.Now().UnixMilli(),
	})

	// Re-stat immediately before the move: a file that changed after the
	// stability window is never moved.
	current, statErr := os.Stat(path)
	if statErr != nil || fingerprintInfo(current) != baseline {
		w.finish(
			path,
			models.JobStatusPartialSuccess,
			"The file changed while the rule was running, so it was not moved.",
			nil,
		)

		return
	}

	if err := w.service.moveOrganizedFile(path, move.Destination); err != nil {
		w.finish(path, models.JobStatusFailed, err.Error(), nil)

		return
	}

	w.service.recordOrganizeMove(w.rule, path, move.Destination)
	w.addProduced([]string{move.Destination})
	w.finish(
		path,
		models.JobStatusSuccess,
		fmt.Sprintf("Moved to %s.", move.Destination),
		[]string{move.Destination},
	)
}

func (w *ruleWatcher) runRecipe(path string) {
	before, err := fingerprintFile(path)
	if err != nil {
		w.finish(path, models.JobStatusFailed, "The file is no longer available.", nil)

		return
	}

	request := models.RecipeRequest{
		RecipeID:   w.rule.RecipeID,
		InputPaths: []string{path},
		OutputDir:  w.rule.OutputDir,
		Options:    w.rule.Options,
	}

	plan := w.service.recipes.Plan(request)
	if !plan.Success {
		message := strings.TrimSpace(plan.Message)
		if message == "" {
			message = "The recipe could not plan this file."
		}
		w.finish(path, models.JobStatusFailed, message, nil)

		return
	}

	w.addProduced(plannedOutputPaths(plan))
	w.service.emitRun(models.AutomationRunEvent{
		RuleID:   w.rule.ID,
		RuleName: w.rule.Name,
		FilePath: path,
		Status:   models.JobStatusRunning,
		Message:  fmt.Sprintf("Processing %s", filepath.Base(path)),
		At:       time.Now().UnixMilli(),
	})

	run := w.service.recipes.Run(w.ctx, request)
	if !run.Success {
		message := strings.TrimSpace(run.Message)
		if message == "" {
			message = "The recipe could not be started."
		}
		w.finish(path, models.JobStatusFailed, message, nil)

		return
	}

	result, waitErr := w.waitForTerminal(run.JobID)
	if waitErr != nil {
		switch {
		case errors.Is(waitErr, errWatcherStopped):
			// Shutdown or rule disable: never delete the source.
			w.finish(path, models.JobStatusCancelled, "Automation stopped before the recipe finished.", nil)
		case errors.Is(waitErr, errRunTimeout):
			w.finish(path, models.JobStatusFailed, "The recipe did not finish in time.", nil)
		default:
			w.finish(path, models.JobStatusFailed, waitErr.Error(), nil)
		}

		return
	}

	status := result.Status
	message := strings.TrimSpace(result.Message)
	outputs := append([]string{}, result.Outputs...)

	if status == models.JobStatusSuccess && w.rule.DeleteSource {
		if note := deleteSafely(path, before); note != "" {
			if message == "" {
				message = note
			} else {
				message = strings.TrimSpace(message + " " + note)
			}
		}
	}

	w.addProduced(outputs)
	w.finish(path, status, message, outputs)
}

func (w *ruleWatcher) waitForTerminal(jobID string) (*models.RecipeResult, error) {
	timeout := w.service.runTimeout
	if timeout <= 0 {
		timeout = defaultRunTimeout
	}
	deadline := time.Now().Add(timeout)

	pollInterval := w.service.statusPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultStatusPollInterval
	}

	for {
		select {
		case <-w.ctx.Done():
			w.cancelRecipeRun(jobID)
			return nil, errWatcherStopped
		default:
		}

		if time.Now().After(deadline) {
			w.cancelRecipeRun(jobID)
			return nil, errRunTimeout
		}

		status := w.service.recipes.Status(jobID)
		if !status.Found {
			return nil, errors.New("The recipe job could not be found.")
		}
		if status.Result != nil && isTerminalStatus(status.Result.Status) {
			result := *status.Result

			return &result, nil
		}

		select {
		case <-w.ctx.Done():
			w.cancelRecipeRun(jobID)
			return nil, errWatcherStopped
		case <-time.After(pollInterval):
		}
	}
}

// cancelRecipeRun asks the recipe service to stop a run the watcher is giving
// up on, so a timed-out or disabled automation does not keep consuming engine
// capacity in the background. Failures are logged and ignored.
func (w *ruleWatcher) cancelRecipeRun(jobID string) {
	if w.service.recipes == nil || strings.TrimSpace(jobID) == "" {
		return
	}

	response := w.service.recipes.Cancel(jobID)
	if !response.Success {
		log.Printf("automation.run.cancel.failed ruleId=%s jobId=%s message=%s", w.rule.ID, jobID, response.Message)
	}
}

func (w *ruleWatcher) finish(path, status, message string, outputs []string) {
	w.service.recordRun(w.rule.ID, status, message, path, outputs)
	w.service.emitRun(models.AutomationRunEvent{
		RuleID:   w.rule.ID,
		RuleName: w.rule.Name,
		FilePath: path,
		Status:   status,
		Message:  message,
		Outputs:  outputs,
		At:       time.Now().UnixMilli(),
	})
}

func (w *ruleWatcher) isProduced(path string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	at, exists := w.produced[path]
	if !exists {
		return false
	}

	ttl := w.service.producedTTL
	if ttl <= 0 {
		ttl = defaultProducedTTL
	}
	if time.Since(at) > ttl {
		delete(w.produced, path)

		return false
	}

	return true
}

func (w *ruleWatcher) addProduced(paths []string) {
	now := time.Now()
	ttl := w.service.producedTTL
	if ttl <= 0 {
		ttl = defaultProducedTTL
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	for path, at := range w.produced {
		if now.Sub(at) > ttl {
			delete(w.produced, path)
		}
	}

	for _, raw := range paths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		w.produced[filepath.Clean(trimmed)] = now
	}
}

func plannedOutputPaths(plan models.RecipePlanResponse) []string {
	paths := make([]string, 0, len(plan.Outputs))
	for _, output := range plan.Outputs {
		if trimmed := strings.TrimSpace(output.OutputPath); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}

	return paths
}

// deleteSafely removes the watched source only when its fingerprint still
// matches the version that was planned. The returned note is appended to the
// run message when the source is kept.
func deleteSafely(path string, before fileFingerprint) string {
	info, err := os.Stat(path)
	if err != nil {
		return "The source file was already gone."
	}
	if fingerprintInfo(info) != before {
		return "The source file changed during the run, so it was kept."
	}
	if err := os.Remove(path); err != nil {
		return fmt.Sprintf("The source file could not be deleted: %v", err)
	}

	return ""
}

func isTerminalStatus(status string) bool {
	switch status {
	case models.JobStatusSuccess,
		models.JobStatusPartialSuccess,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusInterrupted:
		return true
	default:
		return false
	}
}

// isIgnoredName reports hidden and in-progress file names that must never
// start a run.
func isIgnoredName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
		return true
	}
	if strings.HasSuffix(name, "~") {
		return true
	}

	lower := strings.ToLower(name)
	for _, suffix := range []string{".tmp", ".part", ".crdownload"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}

	return false
}

// hasIgnoredSegment reports whether any path segment of a relative path is
// hidden or an in-progress name, so a recursive rule never processes files in
// a .git folder or inside a temporary directory.
func hasIgnoredSegment(relative string) bool {
	for _, segment := range strings.Split(relative, string(filepath.Separator)) {
		if isIgnoredName(segment) {
			return true
		}
	}

	return false
}

func pathWithin(base, target string) bool {
	relative, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}

	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (w *ruleWatcher) isDead() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.dead
}
