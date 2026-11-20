package app

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/capabilities"
	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/media"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/video"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// SystemService exposes machine capabilities, file inspection, native dialogs,
// desktop actions, notifications, and media registration.
type SystemService struct {
	runtime  *Runtime
	notifier *Notifier
}

// GetOCRCapabilitiesV1 reports whether local OCR can run: the pdftoppm,
// tesseract and pdftotext binaries, the installed languages and the
// recommended one. It is safe to call before any OCR job.
func (s *SystemService) GetOCRCapabilitiesV1() models.OCRCapabilitiesResponseV1 {
	return pdf.OCRCapabilitiesV1(s.runtime.ContextOrBackground())
}

// GetTranscriptionCapabilitiesV1 reports whether local Whisper transcription
// can run: the resolved whisper-cli binary, the resolved model with its size,
// and the supported languages. Empty paths resolve the installed defaults, so
// the Settings panel can pass persisted overrides. It never fails: a missing
// binary or model is reported with guidance, not an error.
func (s *SystemService) GetTranscriptionCapabilitiesV1(whisperPath, modelPath string) models.TranscriptionCapabilitiesResponseV1 {
	return video.TranscriptionCapabilitiesV1(whisperPath, modelPath)
}

// GetLocalCapabilitiesV1 reports every external tool the local tools rely on,
// with availability, required/optional state and guidance, plus the installed
// OCR languages. It never fails: a missing tool is reported, not an error.
func (s *SystemService) GetLocalCapabilitiesV1() models.LocalCapabilitiesResponseV1 {
	return capabilities.Detect(s.runtime.ContextOrBackground())
}

// InspectFilesV1 describes files for UI cards. Missing files are reported per
// item instead of failing the batch.
func (s *SystemService) InspectFilesV1(paths []string) models.InspectFilesResponseV1 {
	return models.InspectFilesResponseV1{
		Success: true,
		Message: "files inspected",
		Files:   inspect.InspectFiles(paths),
	}
}

// NotifyV1 sends a native desktop notification. Notifications are optional:
// missing inputs or an unavailable notification service are reported, not fatal.
func (s *SystemService) NotifyV1(title, body string) models.PathActionResponseV1 {
	if strings.TrimSpace(title) == "" || strings.TrimSpace(body) == "" {
		return models.PathActionResponseV1{
			Success: false,
			Message: "A notification needs a title and a body.",
		}
	}
	if s.notifier == nil {
		return models.PathActionResponseV1{
			Success: false,
			Message: "Notifications are not available.",
		}
	}

	if err := s.notifier.Notify(title, body); err != nil {
		log.Printf("app.notify.failed err=%v", err)
		return models.PathActionResponseV1{Success: false, Message: err.Error()}
	}

	return models.PathActionResponseV1{Success: true, Message: "notification sent"}
}

// OpenPath opens an existing file with the operating system default handler.
func (s *SystemService) OpenPath(path string) models.PathActionResponseV1 {
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
func (s *SystemService) RevealInFolder(path string) models.PathActionResponseV1 {
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
func (s *SystemService) OpenFileDialog() (string, error) {
	app := application.Get()

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Select Image File")

	return dialog.PromptForSingleSelection()
}

// OpenMultipleFilesDialog opens a native file dialog and returns multiple selected file paths
func (s *SystemService) OpenMultipleFilesDialog() ([]string, error) {
	app := application.Get()

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Select Image Files")

	return dialog.PromptForMultipleSelection()
}

func (s *SystemService) OpenDirectoryDialog() (string, error) {
	app := application.Get()

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("Select Directory")
	dialog.CanChooseDirectories(true)
	dialog.CanChooseFiles(false)

	return dialog.PromptForSingleSelection()
}

func (s *SystemService) GetPDFPreviewSourceV1(inputPath string) models.PDFPreviewSourceResponseV1 {
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
func (s *SystemService) RegisterMediaPath(path string) models.MediaSourceResponseV1 {
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
