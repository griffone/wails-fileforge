package models

// Rule actions. A rule either runs a recipe for every accepted file or
// organizes the files into a target folder. An older persisted rule has no
// action key and loads as ActionRecipe.
const (
	AutomationActionRecipe   = "recipe"
	AutomationActionOrganize = "organize"
)

// Collision policies for organize rules. A destination that already exists is
// either left alone (CollisionSkip) or renamed with a -2, -3, ... suffix
// (CollisionSuffix). Nothing is ever overwritten.
const (
	AutomationCollisionSkip   = "skip"
	AutomationCollisionSuffix = "suffix"
)

// AutomationRule is one watch-folder rule: a folder, an accepted input kind
// and the action that runs for every arriving file. RuntimeStatus and
// RuntimeReason are response-only dynamic state and are never persisted.
//
// Recursive and Patterns are optional filters added after the first release:
// an older persisted rule has neither key, so it loads with recursion off and
// no patterns and keeps matching exactly the direct children of its folder.
// Patterns match the file base name with filepath.Match semantics; recursion
// comes from Recursive, never from the pattern.
//
// Action was added with organize: an older rule loads as AutomationActionRecipe
// with every organize field empty, so it keeps behaving exactly as before.
type AutomationRule struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Enabled      bool           `json:"enabled"`
	WatchDir     string         `json:"watchDir"`
	Action       string         `json:"action,omitempty"`
	RecipeID     string         `json:"recipeId"`
	Extensions   []string       `json:"extensions,omitempty"`
	Recursive    bool           `json:"recursive,omitempty"`
	Patterns     []string       `json:"patterns,omitempty"`
	OutputDir    string         `json:"outputDir,omitempty"`
	Options      map[string]any `json:"options,omitempty"`
	DeleteSource bool           `json:"deleteSource,omitempty"`

	// Organize configuration. It is only used when Action is
	// AutomationActionOrganize.
	TargetDir       string `json:"targetDir,omitempty"`
	FolderTemplate  string `json:"folderTemplate,omitempty"`
	NameTemplate    string `json:"nameTemplate,omitempty"`
	CollisionPolicy string `json:"collisionPolicy,omitempty"`

	// PreviewHash is the hash of the organize configuration that produced the
	// last successful preview. An organize rule cannot be enabled until this
	// hash matches the current configuration.
	PreviewHash string `json:"previewHash,omitempty"`
	PreviewedAt int64  `json:"previewedAt,omitempty"`

	CreatedAt   int64    `json:"createdAt"`
	UpdatedAt   int64    `json:"updatedAt,omitempty"`
	LastRunAt   int64    `json:"lastRunAt,omitempty"`
	LastStatus  string   `json:"lastStatus,omitempty"`
	LastMessage string   `json:"lastMessage,omitempty"`
	LastFile    string   `json:"lastFile,omitempty"`
	LastOutputs []string `json:"lastOutputs,omitempty"`

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

// Planned move actions reported by an organize preview.
const (
	OrganizeMoveAction = "move"
	OrganizeSkipAction = "skip"
)

// OrganizePlannedMove is one item of an organize dry run: the source file, the
// destination the rule resolved for it (collisions included), and whether the
// item would be moved or left alone. Destination is empty for a skipped item.
type OrganizePlannedMove struct {
	Source      string `json:"source"`
	Destination string `json:"destination,omitempty"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
}

// AutomationOrganizePreviewResponse is the dry-run plan of an organize rule. It
// reports every planned move without touching a file or recording history.
// PreviewStored is true when the preview configuration matched the saved rule
// and the hash needed to enable it was stored.
type AutomationOrganizePreviewResponse struct {
	Success       bool                  `json:"success"`
	Message       string                `json:"message"`
	RuleID        string                `json:"ruleId,omitempty"`
	TargetDir     string                `json:"targetDir,omitempty"`
	Moves         []OrganizePlannedMove `json:"moves"`
	PreviewHash   string                `json:"previewHash,omitempty"`
	PreviewStored bool                  `json:"previewStored"`
	GeneratedAt   int64                 `json:"generatedAt,omitempty"`
	Error         *JobError             `json:"error,omitempty"`
}
