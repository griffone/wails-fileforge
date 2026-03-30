package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	// Import for auto-registration
	_ "fileforge-desktop/internal/doc"
	_ "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	_ "fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	_ "fileforge-desktop/internal/video"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type App struct {
	ctx            context.Context
	toolingService *services.ToolingService
}

func New() *App {
	reg := registry.GetGlobalRegistry()
	return &App{
		toolingService: services.NewToolingService(reg),
	}
}

func (a *App) SetContext(ctx context.Context) {
	a.ctx = ctx
	a.toolingService.SetContext(ctx)
}

func (a *App) ListToolsV1() models.ListToolsResponseV1 {
	return a.toolingService.ListToolsV1()
}

func (a *App) ValidateJobV1(req models.JobRequestV1) models.ValidateJobResponseV1 {
	return a.toolingService.ValidateJobV1(req)
}

func (a *App) RunJobV1(req models.JobRequestV1) models.RunJobResponseV1 {
	return a.toolingService.RunJobV1(req)
}

func (a *App) CancelJobV1(jobID string) models.CancelJobResponseV1 {
	return a.toolingService.CancelJobV1(jobID)
}

func (a *App) GetJobStatusV1(jobID string) models.JobStatusResponseV1 {
	return a.toolingService.GetJobStatusV1(jobID)
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

func (a *App) OpenDirectoryDialog() (string, error) {
	app := application.Get()

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Select Directory")
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)

	return dialog.PromptForSingleSelection()
}

func (a *App) GetPDFPreviewSourceV1(inputPath string) models.PDFPreviewSourceResponseV1 {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Select a valid PDF file path and retry.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_INVALID_PATH", "inputPath is required", nil),
		}
	}

	if strings.ToLower(filepath.Ext(path)) != ".pdf" {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Preview supports only .pdf files.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_NOT_PDF", "inputPath must be a .pdf file", nil),
		}
	}

	const maxPreviewBytes = 8 * 1024 * 1024

	info, err := os.Stat(path)
	if err != nil {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Cannot access the selected PDF file.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_READ_FAILED", err.Error(), nil),
		}
	}

	if info.IsDir() {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Selected path is a directory, not a PDF file.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_INVALID_PATH", "inputPath points to a directory", nil),
		}
	}

	if info.Size() > maxPreviewBytes {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "PDF is too large for preview. Use a smaller file to inspect crop.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_TOO_LARGE", fmt.Sprintf("input size %d exceeds %d bytes", info.Size(), maxPreviewBytes), nil),
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Failed to read PDF content for preview.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_READ_FAILED", err.Error(), nil),
		}
	}

	return models.PDFPreviewSourceResponseV1{
		Success:    true,
		Message:    "preview source loaded",
		DataBase64: base64.StdEncoding.EncodeToString(content),
		MimeType:   "application/pdf",
	}
}
