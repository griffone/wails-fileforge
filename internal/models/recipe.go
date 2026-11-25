package models

// RecipeStep describes one tool invocation inside a recipe. Phase 1 recipes
// are curated Go definitions: the step only exposes the tool identity, not the
// full option template, so the UI can render a compact stepper.
type RecipeStep struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	ToolID string `json:"toolId"`
	Detail string `json:"detail,omitempty"`
}

// RecipeOptionKind is the control type declared by a recipe option field.
type RecipeOptionKind string

const (
	// RecipeOptionInteger is a whole number, optionally bounded and scaled.
	RecipeOptionInteger RecipeOptionKind = "integer"
	// RecipeOptionBoolean is a checkbox.
	RecipeOptionBoolean RecipeOptionKind = "boolean"
	// RecipeOptionEnum is a fixed set of allowed string values.
	RecipeOptionEnum RecipeOptionKind = "enum"
	// RecipeOptionText is a free-form string.
	RecipeOptionText RecipeOptionKind = "text"
)

// RecipeOptionChoice is one allowed value of an enum option.
type RecipeOptionChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// RecipeOptionField declares one editable option a recipe can honour. It
// carries enough metadata for the UI to render and validate the control
// without recipe-specific code.
//
// Scale converts the canonical stored value to the unit shown to the user. For
// example an integer field stored in bytes with Unit "MB" and Scale 1048576 is
// rendered as a whole number of megabytes. Min and Max are expressed in the
// canonical stored unit.
type RecipeOptionField struct {
	Key     string               `json:"key"`
	Label   string               `json:"label"`
	Kind    RecipeOptionKind     `json:"kind"`
	Unit    string               `json:"unit,omitempty"`
	Min     *float64             `json:"min,omitempty"`
	Max     *float64             `json:"max,omitempty"`
	Scale   float64              `json:"scale,omitempty"`
	Values  []RecipeOptionChoice `json:"values,omitempty"`
	Default any                  `json:"default"`
	Help    string               `json:"help,omitempty"`
}

// Recipe is a curated outcome: one input kind, a chain of tools and one
// verified result set. Built-in recipes are Go definitions; user recipes are
// stored overrides applied on top of a built-in base recipe. MaxInputs is 0
// when the recipe accepts any number of inputs.
type Recipe struct {
	ID                     string              `json:"id"`
	Name                   string              `json:"name"`
	Description            string              `json:"description"`
	Category               string              `json:"category"`
	InputKind              string              `json:"inputKind"`
	AcceptExtensions       []string            `json:"acceptExtensions"`
	MaxInputs              int                 `json:"maxInputs,omitempty"`
	Highlights             []string            `json:"highlights,omitempty"`
	Steps                  []RecipeStep        `json:"steps"`
	DefaultTargetSizeBytes int64               `json:"defaultTargetSizeBytes"`
	OptionSchema           []RecipeOptionField `json:"optionSchema,omitempty"`
	OptionOverrides        map[string]any      `json:"optionOverrides,omitempty"`
	UserDefined            bool                `json:"userDefined,omitempty"`
	BaseRecipeID           string              `json:"baseRecipeId,omitempty"`
	Duplicable             bool                `json:"duplicable"`
}

// SaveUserRecipeRequest is the duplicate/edit payload for a user recipe. An
// empty ID creates a new recipe; a non-empty ID updates the stored one. The
// options are validated against the base recipe schema before they are kept.
type SaveUserRecipeRequest struct {
	ID           string         `json:"id,omitempty"`
	Name         string         `json:"name"`
	BaseRecipeID string         `json:"baseRecipeId"`
	Options      map[string]any `json:"options"`
}

// UserRecipeResponse returns the saved, renamed or deleted user recipe.
type UserRecipeResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Recipe  *Recipe   `json:"recipe,omitempty"`
	Error   *JobError `json:"error,omitempty"`
}

// RecipeRequest is the plan/run payload for a recipe. Options carries recipe
// specific knobs; Email PDF reads targetSizeBytes.
type RecipeRequest struct {
	RecipeID   string         `json:"recipeId"`
	InputPaths []string       `json:"inputPaths"`
	OutputDir  string         `json:"outputDir"`
	Options    map[string]any `json:"options"`
}

// RecipePlanResponse resolves what a run would produce without executing any
// tool: recipe metadata, validated inputs, the final destination and warnings.
// Intermediate paths never appear here.
type RecipePlanResponse struct {
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Recipe     *Recipe         `json:"recipe,omitempty"`
	InputPaths []string        `json:"inputPaths"`
	OutputDir  string          `json:"outputDir"`
	Outputs    []PlannedOutput `json:"outputs"`
	Warnings   []string        `json:"warnings,omitempty"`
	Error      *JobError       `json:"error,omitempty"`
}

// RecipeStepResult is the per-step outcome of a recipe run.
type RecipeStepResult struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	ToolID  string    `json:"toolId"`
	Status  string    `json:"status"`
	Message string    `json:"message,omitempty"`
	Outputs []string  `json:"outputs,omitempty"`
	Error   *JobError `json:"error,omitempty"`
}

// RecipeResult is the terminal snapshot of a recipe run. CurrentStep is
// 1-based while a step is active and ends at TotalSteps once the chain
// completes.
type RecipeResult struct {
	JobID           string              `json:"jobId"`
	RecipeID        string              `json:"recipeId"`
	Status          string              `json:"status"`
	Message         string              `json:"message"`
	CurrentStep     int                 `json:"currentStep"`
	TotalSteps      int                 `json:"totalSteps"`
	Steps           []RecipeStepResult  `json:"steps"`
	Outputs         []string            `json:"outputs"`
	TargetSizeBytes int64               `json:"targetSizeBytes,omitempty"`
	Verification    *VerificationReport `json:"verification,omitempty"`
	Error           *JobError           `json:"error,omitempty"`
	StartedAt       int64               `json:"startedAt"`
	EndedAt         int64               `json:"endedAt,omitempty"`
}

// RunRecipeResponse acknowledges a recipe submission. Run snapshots are
// persisted when the service is configured with a store path, so a restart
// keeps finished runs and reports in-flight runs as interrupted.
type RunRecipeResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	JobID   string    `json:"jobId"`
	Status  string    `json:"status"`
	Error   *JobError `json:"error,omitempty"`
}

// RecipeStatusResponse returns the current per-step snapshot of a run.
type RecipeStatusResponse struct {
	Success bool          `json:"success"`
	Found   bool          `json:"found"`
	Message string        `json:"message"`
	Result  *RecipeResult `json:"result,omitempty"`
	Error   *JobError     `json:"error,omitempty"`
}

// ListRecipesResponse lists the built-in recipes available to the UI.
type ListRecipesResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Recipes []Recipe `json:"recipes"`
}

// RecipeProgressEvent is emitted on recipes/progress for every stage of a
// run so the UI can drive a stepper without polling.
type RecipeProgressEvent struct {
	JobID       string `json:"jobId"`
	RecipeID    string `json:"recipeId"`
	Status      string `json:"status"`
	CurrentStep int    `json:"currentStep"`
	TotalSteps  int    `json:"totalSteps"`
	StepName    string `json:"stepName,omitempty"`
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	Percent     int    `json:"percent"`
}
