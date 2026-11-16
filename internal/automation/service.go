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

	"fileforge-desktop/internal/models"
)

const (
	// RunEventNameV1 is the Wails event emitted for every automation run stage.
	RunEventNameV1 = "automation/run/v1"

	// AutomationRunEventNameV1 is kept as an explicit alias for the event name.
	AutomationRunEventNameV1 = RunEventNameV1

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
	List() []models.RecipeV1
	Plan(models.RecipeRequestV1) models.RecipePlanResponseV1
	Run(context.Context, models.RecipeRequestV1) models.RunRecipeResponseV1
	Status(string) models.RecipeStatusResponseV1
}

// Service owns the automation rules and their watchers.
type Service struct {
	store   *Store
	recipes RecipeRunner
	emit    func(models.AutomationRunEventV1)

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
func NewService(store *Store, recipes RecipeRunner, emit func(models.AutomationRunEventV1)) *Service {
	service := &Service{
		store:              store,
		recipes:            recipes,
		emit:               emit,
		stabilityDelay:     defaultStabilityDelay,
		statusPollInterval: defaultStatusPollInterval,
		runTimeout:         defaultRunTimeout,
		producedTTL:        defaultProducedTTL,
		watchers:           make(map[string]*ruleWatcher),
		runtimeErrors:      make(map[string]string),
	}

	return service
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
func (s *Service) ListRules() models.ListAutomationRulesResponseV1 {
	rules := []models.AutomationRuleV1{}
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

	return models.ListAutomationRulesResponseV1{
		Success: true,
		Message: message,
		Rules:   rules,
	}
}

// SaveRule validates, normalizes and persists a rule. When the service is
// running the watcher is restarted to match the new state.
func (s *Service) SaveRule(rule models.AutomationRuleV1) models.AutomationRuleActionResponseV1 {
	if s.store == nil {
		return s.failure("Automation rules are not available.", "AUTOMATION_STORE_UNAVAILABLE", nil)
	}

	normalized, jobErr := s.validateRule(rule)
	if jobErr != nil {
		return models.AutomationRuleActionResponseV1{
			Success: false,
			Message: jobErr.Message,
			Error:   jobErr,
		}
	}

	now := time.Now()
	var existing models.AutomationRuleV1
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

	return models.AutomationRuleActionResponseV1{
		Success: true,
		Message: "Automation rule saved.",
		Rule:    &view,
	}
}

// DeleteRule stops the watcher and removes the rule from the store.
func (s *Service) DeleteRule(id string) models.AutomationRuleActionResponseV1 {
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

	return models.AutomationRuleActionResponseV1{
		Success: true,
		Message: "Automation rule deleted.",
	}
}

// SetRuleEnabled enables or disables a rule and starts or stops its watcher.
func (s *Service) SetRuleEnabled(id string, enabled bool) models.AutomationRuleActionResponseV1 {
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

	return models.AutomationRuleActionResponseV1{
		Success: true,
		Message: message,
		Rule:    &view,
	}
}

// Status returns the compact automation summary used by Home.
func (s *Service) Status() models.AutomationStatusResponseV1 {
	rules := []models.AutomationRuleV1{}
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

	response := models.AutomationStatusResponseV1{
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

func (s *Service) validateRule(rule models.AutomationRuleV1) (models.AutomationRuleV1, *models.JobErrorV1) {
	normalized := cloneRule(rule)
	normalized.ID = strings.TrimSpace(normalized.ID)
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.RecipeID = strings.TrimSpace(normalized.RecipeID)
	normalized.RuntimeStatus = ""
	normalized.RuntimeReason = ""

	if normalized.Name == "" {
		return normalized, automationError("AUTOMATION_RULE_NAME_REQUIRED", "Give the automation rule a name.")
	}

	watchDir, jobErr := resolveExistingDirectory(normalized.WatchDir, "watch folder")
	if jobErr != nil {
		return normalized, jobErr
	}
	normalized.WatchDir = watchDir

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

	if strings.TrimSpace(normalized.OutputDir) != "" {
		outputDir, dirErr := resolveExistingDirectory(normalized.OutputDir, "output folder")
		if dirErr != nil {
			return normalized, dirErr
		}
		if err := probeWritableDirectory(outputDir); err != nil {
			return normalized, automationError(
				"AUTOMATION_RULE_OUTPUT_NOT_WRITABLE",
				fmt.Sprintf("The output folder is not writable: %v", err),
			)
		}
		normalized.OutputDir = outputDir
	} else {
		normalized.OutputDir = ""
	}

	return normalized, nil
}

func resolveExistingDirectory(raw, label string) (string, *models.JobErrorV1) {
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

func (s *Service) startRule(rule models.AutomationRuleV1) {
	s.mu.Lock()
	if !s.started || s.ctx == nil {
		s.mu.Unlock()
		return
	}
	if existing := s.watchers[rule.ID]; existing != nil {
		if !existing.isDead() {
			s.mu.Unlock()
			return
		}
		// A dead watcher (stopped by fsnotify) is replaced by a fresh one.
		delete(s.watchers, rule.ID)
	}
	parentCtx := s.ctx
	s.mu.Unlock()

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

func (s *Service) checkRuleRuntime(rule models.AutomationRuleV1) string {
	info, err := os.Stat(rule.WatchDir)
	if err != nil || !info.IsDir() {
		return fmt.Sprintf("The watch folder is not available: %s", rule.WatchDir)
	}
	if s.recipes == nil || findRecipe(s.recipes.List(), rule.RecipeID) == nil {
		return fmt.Sprintf("Recipe '%s' is not available.", rule.RecipeID)
	}

	return ""
}

func (s *Service) viewOf(rule models.AutomationRuleV1) models.AutomationRuleV1 {
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
	if s.store == nil {
		return
	}

	if err := s.store.UpdateRun(ruleID, time.Now().UnixMilli(), status, message, file, outputs); err != nil {
		log.Printf("automation.run.record.failed ruleId=%s status=%s err=%v", ruleID, status, err)
	}
}

func (s *Service) emitRun(event models.AutomationRunEventV1) {
	if s.emit == nil {
		return
	}
	if event.At <= 0 {
		event.At = time.Now().UnixMilli()
	}
	event.Outputs = append([]string{}, event.Outputs...)

	s.emit(event)
}

func (s *Service) failure(message, detailCode string, details map[string]any) models.AutomationRuleActionResponseV1 {
	if details == nil {
		details = map[string]any{}
	}

	return models.AutomationRuleActionResponseV1{
		Success: false,
		Message: message,
		Error:   models.NewCanonicalJobError(detailCode, message, details),
	}
}

func automationError(detailCode, message string) *models.JobErrorV1 {
	return models.NewCanonicalJobError(detailCode, message, nil)
}

func findRecipe(recipes []models.RecipeV1, id string) *models.RecipeV1 {
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

func normalizeExtension(value string) string {
	return strings.TrimSpace(strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), ".")))
}
