package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	// Import for auto-registration
	_ "fileforge-desktop/internal/doc"
	_ "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	_ "fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/preview"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	_ "fileforge-desktop/internal/video"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type App struct {
	ctx            context.Context
	toolingService *services.ToolingService
	previewService *preview.PreviewService
}

func New() *App {
	reg := registry.GetGlobalRegistry()
	a := &App{
		toolingService: services.NewToolingService(reg),
	}
	// initialize preview service with sensible defaults; AllowedRoots: $HOME + TMP
	home, _ := os.UserHomeDir()
	allowed := []string{}
	if home != "" {
		allowed = append(allowed, home)
	}
	allowed = append(allowed, os.TempDir())
	a.previewService = preview.NewPreviewService(preview.Config{AllowedRoots: allowed, MaxQueue: 1024})
	return a
}

func (a *App) SetContext(ctx context.Context) {
	a.ctx = ctx
	a.toolingService.SetContext(ctx)
}

// Shutdown gracefully stops services owned by App.
func (a *App) Shutdown(ctx context.Context) error {
	var firstErr error
	// shutdown preview service
	if a.previewService != nil {
		if err := a.previewService.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("app: %w", err)
			}
		}
	}
	// shutdown tooling service if it exposes a shutdown (none currently)
	// TODO: if toolingService implements Shutdown(ctx) call it here
	return firstErr
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

// StartPreview enqueues a preview job and returns a job id.
func (a *App) StartPreview(ctx context.Context, req preview.PreviewRequest) preview.PreviewStartResponse {
	id, err := a.previewService.Enqueue(ctx, req)
	if err != nil {
		return preview.PreviewStartResponse{Success: false, Message: err.Error()}
	}
	return preview.PreviewStartResponse{Success: true, JobID: id, Message: "queued"}
}

// GetPreviewStatus returns the job status for a preview job.
func (a *App) GetPreviewStatus(ctx context.Context, jobID string) preview.JobStatus {
	status, ok := a.previewService.Status(jobID)
	if !ok {
		return preview.JobStatus{Status: preview.JobStateFailed, Progress: 0, Message: "not found"}
	}
	return status
}

// GetPreview returns the preview bytes (if ready) for a job.
func (a *App) GetPreview(ctx context.Context, jobID string) preview.PreviewResult {
	res, err := a.previewService.Fetch(ctx, jobID)
	if err != nil {
		return preview.PreviewResult{Success: false, Message: err.Error()}
	}
	return res
}

// CancelPreview attempts to cancel a preview job.
func (a *App) CancelPreview(jobID string) preview.PreviewStartResponse {
	if err := a.previewService.Cancel(jobID); err != nil {
		return preview.PreviewStartResponse{Success: false, Message: err.Error()}
	}
	return preview.PreviewStartResponse{Success: true, JobID: jobID, Message: "canceled"}
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

	// Normalize path inputs before further checks and filesystem access.
	normalized := path

	// Handle file:// URIs (file:///absolute/path and file://host/absolute/path)
	if strings.HasPrefix(normalized, "file://") {
		if u, err := url.Parse(normalized); err == nil {
			// u.Path contains the filesystem path for file:// URLs
			normalized = u.Path
		} else {
			// Fallback: strip the prefix
			normalized = strings.TrimPrefix(normalized, "file://")
		}
	}

	// Expand ~ to user home directory
	if strings.HasPrefix(normalized, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			rest := strings.TrimPrefix(normalized, "~")
			rest = strings.TrimPrefix(rest, string(os.PathSeparator))
			normalized = filepath.Join(home, rest)
		}
	}

	// Clean the path
	normalized = filepath.Clean(normalized)

	if strings.ToLower(filepath.Ext(normalized)) != ".pdf" {
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Preview supports only .pdf files.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_NOT_PDF", "inputPath must be a .pdf file", nil),
		}
	}

	const maxPreviewBytes = 32 * 1024 * 1024

	info, err := os.Stat(normalized)
	if err != nil {
		wrapped := fmt.Errorf("stat %s: %w", normalized, err)
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Cannot access the selected PDF file.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_READ_FAILED", wrapped.Error(), nil),
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

	content, err := os.ReadFile(normalized)
	if err != nil {
		wrapped := fmt.Errorf("read %s: %w", normalized, err)
		return models.PDFPreviewSourceResponseV1{
			Success: false,
			Message: "Failed to read PDF content for preview.",
			Error:   models.NewCanonicalJobError("PDF_PREVIEW_READ_FAILED", wrapped.Error(), nil),
		}
	}

	return models.PDFPreviewSourceResponseV1{
		Success:    true,
		Message:    "preview source loaded",
		DataBase64: base64.StdEncoding.EncodeToString(content),
		MimeType:   "application/pdf",
	}
}
