package models

// AutomationRule is one watch-folder rule: a folder, an accepted input kind
// and the recipe that runs for every arriving file. RuntimeStatus and
// RuntimeReason are response-only dynamic state and are never persisted.
type AutomationRule struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Enabled      bool           `json:"enabled"`
	WatchDir     string         `json:"watchDir"`
	RecipeID     string         `json:"recipeId"`
	Extensions   []string       `json:"extensions,omitempty"`
	OutputDir    string         `json:"outputDir,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
	DeleteSource bool           `json:"deleteSource,omitempty"`
	CreatedAt    int64          `json:"createdAt"`
	UpdatedAt    int64          `json:"updatedAt,omitempty"`
	LastRunAt    int64          `json:"lastRunAt,omitempty"`
	LastStatus   string         `json:"lastStatus,omitempty"`
	LastMessage  string         `json:"lastMessage,omitempty"`
	LastFile     string         `json:"lastFile,omitempty"`
	LastOutputs  []string       `json:"lastOutputs,omitempty"`

	// Runtime-only state; never persisted.
	RuntimeStatus string `json:"runtimeStatus,omitempty"`
	RuntimeReason string `json:"runtimeReason,omitempty"`
}

// ListAutomationRulesResponse lists every saved automation rule with its
// current runtime state.
type ListAutomationRulesResponse struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Rules   []AutomationRule `json:"rules"`
}

// AutomationRuleActionResponse is the result of a rule mutation. Rule is the
// saved rule with its runtime state when the mutation succeeded.
type AutomationRuleActionResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Rule    *AutomationRule `json:"rule,omitempty"`
	Error   *JobError       `json:"error,omitempty"`
}

// AutomationStatusResponse is the compact automation summary shown on Home.
// The last-run fields mirror the most recently run rule.
type AutomationStatusResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	TotalRules  int    `json:"totalRules"`
	ActiveRules int    `json:"activeRules"`
	LastRunAt   int64  `json:"lastRunAt,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty"`
	LastMessage string `json:"lastMessage,omitempty"`
	LastFile    string `json:"lastFile,omitempty"`
}

// AutomationRunEvent is emitted on automation/run when a rule starts
// processing a file and when that run reaches a terminal state.
type AutomationRunEvent struct {
	RuleID   string   `json:"ruleId"`
	RuleName string   `json:"ruleName"`
	FilePath string   `json:"filePath"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Outputs  []string `json:"outputs,omitempty"`
	At       int64    `json:"at"`
}

// AutomationRunRecord is one terminal automation run kept in the local run
// history. RuleName is captured at run time so the history survives rule
// deletion.
type AutomationRunRecord struct {
	ID       string   `json:"id"`
	RuleID   string   `json:"ruleId"`
	RuleName string   `json:"ruleName"`
	FilePath string   `json:"filePath"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Outputs  []string `json:"outputs,omitempty"`
	At       int64    `json:"at"`
}

// ListAutomationRunsResponse lists the persisted automation run history,
// newest first.
type ListAutomationRunsResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Runs    []AutomationRunRecord `json:"runs"`
}

// AutomationRunsActionResponse is the result of clearing the run history.
type AutomationRunsActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
