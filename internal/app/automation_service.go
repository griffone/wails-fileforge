package app

import (
	"fileforge-desktop/internal/automation"
	"fileforge-desktop/internal/models"
)

// AutomationService exposes watch-folder rules and their recorded runs.
type AutomationService struct {
	service *automation.Service
}

// ListAutomationRulesV1 lists the saved watch-folder rules with their current
// runtime state (watching, stopped or degraded).
func (s *AutomationService) ListAutomationRulesV1() models.ListAutomationRulesResponseV1 {
	return s.service.ListRules()
}

// SaveAutomationRuleV1 validates and persists a watch-folder rule.
func (s *AutomationService) SaveAutomationRuleV1(rule models.AutomationRuleV1) models.AutomationRuleActionResponseV1 {
	return s.service.SaveRule(rule)
}

// DeleteAutomationRuleV1 stops and removes a watch-folder rule.
func (s *AutomationService) DeleteAutomationRuleV1(id string) models.AutomationRuleActionResponseV1 {
	return s.service.DeleteRule(id)
}

// SetAutomationRuleEnabledV1 starts or stops a rule's watcher.
func (s *AutomationService) SetAutomationRuleEnabledV1(id string, enabled bool) models.AutomationRuleActionResponseV1 {
	return s.service.SetRuleEnabled(id, enabled)
}

// GetAutomationStatusV1 returns the compact automation summary used on Home.
func (s *AutomationService) GetAutomationStatusV1() models.AutomationStatusResponseV1 {
	return s.service.Status()
}

// ListAutomationRunsV1 returns the persisted terminal automation runs, newest
// first. A zero limit uses the service default.
func (s *AutomationService) ListAutomationRunsV1(limit int) models.ListAutomationRunsResponseV1 {
	return s.service.ListRuns(limit)
}

// ClearAutomationRunsV1 permanently removes the persisted run history.
func (s *AutomationService) ClearAutomationRunsV1() models.AutomationRunsActionResponseV1 {
	return s.service.ClearRuns()
}
