package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Import for auto-registration
	"fileforge-desktop/internal/automation"
	"fileforge-desktop/internal/capabilities"
	_ "fileforge-desktop/internal/doc"
	_ "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/media"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/preview"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	"fileforge-desktop/internal/video"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type App struct {
	ctx               context.Context
	toolingService    *services.ToolingService
	recipeService     *recipes.Service
	automationService *automation.Service
	previewService    *preview.PreviewService
}

func New() *App {
	reg := registry.GetGlobalRegistry()
	toolingService := services.NewToolingService(reg)
	a := &App{
		toolingService: toolingService,
	}
	a.recipeService = recipes.NewService(reg, toolingService.JobHistory(), func(evt models.RecipeProgressEventV1) {
		app := application.Get()
		if app == nil {
			return
		}
		app.Event.Emit(recipes.ProgressEventNameV1, evt)
	})
	automationStore := automation.NewStore(automation.DefaultPath())
	a.automationService = automation.NewService(automationStore, a.recipeService, func(evt models.AutomationRunEventV1) {
		app := application.Get()
		if app == nil {
			return
		}
		app.Event.Emit(automation.RunEventNameV1, evt)
	})
	a.automationService.SetRunStore(automation.NewRunStore(automation.DefaultRunPath()))
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

// contextOrBackground returns the app lifetime context, falling back to a
// background context in headless tests and early calls.
func (a *App) contextOrBackground() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

// ServiceStartup wires the Wails application context into the app services so
// job persistence and interrupted-job recovery become active.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.SetContext(ctx)
	// Automation rules are optional: a broken rule or watch folder is reported
	// as degraded and must never abort app startup.
	if a.automationService != nil {
		a.automationService.Start(ctx)
	}
	return nil
}

// ServiceShutdown gracefully stops services owned by App.
func (a *App) ServiceShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.Shutdown(ctx)
}

// Shutdown gracefully stops services owned by App.
func (a *App) Shutdown(ctx context.Context) error {
	var firstErr error
	if a.automationService != nil {
		a.automationService.Shutdown()
	}
	if a.recipeService != nil {
		a.recipeService.Shutdown()
	}
	// shutdown preview service
	if a.previewService != nil {
		if err := a.previewService.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("app: %w", err)
			}
		}
	}
	if err := media.CloseServer(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("app media server: %w", err)
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

func (a *App) PlanJobV1(req models.JobRequestV1) models.PlanJobResponseV1 {
	return a.toolingService.PlanJobV1(req)
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

// VerifyJobV1 verifies every output of a finished job: existence, readability,
// size, file type and media-specific metadata. The report lists passing and
// failing checks per output.
func (a *App) VerifyJobV1(jobID string) models.VerificationReportV1 {
	return a.toolingService.VerifyJobV1(jobID)
}

// ListJobHistoryV1 returns recorded terminal jobs with their output
// fingerprints, newest first. The history survives app restarts.
func (a *App) ListJobHistoryV1() []models.JobHistoryEntryV1 {
	return a.toolingService.ListJobHistoryV1()
}

// UndoJobV1 moves the untouched outputs of a recorded job to the app trash.
// Modified or missing outputs are skipped and reported per file.
func (a *App) UndoJobV1(jobID string) models.UndoResultV1 {
	return a.toolingService.UndoJobV1(jobID)
}

// RestoreJobV1 moves trashed outputs back to their original paths when the
// destination is free.
func (a *App) RestoreJobV1(jobID string) models.UndoResultV1 {
	return a.toolingService.RestoreJobV1(jobID)
}

// EmptyTrashV1 permanently removes the app-managed trash. User files outside
// the trash are never touched.
func (a *App) EmptyTrashV1() models.PathActionResponseV1 {
	return a.toolingService.EmptyTrashV1()
}

// ListRecipesV1 lists the built-in outcome recipes. Email PDF is first.
func (a *App) ListRecipesV1() models.ListRecipesResponseV1 {
	return models.ListRecipesResponseV1{
		Success: true,
		Message: "recipes listed successfully",
		Recipes: a.recipeService.List(),
	}
}

// GetOCRCapabilitiesV1 reports whether local OCR can run: the pdftoppm,
// tesseract and pdftotext binaries, the installed languages and the
// recommended one. It is safe to call before any OCR job.
func (a *App) GetOCRCapabilitiesV1() models.OCRCapabilitiesResponseV1 {
	return pdf.OCRCapabilitiesV1(a.contextOrBackground())
}

// GetTranscriptionCapabilitiesV1 reports whether local Whisper transcription
// can run: the resolved whisper-cli binary, the resolved model with its size,
// and the supported languages. Empty paths resolve the installed defaults, so
// the Settings panel can pass persisted overrides. It never fails: a missing
// binary or model is reported with guidance, not an error.
func (a *App) GetTranscriptionCapabilitiesV1(whisperPath, modelPath string) models.TranscriptionCapabilitiesResponseV1 {
	return video.TranscriptionCapabilitiesV1(whisperPath, modelPath)
}

// GetLocalCapabilitiesV1 reports every external tool the local tools rely on,
// with availability, required/optional state and guidance, plus the installed
// OCR languages. It never fails: a missing tool is reported, not an error.
func (a *App) GetLocalCapabilitiesV1() models.LocalCapabilitiesResponseV1 {
	return capabilities.Detect(a.contextOrBackground())
}

// PlanRecipeV1 validates a recipe request and resolves its final output
// without running any tool.
func (a *App) PlanRecipeV1(req models.RecipeRequestV1) models.RecipePlanResponseV1 {
	return a.recipeService.Plan(req)
}

// RunRecipeV1 starts a recipe chain in the background and returns its job id.
func (a *App) RunRecipeV1(req models.RecipeRequestV1) models.RunRecipeResponseV1 {
	return a.recipeService.Run(a.contextOrBackground(), req)
}

// GetRecipeStatusV1 returns the per-step snapshot of a recipe run.
func (a *App) GetRecipeStatusV1(jobID string) models.RecipeStatusResponseV1 {
	return a.recipeService.Status(jobID)
}

// CancelRecipeV1 requests cancellation of a running recipe.
func (a *App) CancelRecipeV1(jobID string) models.RecipeStatusResponseV1 {
	return a.recipeService.Cancel(jobID)
}

// ListAutomationRulesV1 lists the saved watch-folder rules with their current
// runtime state (watching, stopped or degraded).
func (a *App) ListAutomationRulesV1() models.ListAutomationRulesResponseV1 {
	return a.automationService.ListRules()
}

// SaveAutomationRuleV1 validates and persists a watch-folder rule.
func (a *App) SaveAutomationRuleV1(rule models.AutomationRuleV1) models.AutomationRuleActionResponseV1 {
	return a.automationService.SaveRule(rule)
}

// DeleteAutomationRuleV1 stops and removes a watch-folder rule.
func (a *App) DeleteAutomationRuleV1(id string) models.AutomationRuleActionResponseV1 {
	return a.automationService.DeleteRule(id)
}

// SetAutomationRuleEnabledV1 starts or stops a rule's watcher.
func (a *App) SetAutomationRuleEnabledV1(id string, enabled bool) models.AutomationRuleActionResponseV1 {
	return a.automationService.SetRuleEnabled(id, enabled)
}

// GetAutomationStatusV1 returns the compact automation summary used on Home.
func (a *App) GetAutomationStatusV1() models.AutomationStatusResponseV1 {
	return a.automationService.Status()
}

// ListAutomationRunsV1 returns the persisted terminal automation runs, newest
// first. A zero limit uses the service default.
func (a *App) ListAutomationRunsV1(limit int) models.ListAutomationRunsResponseV1 {
	return a.automationService.ListRuns(limit)
}

// ClearAutomationRunsV1 permanently removes the persisted run history.
func (a *App) ClearAutomationRunsV1() models.AutomationRunsActionResponseV1 {
	return a.automationService.ClearRuns()
}

// InspectFilesV1 describes files for UI cards. Missing files are reported per
// item instead of failing the batch.
func (a *App) InspectFilesV1(paths []string) models.InspectFilesResponseV1 {
	return models.InspectFilesResponseV1{
		Success: true,
		Message: "files inspected",
		Files:   inspect.InspectFiles(paths),
	}
}

// OpenPath opens an existing file with the operating system default handler.
func (a *App) OpenPath(path string) models.PathActionResponseV1 {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return models.PathActionResponseV1{Success: false, Message: "Choose a file to open."}
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return models.PathActionResponseV1{Success: false, Message: "The file is no longer available."}
	}
	if info.IsDir() {
		return models.PathActionResponseV1{Success: false, Message: "Choose a file to open, not a folder."}
	}

	app := application.Get()
	if app == nil || app.Browser == nil {
		return models.PathActionResponseV1{Success: false, Message: "Opening files is only available in the desktop app."}
	}

	if err := app.Browser.OpenFile(trimmed); err != nil {
		return models.PathActionResponseV1{Success: false, Message: "The file could not be opened."}
	}

	return models.PathActionResponseV1{Success: true, Message: "file opened"}
}

// RevealInFolder shows the given file or folder in the native file manager.
func (a *App) RevealInFolder(path string) models.PathActionResponseV1 {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return models.PathActionResponseV1{Success: false, Message: "Choose a file or folder to show."}
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return models.PathActionResponseV1{Success: false, Message: "The file is no longer available."}
	}

	app := application.Get()
	if app == nil || app.Env == nil {
		return models.PathActionResponseV1{Success: false, Message: "Showing files is only available in the desktop app."}
	}

	if err := app.Env.OpenFileManager(trimmed, !info.IsDir()); err != nil {
		return models.PathActionResponseV1{Success: false, Message: "The file could not be shown in the folder."}
	}

	return models.PathActionResponseV1{Success: true, Message: "folder opened"}
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

// RegisterMediaPath registers a local file for token-scoped streaming. The
// relative URL is served by the embedded asset server in production; the
// loopback fallback covers Wails development mode, where the webview is served
// by the frontend dev server and never reaches the asset middleware.
func (a *App) RegisterMediaPath(path string) models.MediaSourceResponseV1 {
	if strings.TrimSpace(path) == "" {
		return models.MediaSourceResponseV1{Success: false, Message: "Choose a file to preview."}
	}

	token, err := media.Register(path)
	if err != nil {
		return models.MediaSourceResponseV1{Success: false, Message: "This file cannot be previewed."}
	}

	response := models.MediaSourceResponseV1{
		Success: true,
		URL:     "/media/" + token,
		Message: "ok",
	}
	if baseURL, serverErr := media.EnsureServer(); serverErr == nil && baseURL != "" {
		response.FallbackURL = baseURL + "/media/" + token
	}

	return response
}
