package app

import (
	"context"

	// Import for auto-registration
	_ "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/services"
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

func (a *App) GetSupportedFormats() []models.SupportedFormat {
	return a.conversionService.GetSupportedFormats()
}
