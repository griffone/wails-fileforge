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
	application.RegisterEvent[models.JobProgressEvent](services.JobProgressEventName)
	application.RegisterEvent[models.JobResult](services.JobStateEventName)
	application.RegisterEvent[models.RecipeProgressEvent](recipes.ProgressEventName)
	application.RegisterEvent[models.RecipeResult](recipes.StateEventName)
	application.RegisterEvent[models.AutomationRunEvent](automation.RunEventName)
	application.RegisterEvent[models.ToolchainInstallEvent](toolchain.InstallEventName)
}
