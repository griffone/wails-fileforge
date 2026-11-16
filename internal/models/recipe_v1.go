package models

// RecipeStepV1 describes one tool invocation inside a recipe. Phase 1 recipes
// are curated Go definitions: the step only exposes the tool identity, not the
// full option template, so the UI can render a compact stepper.
type RecipeStepV1 struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	ToolID string `json:"toolId"`
	Detail string `json:"detail,omitempty"`
}

// RecipeV1 is a curated outcome: one input kind, a chain of tools and one
// verified result set. Recipes are built-in Go definitions, not user files.
// MaxInputs is 0 when the recipe accepts any number of inputs.
type RecipeV1 struct {
	ID                     string         `json:"id"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	Category               string         `json:"category"`
	InputKind              string         `json:"inputKind"`
	AcceptExtensions       []string       `json:"acceptExtensions"`
	MaxInputs              int            `json:"maxInputs,omitempty"`
	Highlights             []string       `json:"highlights,omitempty"`
	Steps                  []RecipeStepV1 `json:"steps"`
	DefaultTargetSizeBytes int64          `json:"defaultTargetSizeBytes"`
}

// RecipeRequestV1 is the plan/run payload for a recipe. Options carries recipe
// specific knobs; Email PDF reads targetSizeBytes.
type RecipeRequestV1 struct {
	RecipeID   string         `json:"recipeId"`
	InputPaths []string       `json:"inputPaths"`
	OutputDir  string         `json:"outputDir"`
	Options    map[string]any `json:"options"`
}

// RecipePlanResponseV1 resolves what a run would produce without executing any
// tool: recipe metadata, validated inputs, the final destination and warnings.
// Intermediate paths never appear here.
type RecipePlanResponseV1 struct {
	Success    bool              `json:"success"`
	Message    string            `json:"message"`
	Recipe     *RecipeV1         `json:"recipe,omitempty"`
	InputPaths []string          `json:"inputPaths"`
	OutputDir  string            `json:"outputDir"`
	Outputs    []PlannedOutputV1 `json:"outputs"`
	Warnings   []string          `json:"warnings,omitempty"`
	Error      *JobErrorV1       `json:"error,omitempty"`
}

// RecipeStepResultV1 is the per-step outcome of a recipe run.
type RecipeStepResultV1 struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	ToolID  string      `json:"toolId"`
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Outputs []string    `json:"outputs,omitempty"`
	Error   *JobErrorV1 `json:"error,omitempty"`
}

// RecipeResultV1 is the terminal snapshot of a recipe run. CurrentStep is
// 1-based while a step is active and ends at TotalSteps once the chain
// completes.
type RecipeResultV1 struct {
	JobID           string                `json:"jobId"`
	RecipeID        string                `json:"recipeId"`
	Status          string                `json:"status"`
	Message         string                `json:"message"`
	CurrentStep     int                   `json:"currentStep"`
	TotalSteps      int                   `json:"totalSteps"`
	Steps           []RecipeStepResultV1  `json:"steps"`
	Outputs         []string              `json:"outputs"`
	TargetSizeBytes int64                 `json:"targetSizeBytes,omitempty"`
	Verification    *VerificationReportV1 `json:"verification,omitempty"`
	Error           *JobErrorV1           `json:"error,omitempty"`
	StartedAt       int64                 `json:"startedAt"`
	EndedAt         int64                 `json:"endedAt,omitempty"`
}

// RunRecipeResponseV1 acknowledges a recipe submission. Recipe jobs live in
// memory only for now, so a restart loses in-flight recipe runs.
type RunRecipeResponseV1 struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	JobID   string      `json:"jobId"`
	Status  string      `json:"status"`
	Error   *JobErrorV1 `json:"error,omitempty"`
}

// RecipeStatusResponseV1 returns the current per-step snapshot of a run.
type RecipeStatusResponseV1 struct {
	Success bool            `json:"success"`
	Found   bool            `json:"found"`
	Message string          `json:"message"`
	Result  *RecipeResultV1 `json:"result,omitempty"`
	Error   *JobErrorV1     `json:"error,omitempty"`
}

// ListRecipesResponseV1 lists the built-in recipes available to the UI.
type ListRecipesResponseV1 struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Recipes []RecipeV1 `json:"recipes"`
}

// RecipeProgressEventV1 is emitted on recipes/progress/v1 for every stage of a
// run so the UI can drive a stepper without polling.
type RecipeProgressEventV1 struct {
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
