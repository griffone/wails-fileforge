package app

import (
	"fileforge-desktop/internal/automation"
	"fileforge-desktop/internal/models"
)

// AutomationService exposes watch-folder rules and their recorded runs.
type AutomationService struct {
	service *automation.Service
}

// ListAutomationRules lists the saved watch-folder rules with their current
// runtime state (watching, stopped or degraded).
func (s *AutomationService) ListAutomationRules() models.ListAutomationRulesResponse {
	return s.service.ListRules()
}

// SaveAutomationRule validates and persists a watch-folder rule.
func (s *AutomationService) SaveAutomationRule(rule models.AutomationRule) models.AutomationRuleActionResponse {
	return s.service.SaveRule(rule)
}

// DeleteAutomationRule stops and removes a watch-folder rule.
func (s *AutomationService) DeleteAutomationRule(id string) models.AutomationRuleActionResponse {
	return s.service.DeleteRule(id)
}

// SetAutomationRuleEnabled starts or stops a rule's watcher.
func (s *AutomationService) SetAutomationRuleEnabled(id string, enabled bool) models.AutomationRuleActionResponse {
	return s.service.SetRuleEnabled(id, enabled)
}

// GetAutomationStatus returns the compact automation summary used on Home.
func (s *AutomationService) GetAutomationStatus() models.AutomationStatusResponse {
	return s.service.Status()
}

// PreviewAutomationRule returns the planned organize moves for a rule without
// moving, creating or recording anything. A successful preview of the saved
// configuration is what allows an organize rule to be enabled.
func (s *AutomationService) PreviewAutomationRule(rule models.AutomationRule) models.AutomationOrganizePreviewResponse {
	return s.service.PreviewOrganize(rule)
}

// ListAutomationRuns returns the persisted terminal automation runs, newest
// first. A zero limit uses the service default.
func (s *AutomationService) ListAutomationRuns(limit int) models.ListAutomationRunsResponse {
	return s.service.ListRuns(limit)
}

// ClearAutomationRuns permanently removes the persisted run history.
func (s *AutomationService) ClearAutomationRuns() models.AutomationRunsActionResponse {
	return s.service.ClearRuns()
}
