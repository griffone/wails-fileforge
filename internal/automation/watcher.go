package automation

import (
	"context"
	"errors"
	"fmt"
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

// ruleWatcher owns one fsnotify watcher for one enabled rule.
type ruleWatcher struct {
	service *Service
	rule    models.AutomationRuleV1

	extensions map[string]struct{}
	// excludeOutputDir is true when the rule writes into a subfolder of the
	// watched folder; events from that subtree are ignored.
	excludeOutputDir bool

	watcher *fsnotify.Watcher
	ctx     context.Context
	cancel  context.CancelFunc

	mu           sync.Mutex
	timers       map[string]*time.Timer
	fingerprints map[string]fileFingerprint
	inFlight     map[string]struct{}
	produced     map[string]time.Time
	stopping     bool
	dead         bool
	wg           sync.WaitGroup

	// slots caps this rule at one concurrent recipe run.
	slots chan struct{}

	// queue carries accepted, stable paths to the single per-rule worker.
	queue chan string
}

func newRuleWatcher(service *Service, rule models.AutomationRuleV1, parentCtx context.Context) (*ruleWatcher, error) {
	extensions := make(map[string]struct{}, len(rule.Extensions))
	for _, extension := range rule.Extensions {
		if normalized := normalizeExtension(extension); normalized != "" {
			extensions[normalized] = struct{}{}
		}
	}

	excludeOutputDir := rule.OutputDir != "" &&
		rule.OutputDir != rule.WatchDir &&
		pathWithin(rule.WatchDir, rule.OutputDir)

	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsWatcher.Add(rule.WatchDir); err != nil {
		_ = fsWatcher.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(parentCtx)

	return &ruleWatcher{
		service:          service,
		rule:             rule,
		extensions:       extensions,
		excludeOutputDir: excludeOutputDir,
		watcher:          fsWatcher,
		ctx:              ctx,
		cancel:           cancel,
		timers:           make(map[string]*time.Timer),
		fingerprints:     make(map[string]fileFingerprint),
		inFlight:         make(map[string]struct{}),
		produced:         make(map[string]time.Time),
		slots:            make(chan struct{}, 1),
		queue:            make(chan string, watcherQueueCapacity),
	}, nil
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
		case path := <-w.queue:
			w.process(path)
		}
	}
}

// enqueue hands one stable path to the rule worker without blocking the
// fsnotify loop. A full queue drops the path; the next fsnotify event or a
// later retry can pick the file up again.
func (w *ruleWatcher) enqueue(path string) bool {
	select {
	case <-w.ctx.Done():
		return false
	default:
	}

	select {
	case w.queue <- path:
		return true
	default:
		log.Printf("automation.watcher.queue_full ruleId=%s path=%s capacity=%d", w.rule.ID, path, cap(w.queue))
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
	if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) {
		return
	}

	w.handlePath(event.Name)
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

// acceptsFile applies the non-recursive, hidden/temp, extension and
// output-folder filters. It returns the stat info so the caller does not repeat
// the filesystem call.
func (w *ruleWatcher) acceptsFile(path string) (os.FileInfo, bool) {
	relative, err := filepath.Rel(w.rule.WatchDir, path)
	if err != nil {
		return nil, false
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, false
	}
	// Non-recursive: only direct children of the watched folder are processed.
	if strings.ContainsRune(relative, filepath.Separator) {
		return nil, false
	}
	if isIgnoredName(relative) {
		return nil, false
	}

	if w.excludeOutputDir && pathWithin(w.rule.OutputDir, path) {
		return nil, false
	}

	extension := normalizeExtension(filepath.Ext(relative))
	if extension == "" {
		return nil, false
	}
	if _, accepted := w.extensions[extension]; !accepted {
		return nil, false
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}

	return info, true
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

	if !w.enqueue(path) {
		w.endProcess(path)
	}
}

func (w *ruleWatcher) endProcess(path string) {
	w.mu.Lock()
	delete(w.inFlight, path)
	w.mu.Unlock()
}

func (w *ruleWatcher) process(path string) {
	defer w.endProcess(path)

	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	case <-w.ctx.Done():
		return
	}
	if w.ctx.Err() != nil {
		return
	}

	w.runRecipe(path)
}

func (w *ruleWatcher) runRecipe(path string) {
	before, err := fingerprintFile(path)
	if err != nil {
		w.finish(path, models.JobStatusFailed, "The file is no longer available.", nil)

		return
	}

	request := models.RecipeRequestV1{
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
	w.service.emitRun(models.AutomationRunEventV1{
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

func (w *ruleWatcher) waitForTerminal(jobID string) (*models.RecipeResultV1, error) {
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
	w.service.emitRun(models.AutomationRunEventV1{
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

func plannedOutputPaths(plan models.RecipePlanResponseV1) []string {
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
