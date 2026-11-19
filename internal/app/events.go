package app

import (
	"fileforge-desktop/internal/automation"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/services"
	"fileforge-desktop/internal/toolchain"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Registering the emitted events with their payload types lets the binding
// generator emit typed event names and payload interfaces for the frontend.
// The names are the current versioned ones; phase 6 renames them.
func init() {
	application.RegisterEvent[models.JobProgressEventV1](services.JobProgressEventNameV1)
	application.RegisterEvent[models.JobResultV1](services.JobStateEventNameV1)
	application.RegisterEvent[models.RecipeProgressEventV1](recipes.ProgressEventNameV1)
	application.RegisterEvent[models.RecipeResultV1](recipes.StateEventNameV1)
	application.RegisterEvent[models.AutomationRunEventV1](automation.RunEventNameV1)
	application.RegisterEvent[models.ToolchainInstallEventV1](toolchain.InstallEventNameV1)
}
