package app

import (
	"context"

	// Import for auto-registration
	_ "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type App struct {
	ctx               context.Context
	conversionService *services.ConversionService
}

func New() *App {
	return &App{
		conversionService: services.NewConversionService(),
	}
}

func (a *App) SetContext(ctx context.Context) {
	a.ctx = ctx
	a.conversionService.SetContext(ctx)
}

// Wails bindings
func (a *App) ConvertFile(req models.ConversionRequest) models.ConversionResult {
	return a.conversionService.ConvertFile(req)
}

// ConvertBatch converts multiple files in batch
func (a *App) ConvertBatch(req models.BatchConversionRequest) models.BatchConversionResult {
	return a.conversionService.ConvertBatch(req)
}

func (a *App) GetSupportedFormats() []models.SupportedFormat {
	return a.conversionService.GetSupportedFormats()
}

// OpenFileDialog opens a native file dialog and returns the selected file path
func (a *App) OpenFileDialog() (string, error) {
	app := application.Get()

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Select Image File")

	return dialog.PromptForSingleSelection()
}

// OpenMultipleFilesDialog opens a native file dialog and returns multiple selected file paths
func (a *App) OpenMultipleFilesDialog() ([]string, error) {
	app := application.Get()

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Select Image Files")

	return dialog.PromptForMultipleSelection()
}

// OpenDirectoryDialog opens a native directory dialog and returns the selected directory path
// TODO: Implement proper directory selection when Wails 3 API is available
func (a *App) OpenDirectoryDialog() (string, error) {
	// For now, return empty string - user can manually input directory
	return "", nil
}
