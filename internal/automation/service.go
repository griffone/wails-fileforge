// Package automation runs recipes automatically for files that land in a
// watched folder. Rules are persisted by Store; Service owns the fsnotify
// watchers, debounces file events, prevents output loops and records the last
// run of every rule.
package automation

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	// RunEventName is the Wails event emitted for every automation run stage.
	RunEventName = "automation/run"

	// AutomationRunEventName is kept as an explicit alias for the event name.
	AutomationRunEventName = RunEventName

	// Runtime states reported by ListRules. They describe the watcher, not the
	// persisted rule.
	RuntimeStatusStopped  = "stopped"
	RuntimeStatusWatching = "watching"
	RuntimeStatusDegraded = "degraded"

	defaultStabilityDelay     = time.Second
	defaultStatusPollInterval = 500 * time.Millisecond
	defaultRunTimeout         = 30 * time.Minute
	defaultProducedTTL        = 5 * time.Minute
	defaultShutdownWait       = 3 * time.Second
)

// RecipeRunner is the recipe surface the automation service depends on.
// *recipes.Service satisfies it.
type RecipeRunner interface {
	List() []models.Recipe
	Plan(models.RecipeRequest) models.RecipePlanResponse
	Run(context.Context, models.RecipeRequest) models.RunRecipeResponse
	Status(string) models.RecipeStatusResponse
	Cancel(string) models.RecipeStatusResponse
}

// MoveRecorder records organize moves in the shared undo history.
// *jobhistory.Store satisfies it.
type MoveRecorder interface {
	RecordMoves(jobID, toolID string, moves []jobhistory.MoveRecord)
}

// Service owns the automation rules and their watchers.
type Service struct {
	store   *Store
	runs    *RunStore
	recipes RecipeRunner
	emit    func(models.AutomationRunEvent)

	// moveHistory records every successful organize move so the existing Undo
	// and Restore path can bring the file back. It may be nil in tests.
	moveHistory MoveRecorder

	// titleFor resolves {title}; it is a test seam around outputname.Suggest.
	titleFor func(path string) string
	// mover is the filesystem seam for organize moves.
	mover organizeMover

	// Test-settable timings.
	stabilityDelay     time.Duration
	statusPollInterval time.Duration
	runTimeout         time.Duration
	producedTTL        time.Duration

	mu            sync.Mutex
	started       bool
	ctx           context.Context
	cancel        context.CancelFunc
	watchers      map[string]*ruleWatcher
	runtimeErrors map[string]string
}

// NewService builds the automation service. emit may be nil in tests or
// headless runs.
func NewService(store *Store, recipes RecipeRunner, emit func(models.AutomationRunEvent)) *Service {
	service := &Service{
		store:              store,
		recipes:            recipes,
		emit:               emit,
		titleFor:           defaultOrganizeTitle,
		mover:              defaultOrganizeMover(),
		stabilityDelay:     defaultStabilityDelay,
		statusPollInterval: defaultStatusPollInterval,
		runTimeout:         defaultRunTimeout,
		producedTTL:        defaultProducedTTL,
		watchers:           make(map[string]*ruleWatcher),
		runtimeErrors:      make(map[string]string),
	}

	return service
}

// SetMoveHistory attaches the undo history used by organize rules. It is called
// once during app wiring, before Start.
func (s *Service) SetMoveHistory(history MoveRecorder) {
	s.moveHistory = history
}

// SetRunStore attaches the optional terminal-run history. It is called once
// during app wiring, before Start.
func (s *Service) SetRunStore(runs *RunStore) {
	s.runs = runs
}

// Start begins watching every enabled rule. It is idempotent and never fails:
// a broken rule is reported as degraded instead of aborting app startup.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started = true
	s.watchers = make(map[string]*ruleWatcher)
	s.runtimeErrors = make(map[string]string)
	s.mu.Unlock()

	if s.store == nil {
		return
	}

	for _, rule := range s.store.List() {
		if rule.Enabled {
			s.startRule(rule)
		}
	}
}

// Shutdown cancels in-flight work, stops every watcher and clears the runtime
// state. It waits up to a few seconds for run loops to observe the cancel.
func (s *Service) Shutdown() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	s.started = false
	cancel := s.cancel
	s.cancel = nil
	s.ctx = nil
	watchers := make([]*ruleWatcher, 0, len(s.watchers))
	for _, watcher := range s.watchers {
		watchers = append(watchers, watcher)
	}
	s.watchers = make(map[string]*ruleWatcher)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	deadline := time.Now().Add(defaultShutdownWait)
	for _, watcher := range watchers {
		watcher.stop(deadline)
	}

	s.mu.Lock()
	s.runtimeErrors = make(map[string]string)
	s.mu.Unlock()
}

// ListRules returns every saved rule with its current runtime state.
func (s *Service) ListRules() models.ListAutomationRulesResponse {
	rules := []models.AutomationRule{}
	if s.store != nil {
		rules = s.store.List()
	}

	s.mu.Lock()
	for index := range rules {
		rule := &rules[index]
		rule.RuntimeStatus = RuntimeStatusStopped
		rule.RuntimeReason = ""
		if !rule.Enabled {
			continue
		}
		if s.watchers[rule.ID] != nil && !s.watchers[rule.ID].isDead() {
			rule.RuntimeStatus = RuntimeStatusWatching
			continue
		}
		if reason := s.runtimeErrors[rule.ID]; reason != "" {
			rule.RuntimeStatus = RuntimeStatusDegraded
			rule.RuntimeReason = reason
		}
	}
	s.mu.Unlock()

	message := "Automation rules loaded."
	if len(rules) == 0 {
		message = "No automation rules yet."
	}

	return models.ListAutomationRulesResponse{
		Success: true,
		Message: message,
		Rules:   rules,
	}
}

// SaveRule validates, normalizes and persists a rule. When the service is
// running the watcher is restarted to match the new state.
func (s *Service) SaveRule(rule models.AutomationRule) models.AutomationRuleActionResponse {
	if s.store == nil {
		return s.failure("Automation rules are not available.", "AUTOMATION_STORE_UNAVAILABLE", nil)
	}

	normalized, jobErr := s.validateRule(rule)
	if jobErr != nil {
		return models.AutomationRuleActionResponse{
			Success: false,
			Message: jobErr.Message,
			Error:   jobErr,
		}
	}

	now := time.Now()
	var existing models.AutomationRule
	existingFound := false
	if normalized.ID == "" {
		normalized.ID = fmt.Sprintf("rule_%d", now.UnixNano())
		normalized.CreatedAt = now.UnixMilli()
	} else {
		existing, existingFound = s.store.Get(normalized.ID)
		if !existingFound {
			return s.failure(
				fmt.Sprintf("Automation rule '%s' was not found.", normalized.ID),
				"AUTOMATION_RULE_NOT_FOUND",
				nil,
			)
		}
		normalized.CreatedAt = existing.CreatedAt
		if normalized.LastRunAt == 0 && existing.LastRunAt > 0 {
			normalized.LastRunAt = existing.LastRunAt
			normalized.LastStatus = existing.LastStatus
			normalized.LastMessage = existing.LastMessage
			normalized.LastFile = existing.LastFile
			normalized.LastOutputs = existing.LastOutputs
		}
	}
	if normalized.CreatedAt <= 0 {
		normalized.CreatedAt = now.UnixMilli()
	}

	// A preview hash is server-owned. The client value is discarded and the
	// stored one is carried over only when the organize configuration did not
	// change, so editing any relevant field always invalidates the preview.
	normalized.PreviewHash = ""
	normalized.PreviewedAt = 0
	if existingFound && normalized.Action == models.AutomationActionOrganize &&
		strings.TrimSpace(existing.PreviewHash) != "" &&
		organizeConfigHash(existing) == organizeConfigHash(normalized) {
		normalized.PreviewHash = existing.PreviewHash
		normalized.PreviewedAt = existing.PreviewedAt
	}

	previewGated := false
	if normalized.Action == models.AutomationActionOrganize &&
		normalized.Enabled &&
		!organizePreviewCurrent(normalized) {
		// Never start a watcher that moves files without a preview of this
		// exact configuration, even when the client asked to enable it.
		normalized.Enabled = false
		previewGated = true
	}

	normalized.UpdatedAt = now.UnixMilli()

	saved, err := s.store.Save(normalized)
	if err != nil {
		log.Printf("automation.rule.save.failed ruleId=%s err=%v", normalized.ID, err)
		return s.failure(
			"The automation rule could not be saved.",
			"AUTOMATION_RULE_SAVE_FAILED",
			map[string]any{"reason": err.Error()},
		)
	}

	if s.isStarted() {
		s.stopRule(saved.ID)
		if saved.Enabled {
			s.startRule(saved)
		}
	}

	view := s.viewOf(saved)
	message := "Automation rule saved."
	if previewGated {
		message = "Automation rule saved. It stays disabled until you preview the current settings."
	}

	return models.AutomationRuleActionResponse{
		Success: true,
		Message: message,
		Rule:    &view,
	}
}

// DeleteRule stops the watcher and removes the rule from the store.
func (s *Service) DeleteRule(id string) models.AutomationRuleActionResponse {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return s.failure("Choose an automation rule to delete.", "AUTOMATION_RULE_ID_REQUIRED", nil)
	}
	if s.store == nil {
		return s.failure("Automation rules are not available.", "AUTOMATION_STORE_UNAVAILABLE", nil)
	}
	if _, found := s.store.Get(trimmed); !found {
		return s.failure(
			fmt.Sprintf("Automation rule '%s' was not found.", trimmed),
			"AUTOMATION_RULE_NOT_FOUND",
			nil,
		)
	}

	s.stopRule(trimmed)
	if err := s.store.Delete(trimmed); err != nil {
		if err == ErrRuleNotFound {
			return s.failure(
				fmt.Sprintf("Automation rule '%s' was not found.", trimmed),
				"AUTOMATION_RULE_NOT_FOUND",
				nil,
			)
		}
		log.Printf("automation.rule.delete.failed ruleId=%s err=%v", trimmed, err)
		return s.failure(
			"The automation rule could not be deleted.",
			"AUTOMATION_RULE_DELETE_FAILED",
			map[string]any{"reason": err.Error()},
		)
	}
	s.clearRuntimeError(trimmed)

	return models.AutomationRuleActionResponse{
		Success: true,
		Message: "Automation rule deleted.",
	}
}

// SetRuleEnabled enables or disables a rule and starts or stops its watcher.
func (s *Service) SetRuleEnabled(id string, enabled bool) models.AutomationRuleActionResponse {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return s.failure("Choose an automation rule.", "AUTOMATION_RULE_ID_REQUIRED", nil)
	}
	if s.store == nil {
		return s.failure("Automation rules are not available.", "AUTOMATION_STORE_UNAVAILABLE", nil)
	}

	rule, found := s.store.Get(trimmed)
	if !found {
		return s.failure(
			fmt.Sprintf("Automation rule '%s' was not found.", trimmed),
			"AUTOMATION_RULE_NOT_FOUND",
			nil,
		)
	}

	if enabled && rule.Action == models.AutomationActionOrganize && !organizePreviewCurrent(rule) {
		return s.failure(
			"Run a preview of the current settings before enabling this rule; enabling an organize rule without previewing could move files into the wrong place.",
			"AUTOMATION_RULE_PREVIEW_REQUIRED",
			map[string]any{"ruleId": rule.ID},
		)
	}

	if rule.Enabled != enabled {
		if err := s.store.SetEnabled(trimmed, enabled); err != nil {
			log.Printf("automation.rule.enabled.failed ruleId=%s enabled=%t err=%v", trimmed, enabled, err)
			return s.failure(
				"The automation rule could not be updated.",
				"AUTOMATION_RULE_UPDATE_FAILED",
				map[string]any{"reason": err.Error()},
			)
		}
		rule.Enabled = enabled
	}

	if s.isStarted() {
		if enabled {
			s.startRule(rule)
		} else {
			s.stopRule(trimmed)
		}
	}

	view := s.viewOf(rule)
	message := "Automation rule disabled."
	if enabled {
		message = "Automation rule enabled."
	}

	return models.AutomationRuleActionResponse{
		Success: true,
		Message: message,
		Rule:    &view,
	}
}

// Status returns the compact automation summary used by Home.
func (s *Service) Status() models.AutomationStatusResponse {
	rules := []models.AutomationRule{}
	if s.store != nil {
		rules = s.store.List()
	}

	s.mu.Lock()
	active := 0
	for _, watcher := range s.watchers {
		if watcher != nil && !watcher.isDead() {
			active++
		}
	}
	s.mu.Unlock()

	response := models.AutomationStatusResponse{
		Success:     true,
		Message:     "Automation status loaded.",
		TotalRules:  len(rules),
		ActiveRules: active,
	}
	if len(rules) == 0 {
		response.Message = "No automation rules yet."

		return response
	}

	latest := rules[0]
	for _, rule := range rules[1:] {
		if rule.LastRunAt > latest.LastRunAt {
			latest = rule
		}
	}
	response.LastRunAt = latest.LastRunAt
	response.LastStatus = latest.LastStatus
	response.LastMessage = latest.LastMessage
	response.LastFile = latest.LastFile

	return response
}

// ListRuns returns the persisted terminal run history, newest first. An empty
// history is a successful empty response; a missing run store is reported as a
// failure so the UI can explain why the panel is unavailable.
func (s *Service) ListRuns(limit int) models.ListAutomationRunsResponse {
	if s.runs == nil {
		return models.ListAutomationRunsResponse{
			Success: false,
			Message: "Automation run history is not available.",
		}
	}

	runs := s.runs.List(limit)
	message := "Automation runs loaded."
	if len(runs) == 0 {
		message = "No automation runs yet."
	}

	return models.ListAutomationRunsResponse{
		Success: true,
		Message: message,
		Runs:    runs,
	}
}

// ClearRuns permanently removes the persisted run history.
func (s *Service) ClearRuns() models.AutomationRunsActionResponse {
	if s.runs == nil {
		return models.AutomationRunsActionResponse{
			Success: false,
			Message: "Automation run history is not available.",
		}
	}

	if err := s.runs.Clear(); err != nil {
		log.Printf("automation.runs.clear.failed err=%v", err)
		return models.AutomationRunsActionResponse{
			Success: false,
			Message: "The automation run history could not be cleared.",
		}
	}

	return models.AutomationRunsActionResponse{
		Success: true,
		Message: "Automation run history cleared.",
	}
}

func (s *Service) validateRule(rule models.AutomationRule) (models.AutomationRule, *models.JobError) {
	return s.validateRuleWithProbe(rule, true)
}

// validateRuleWithProbe validates a rule. probe is false for a dry run, which
// must not create even a temporary writability file.
func (s *Service) validateRuleWithProbe(rule models.AutomationRule, probe bool) (models.AutomationRule, *models.JobError) {
	normalized := cloneRule(rule)
	normalized.ID = strings.TrimSpace(normalized.ID)
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.RecipeID = strings.TrimSpace(normalized.RecipeID)
	normalized.RuntimeStatus = ""
	normalized.RuntimeReason = ""

	if normalized.Name == "" {
		return normalized, automationError("AUTOMATION_RULE_NAME_REQUIRED", "Give the automation rule a name.")
	}

	action, actionErr := normalizeRuleAction(normalized.Action)
	if actionErr != nil {
		return normalized, actionErr
	}
	normalized.Action = action

	watchDir, jobErr := resolveExistingDirectory(normalized.WatchDir, "watch folder")
	if jobErr != nil {
		return normalized, jobErr
	}
	normalized.WatchDir = watchDir

	if normalized.Action == models.AutomationActionOrganize {
		return s.validateOrganizeRule(normalized, probe)
	}

	if s.recipes == nil {
		return normalized, automationError("AUTOMATION_RECIPE_UNAVAILABLE", "Recipes are not available.")
	}
	recipe := findRecipe(s.recipes.List(), normalized.RecipeID)
	if recipe == nil {
		message := "Choose a recipe for the automation rule."
		if normalized.RecipeID != "" {
			message = fmt.Sprintf("Recipe '%s' was not found.", normalized.RecipeID)
		}
		return normalized, automationError("AUTOMATION_RECIPE_NOT_FOUND", message)
	}

	accepted := normalizeExtensions(recipe.AcceptExtensions)
	requested := normalizeExtensions(normalized.Extensions)
	if len(requested) == 0 {
		normalized.Extensions = accepted
	} else {
		acceptedSet := make(map[string]struct{}, len(accepted))
		for _, extension := range accepted {
			acceptedSet[extension] = struct{}{}
		}
		for _, extension := range requested {
			if _, ok := acceptedSet[extension]; !ok {
				return normalized, automationError(
					"AUTOMATION_RULE_EXTENSION_UNSUPPORTED",
					fmt.Sprintf("The %s recipe does not accept .%s files.", recipe.Name, extension),
				)
			}
		}
		normalized.Extensions = requested
	}

	patterns, patternErr := normalizePatterns(normalized.Patterns)
	if patternErr != nil {
		return normalized, patternErr
	}
	normalized.Patterns = patterns

	if strings.TrimSpace(normalized.OutputDir) != "" {
		outputDir, dirErr := resolveExistingDirectory(normalized.OutputDir, "output folder")
		if dirErr != nil {
			return normalized, dirErr
		}
		if probe {
			if err := probeWritableDirectory(outputDir); err != nil {
				return normalized, automationError(
					"AUTOMATION_RULE_OUTPUT_NOT_WRITABLE",
					fmt.Sprintf("The output folder is not writable: %v", err),
				)
			}
		}
		normalized.OutputDir = outputDir
	} else {
		normalized.OutputDir = ""
	}

	return normalized, nil
}

// validateOrganizeRule validates the move configuration. The safety rules that
// execution depends on are enforced here, at save time: the target folder can
// never be the watch folder or live inside it, so loop protection does not
// rely on the watcher noticing its own moves.
func (s *Service) validateOrganizeRule(normalized models.AutomationRule, probe bool) (models.AutomationRule, *models.JobError) {
	normalized.Extensions = normalizeExtensions(normalized.Extensions)
	// An organize rule never writes a processed copy, so a leftover output
	// folder from a recipe rule would only act as a hidden file filter.
	normalized.OutputDir = ""

	patterns, patternErr := normalizePatterns(normalized.Patterns)
	if patternErr != nil {
		return normalized, patternErr
	}
	normalized.Patterns = patterns

	targetDir, targetErr := resolveExistingDirectory(normalized.TargetDir, "target folder")
	if targetErr != nil {
		return normalized, targetErr
	}
	if probe {
		if err := probeWritableDirectory(targetDir); err != nil {
			return normalized, automationError(
				"AUTOMATION_RULE_TARGET_NOT_WRITABLE",
				fmt.Sprintf("The target folder is not writable: %v", err),
			)
		}
	}

	if sameDirectory(normalized.WatchDir, targetDir) {
		return normalized, automationError(
			"AUTOMATION_RULE_TARGET_INSIDE_WATCH",
			"The target folder cannot be the watch folder. Choose a folder outside it so the rule never sees its own moves.",
		)
	}
	if directoryWithin(normalized.WatchDir, targetDir) {
		return normalized, automationError(
			"AUTOMATION_RULE_TARGET_INSIDE_WATCH",
			fmt.Sprintf(
				"The target folder cannot be inside the watch folder (%s). Choose a folder outside it so the rule never sees its own moves.",
				normalized.WatchDir,
			),
		)
	}
	normalized.TargetDir = targetDir

	normalized.FolderTemplate = strings.TrimSpace(normalized.FolderTemplate)
	normalized.NameTemplate = strings.TrimSpace(normalized.NameTemplate)
	if templateErr := validateOrganizeTemplates(normalized.FolderTemplate, normalized.NameTemplate); templateErr != nil {
		return normalized, automationError("AUTOMATION_RULE_TEMPLATE_INVALID", templateErr.Error())
	}

	policy := normalizeCollisionPolicy(normalized.CollisionPolicy)
	if requested := strings.ToLower(strings.TrimSpace(normalized.CollisionPolicy)); requested != "" &&
		requested != models.AutomationCollisionSkip &&
		requested != models.AutomationCollisionSuffix {
		return normalized, automationError(
			"AUTOMATION_RULE_COLLISION_INVALID",
			fmt.Sprintf(
				"The collision policy '%s' is not supported. Use '%s' or '%s'.",
				strings.TrimSpace(normalized.CollisionPolicy),
				models.AutomationCollisionSkip,
				models.AutomationCollisionSuffix,
			),
		)
	}
	normalized.CollisionPolicy = policy

	return normalized, nil
}

// normalizeRuleAction returns the effective action and rejects an unknown one.
func normalizeRuleAction(value string) (string, *models.JobError) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", models.AutomationActionRecipe:
		return models.AutomationActionRecipe, nil
	case models.AutomationActionOrganize:
		return models.AutomationActionOrganize, nil
	default:
		return "", automationError(
			"AUTOMATION_RULE_ACTION_INVALID",
			fmt.Sprintf(
				"The action '%s' is not supported. Use '%s' or '%s'.",
				strings.TrimSpace(value),
				models.AutomationActionRecipe,
				models.AutomationActionOrganize,
			),
		)
	}
}

// PreviewOrganize returns the planned moves of an organize rule without
// writing a file or recording history. The plan is built with the exact
// template, collision and containment logic execution uses.
//
// When the previewed configuration matches the saved rule, the preview hash is
// stored on that rule and the rule becomes enableable. Previewing unsaved edits
// returns the plan but does not store the hash.
func (s *Service) PreviewOrganize(rule models.AutomationRule) models.AutomationOrganizePreviewResponse {
	response := models.AutomationOrganizePreviewResponse{
		Success:     false,
		Moves:       []models.OrganizePlannedMove{},
		GeneratedAt: time.Now().UnixMilli(),
	}

	normalized, jobErr := s.validateRuleWithProbe(rule, false)
	if jobErr != nil {
		response.Message = jobErr.Message
		response.Error = jobErr

		return response
	}
	if normalized.Action != models.AutomationActionOrganize {
		jobErr := automationError(
			"AUTOMATION_RULE_ACTION_NOT_ORGANIZE",
			"This rule does not organize files. Choose the organize action to preview moves.",
		)
		response.Message = jobErr.Message
		response.Error = jobErr

		return response
	}

	moves, planErr := s.planOrganizeMoves(normalized)
	if planErr != nil {
		response.Message = planErr.Message
		response.Error = planErr

		return response
	}

	hash := organizeConfigHash(normalized)
	response.Success = true
	response.Moves = moves
	response.RuleID = normalized.ID
	response.TargetDir = normalized.TargetDir
	response.PreviewHash = hash

	if normalized.ID != "" && s.store != nil {
		if stored, found := s.store.Get(normalized.ID); found && organizeConfigHash(stored) == hash {
			if err := s.store.SetPreview(normalized.ID, hash, response.GeneratedAt); err != nil {
				log.Printf("automation.rule.preview.persist_failed ruleId=%s err=%v", normalized.ID, err)
			} else {
				response.PreviewStored = true
			}
		}
	}

	response.Message = organizePreviewMessage(moves, response.PreviewStored)

	return response
}

func organizePreviewMessage(moves []models.OrganizePlannedMove, stored bool) string {
	planned := 0
	skipped := 0
	for _, move := range moves {
		if move.Action == models.OrganizeMoveAction {
			planned++

			continue
		}
		skipped++
	}

	message := fmt.Sprintf("Preview: %d %s planned, %d %s.", planned, pluralizeWord("move", planned), skipped, pluralizeWord("skip", skipped))
	if !stored {
		message += " Save the rule with these settings, then preview again before enabling it."
	}

	return message
}

func pluralizeWord(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}

// sameDirectory reports whether both paths name the same existing directory.
// Symlinks are resolved first so a link to the watch folder is still rejected.
func sameDirectory(a, b string) bool {
	return fsx.SameFile(resolveDirectoryPath(a), resolveDirectoryPath(b))
}

// directoryWithin reports whether candidate lives inside outer. Both paths are
// resolved through symlinks so a link cannot bypass the watch-folder guard.
func directoryWithin(outer, candidate string) bool {
	outerPath := resolveDirectoryPath(outer)
	candidatePath := resolveDirectoryPath(candidate)
	if fsx.SameFile(outerPath, candidatePath) {
		return true
	}

	return pathWithin(outerPath, candidatePath)
}

// resolveDirectoryPath resolves symlinks when possible and always returns a
// cleaned absolute path.
func resolveDirectoryPath(path string) string {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return filepath.Clean(strings.TrimSpace(path))
	}
	absolute = filepath.Clean(absolute)
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		return filepath.Clean(resolved)
	}

	return absolute
}

func resolveExistingDirectory(raw, label string) (string, *models.JobError) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", automationError(
			"AUTOMATION_RULE_"+strings.ToUpper(strings.ReplaceAll(label, " ", "_"))+"_REQUIRED",
			fmt.Sprintf("Choose a %s.", label),
		)
	}

	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", automationError(
			"AUTOMATION_RULE_"+strings.ToUpper(strings.ReplaceAll(label, " ", "_"))+"_INVALID",
			fmt.Sprintf("The %s path is not valid.", label),
		)
	}
	absolute = filepath.Clean(absolute)

	info, err := os.Stat(absolute)
	if err != nil {
		if os.IsNotExist(err) {
			return "", automationError(
				"AUTOMATION_RULE_"+strings.ToUpper(strings.ReplaceAll(label, " ", "_"))+"_MISSING",
				fmt.Sprintf("The %s does not exist: %s", label, absolute),
			)
		}

		return "", automationError(
			"AUTOMATION_RULE_"+strings.ToUpper(strings.ReplaceAll(label, " ", "_"))+"_UNREADABLE",
			fmt.Sprintf("The %s cannot be accessed.", label),
		)
	}
	if !info.IsDir() {
		return "", automationError(
			"AUTOMATION_RULE_"+strings.ToUpper(strings.ReplaceAll(label, " ", "_"))+"_NOT_DIRECTORY",
			fmt.Sprintf("The %s must be a directory.", label),
		)
	}

	return absolute, nil
}

func probeWritableDirectory(directory string) error {
	probe, err := os.CreateTemp(directory, ".fileforge-automation-write-*.tmp")
	if err != nil {
		return err
	}
	name := probe.Name()
	_ = probe.Close()

	if err := os.Remove(name); err != nil {
		return err
	}

	return nil
}

func (s *Service) startRule(rule models.AutomationRule) {
	s.mu.Lock()
	if !s.started || s.ctx == nil {
		s.mu.Unlock()
		return
	}
	existing := s.watchers[rule.ID]
	if existing != nil && !existing.isDead() {
		s.mu.Unlock()
		return
	}
	parentCtx := s.ctx
	s.mu.Unlock()

	if existing != nil {
		// A dead watcher (stopped by fsnotify) may still have goroutines
		// unwinding. Wait for them before the replacement starts so two
		// watcher generations never process the same file.
		existing.stop(time.Now().Add(defaultShutdownWait))

		s.mu.Lock()
		if s.watchers[rule.ID] == existing {
			delete(s.watchers, rule.ID)
		}
		s.mu.Unlock()
	}

	if reason := s.checkRuleRuntime(rule); reason != "" {
		s.setRuntimeError(rule.ID, reason)
		return
	}

	watcher, err := newRuleWatcher(s, rule, parentCtx)
	if err != nil {
		log.Printf("automation.watcher.start.failed ruleId=%s dir=%s err=%v", rule.ID, rule.WatchDir, err)
		s.setRuntimeError(rule.ID, fmt.Sprintf("The watch folder could not be watched: %v", err))
		return
	}

	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		watcher.stop(time.Now().Add(defaultShutdownWait))
		return
	}
	if existing := s.watchers[rule.ID]; existing != nil && !existing.isDead() {
		s.mu.Unlock()
		watcher.stop(time.Now().Add(defaultShutdownWait))
		return
	}
	s.watchers[rule.ID] = watcher
	delete(s.runtimeErrors, rule.ID)
	s.mu.Unlock()

	watcher.start()
}

func (s *Service) stopRule(id string) {
	s.mu.Lock()
	watcher := s.watchers[id]
	delete(s.watchers, id)
	s.mu.Unlock()

	if watcher != nil {
		watcher.stop(time.Now().Add(defaultShutdownWait))
	}
}

func (s *Service) checkRuleRuntime(rule models.AutomationRule) string {
	info, err := os.Stat(rule.WatchDir)
	if err != nil || !info.IsDir() {
		return fmt.Sprintf("The watch folder is not available: %s", rule.WatchDir)
	}

	if rule.Action == models.AutomationActionOrganize {
		if !organizePreviewCurrent(rule) {
			return "Preview the current settings before enabling this rule."
		}
		targetInfo, targetErr := os.Stat(rule.TargetDir)
		if targetErr != nil || !targetInfo.IsDir() {
			return fmt.Sprintf("The target folder is not available: %s", rule.TargetDir)
		}
		if sameDirectory(rule.WatchDir, rule.TargetDir) || directoryWithin(rule.WatchDir, rule.TargetDir) {
			return "The target folder is inside the watch folder."
		}

		return ""
	}

	if s.recipes == nil || findRecipe(s.recipes.List(), rule.RecipeID) == nil {
		return fmt.Sprintf("Recipe '%s' is not available.", rule.RecipeID)
	}

	return ""
}

func (s *Service) viewOf(rule models.AutomationRule) models.AutomationRule {
	view := cloneRule(rule)
	view.RuntimeStatus = RuntimeStatusStopped
	view.RuntimeReason = ""

	s.mu.Lock()
	defer s.mu.Unlock()

	if !view.Enabled {
		return view
	}
	if watcher := s.watchers[view.ID]; watcher != nil && !watcher.isDead() {
		view.RuntimeStatus = RuntimeStatusWatching
		return view
	}
	if reason := s.runtimeErrors[view.ID]; reason != "" {
		view.RuntimeStatus = RuntimeStatusDegraded
		view.RuntimeReason = reason
	}

	return view
}

func (s *Service) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.started
}

func (s *Service) setRuntimeError(id, reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}

	s.mu.Lock()
	if s.runtimeErrors == nil {
		s.runtimeErrors = make(map[string]string)
	}
	s.runtimeErrors[id] = reason
	s.mu.Unlock()
}

func (s *Service) clearRuntimeError(id string) {
	s.mu.Lock()
	delete(s.runtimeErrors, id)
	s.mu.Unlock()
}

func (s *Service) recordRun(ruleID, status, message, file string, outputs []string) {
	at := time.Now().UnixMilli()

	if s.store != nil {
		if err := s.store.UpdateRun(ruleID, at, status, message, file, outputs); err != nil {
			log.Printf("automation.run.record.failed ruleId=%s status=%s err=%v", ruleID, status, err)
		}
	}

	if s.runs == nil {
		return
	}

	// The rule may have been deleted while the run was in flight; the captured
	// name falls back to the id so the history entry still makes sense.
	ruleName := ruleID
	if s.store != nil {
		if rule, found := s.store.Get(ruleID); found && strings.TrimSpace(rule.Name) != "" {
			ruleName = rule.Name
		}
	}

	s.runs.Record(models.AutomationRunRecord{
		RuleID:   ruleID,
		RuleName: ruleName,
		FilePath: file,
		Status:   status,
		Message:  message,
		Outputs:  outputs,
		At:       at,
	})
}

func (s *Service) emitRun(event models.AutomationRunEvent) {
	if s.emit == nil {
		return
	}
	if event.At <= 0 {
		event.At = time.Now().UnixMilli()
	}
	event.Outputs = append([]string{}, event.Outputs...)

	s.emit(event)
}

func (s *Service) failure(message, detailCode string, details map[string]any) models.AutomationRuleActionResponse {
	if details == nil {
		details = map[string]any{}
	}

	return models.AutomationRuleActionResponse{
		Success: false,
		Message: message,
		Error:   models.NewCanonicalJobError(detailCode, message, details),
	}
}

func automationError(detailCode, message string) *models.JobError {
	return models.NewCanonicalJobError(detailCode, message, nil)
}

func findRecipe(recipes []models.Recipe, id string) *models.Recipe {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return nil
	}
	for index := range recipes {
		if recipes[index].ID == trimmed {
			return &recipes[index]
		}
	}

	return nil
}

func normalizeExtensions(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		extension := normalizeExtension(value)
		if extension == "" {
			continue
		}
		if _, exists := seen[extension]; exists {
			continue
		}
		seen[extension] = struct{}{}
		normalized = append(normalized, extension)
	}

	return normalized
}

// normalizePatterns trims and deduplicates base-name globs and rejects a
// pattern the watcher could never honour. Matching is filepath.Match against
// the file name, so folder separators and "**" are errors instead of silently
// dead filters; recursion is the Recursive flag's job.
func normalizePatterns(values []string) ([]string, *models.JobError) {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		pattern := strings.TrimSpace(value)
		if pattern == "" {
			continue
		}
		if _, exists := seen[pattern]; exists {
			continue
		}

		if strings.ContainsAny(pattern, `/\`) || strings.Contains(pattern, "**") {
			return nil, automationError(
				"AUTOMATION_RULE_PATTERN_INVALID",
				fmt.Sprintf(
					"The pattern '%s' uses a folder separator or '**'. Patterns match the file name only; enable 'Include subfolders' to watch nested folders.",
					pattern,
				),
			)
		}
		if _, err := filepath.Match(pattern, ""); err != nil {
			return nil, automationError(
				"AUTOMATION_RULE_PATTERN_INVALID",
				fmt.Sprintf(
					"The pattern '%s' is not valid: %v. Use shell-style globs such as invoice-*.pdf.",
					pattern,
					err,
				),
			)
		}

		seen[pattern] = struct{}{}
		normalized = append(normalized, pattern)
	}

	return normalized, nil
}

func normalizeExtension(value string) string {
	return strings.TrimSpace(strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), ".")))
}
