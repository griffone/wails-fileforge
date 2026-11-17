package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
)

// InstallEventNameV1 is the Wails event emitted while the toolchain installs.
const InstallEventNameV1 = "toolchain/install/v1"

// Install phases reported in status snapshots and progress events.
const (
	PhaseStarting    = "starting"
	PhaseMicromamba  = "micromamba"
	PhaseEnvironment = "environment"
	PhaseWhisper     = "whisper"
	PhaseModel       = "model"
	PhaseTessdata    = "tessdata"
	PhaseFinalize    = "finalize"
	PhaseReady       = "ready"
	PhaseCancelled   = "cancelled"
	PhaseIdle        = "idle"
)

const (
	micromambaDownloadStartPercent = 0
	micromambaDownloadEndPercent   = 5
	environmentEndPercent          = 40
	whisperDownloadStartPercent    = 40
	whisperDownloadEndPercent      = 47
	whisperEndPercent              = 50
	modelDownloadStartPercent      = 50
	modelDownloadEndPercent        = 93
	tessdataStartPercent           = 93
	readyPercent                   = 100

	condaPackagesMessage = "Installing FFmpeg, Tesseract, Poppler and Ghostscript…"
	versionProbeTimeout  = 1500 * time.Millisecond
)

// Service owns the managed toolchain: status verification, environment
// application and the cancelable install state machine. All state is guarded
// by a mutex; Status is safe to poll while an install runs.
type Service struct {
	dataDir    string
	root       string
	platform   Platform
	downloader Downloader
	runner     CommandRunner
	emit       func(models.ToolchainInstallEventV1)

	mu     sync.Mutex
	state  installState
	cancel context.CancelFunc

	versionsMu sync.Mutex
	versions   map[string]string
}

// installState is the mutable install snapshot merged into every status
// response.
type installState struct {
	installing   bool
	cancelling   bool
	percent      int
	phase        string
	detail       string
	err          string
	model        string
	ocrLanguages []string
}

// NewService builds the installer for one application data directory and
// platform. A nil downloader or runner uses the real implementations; a nil
// emitter disables events.
func NewService(dataDir string, platform Platform, downloader Downloader, runner CommandRunner, emit func(models.ToolchainInstallEventV1)) *Service {
	if downloader == nil {
		downloader = &HTTPDownloader{}
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	trimmedDataDir := strings.TrimSpace(dataDir)
	root := ""
	if trimmedDataDir != "" {
		root = ToolchainDir(filepath.Clean(trimmedDataDir))
	}

	return &Service{
		dataDir:    trimmedDataDir,
		root:       root,
		platform:   platform,
		downloader: downloader,
		runner:     runner,
		emit:       emit,
		versions:   make(map[string]string),
	}
}

// Root returns the managed toolchain directory.
func (s *Service) Root() string {
	if s == nil {
		return ""
	}

	return s.root
}

// Status verifies every managed component on disk and merges the current
// install state. Version probes are best-effort and never fail the status.
func (s *Service) Status() models.ToolchainStatusV1 {
	if s == nil {
		return models.ToolchainStatusV1{
			Success: false,
			Message: "The toolchain installer is not configured.",
		}
	}

	return s.statusSnapshot(true)
}

// statusSnapshot builds the status. Version probes are skipped on hot paths
// (startup readiness, install verification, start/cancel responses) because
// they spawn processes; the UI-facing Status fills them in.
func (s *Service) statusSnapshot(probeVersions bool) models.ToolchainStatusV1 {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return models.ToolchainStatusV1{
			Success: false,
			Message: "The toolchain installer is not configured.",
		}
	}

	s.mu.Lock()
	installing := s.state.installing
	cancelling := s.state.cancelling
	percent := s.state.percent
	phase := s.state.phase
	detail := s.state.detail
	errText := s.state.err
	s.mu.Unlock()

	model := s.preferredModel()
	languages := s.preferredLanguages()
	components := s.buildComponents(model, languages, probeVersions)

	ready := len(components) > 0
	for _, component := range components {
		if component.Required && !component.Ready {
			ready = false
			break
		}
	}

	status := models.ToolchainStatusV1{
		Success:      true,
		Message:      "Local tools are ready.",
		Ready:        ready,
		Installing:   installing,
		Cancelling:   cancelling,
		Percent:      percent,
		Phase:        phase,
		Detail:       detail,
		Error:        errText,
		StorageDir:   s.root,
		Components:   components,
		Model:        model,
		ModelPath:    modelFilePath(s.root, model),
		OCRLanguages: languages,
	}

	switch {
	case installing:
		status.Message = "Local tools are being installed."
	case ready:
		status.Percent = readyPercent
		status.Phase = PhaseReady
		status.Detail = "Local tools are ready."
		status.Error = ""
		status.Success = true
		status.Message = "Local tools are ready."
	case errText != "":
		status.Success = false
		status.Message = errText
	case len(status.Components) == 0:
		status.Success = false
		status.Message = "The toolchain installer is not configured."
	case !s.platform.Supported:
		status.Message = s.platform.Reason
		status.Detail = s.platform.Reason
		if status.Phase == "" {
			status.Phase = PhaseIdle
		}
	case phase == PhaseCancelled:
		status.Message = "The local toolchain install was cancelled."
	default:
		status.Message = "Local tools are not installed."
		if status.Phase == "" {
			status.Phase = PhaseIdle
			status.Detail = "Local tools are not installed."
		}
	}

	return status
}

// ApplyEnvironment prepends the managed binaries to this process PATH and
// points TESSDATA_PREFIX at the managed Tesseract data. It is idempotent and
// never touches the system environment.
func (s *Service) ApplyEnvironment() error {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return fmt.Errorf("the toolchain directory is not configured")
	}

	return applyManagedEnvironment(s.root, s.platform.OS, os.Getenv, os.Setenv)
}

// StartIfNeeded installs the default toolchain when it is not ready yet. It
// returns immediately: the install runs in the background and reports through
// events and Status.
func (s *Service) StartIfNeeded(ctx context.Context) {
	if s == nil || !s.platform.Supported {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.toolchainReady() {
		return
	}

	s.Start(ctx, models.ToolchainInstallOptionsV1{})
}

// toolchainReady verifies the required components without spawning version
// probes, so startup stays fast.
func (s *Service) toolchainReady() bool {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return false
	}

	components := s.buildComponents(s.preferredModel(), s.preferredLanguages(), false)
	if len(components) == 0 {
		return false
	}
	for _, component := range components {
		if component.Required && !component.Ready {
			return false
		}
	}

	return true
}

// Start validates the options and launches the install in one background
// goroutine. A second call while installing returns the current snapshot
// without starting a duplicate.
func (s *Service) Start(ctx context.Context, options models.ToolchainInstallOptionsV1) models.ToolchainStatusV1 {
	if s == nil {
		return models.ToolchainStatusV1{Success: false, Message: "The toolchain installer is not configured."}
	}

	model, modelErr := NormalizeModel(options.Model)
	if modelErr != nil {
		return s.failureStatus(modelErr.Error())
	}
	languages, languagesErr := NormalizeOCRLanguages(options.OCRLanguages)
	if languagesErr != nil {
		return s.failureStatus(languagesErr.Error())
	}
	if !s.platform.Supported {
		reason := s.platform.Reason
		if strings.TrimSpace(reason) == "" {
			reason = "The managed toolchain is not supported on this platform."
		}

		return s.failureStatus(reason)
	}
	if strings.TrimSpace(s.dataDir) == "" {
		return s.failureStatus("The application data directory is not configured.")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.state.installing {
		s.mu.Unlock()

		return s.statusSnapshot(false)
	}

	installContext, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.state = installState{
		installing:   true,
		percent:      0,
		phase:        PhaseStarting,
		detail:       "Preparing the local toolchain…",
		model:        model,
		ocrLanguages: append([]string{}, languages...),
	}
	s.mu.Unlock()

	s.emitEvent(models.ToolchainInstallEventV1{
		Phase:   PhaseStarting,
		Percent: 0,
		Message: "Preparing the local toolchain…",
	})

	go s.runInstall(installContext, model, languages, options.Force)

	return s.statusSnapshot(false)
}

// Cancel stops the active install or download. The install goroutine cleans the
// staging files and finishes with a cancelled state that is not ready.
func (s *Service) Cancel() models.ToolchainStatusV1 {
	if s == nil {
		return models.ToolchainStatusV1{Success: false, Message: "The toolchain installer is not configured."}
	}

	s.mu.Lock()
	if !s.state.installing {
		s.mu.Unlock()

		return s.statusSnapshot(false)
	}

	firstRequest := !s.state.cancelling
	s.state.cancelling = true
	if firstRequest {
		s.state.phase = "cancelling"
		s.state.detail = "Cancelling the installation…"
	}
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if firstRequest {
		s.emitEvent(models.ToolchainInstallEventV1{
			Phase:         "cancelling",
			Percent:       s.currentPercent(),
			Indeterminate: true,
			Message:       "Cancelling the installation…",
		})
	}

	return s.statusSnapshot(false)
}

func (s *Service) currentPercent() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state.percent
}

// runInstall is the single install goroutine. It never panics: a panic becomes a
// terminal error state.
func (s *Service) runInstall(ctx context.Context, model string, languages []string, force bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.finishError(fmt.Errorf("the toolchain install stopped unexpectedly: %v", recovered))
		}
	}()

	if err := s.install(ctx, model, languages, force); err != nil {
		if ctx.Err() != nil {
			s.finishCancelled()
			return
		}
		s.finishError(err)

		return
	}

	s.finishSuccess(model, languages)
}

func (s *Service) install(ctx context.Context, model string, languages []string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("create the toolchain folder: %w", err)
	}

	staging := stagingDir(s.root)
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fmt.Errorf("create the staging folder: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(staging)
	}()

	if err := s.installMicromamba(ctx, staging, force); err != nil {
		return err
	}
	if err := s.installEnvironment(ctx, force); err != nil {
		return err
	}
	if err := s.installWhisper(ctx, staging, force); err != nil {
		return err
	}
	if err := s.installModel(ctx, staging, model, force); err != nil {
		return err
	}
	if err := s.installTessdata(ctx, staging, languages, force); err != nil {
		return err
	}

	return s.finalize(model, languages)
}

func (s *Service) installMicromamba(ctx context.Context, staging string, force bool) error {
	target := micromambaPath(s.root, s.platform)
	if !force && fileWithContent(target) {
		s.progress(PhaseMicromamba, micromambaDownloadEndPercent, "micromamba is already installed.", false)

		return nil
	}

	s.progress(PhaseMicromamba, micromambaDownloadStartPercent, "Downloading micromamba…", false)
	tempPath := filepath.Join(staging, "micromamba-download")
	if err := s.downloader.Download(ctx, s.platform.MicromambaURL, tempPath, s.progressRange(
		PhaseMicromamba, "Downloading micromamba…",
		micromambaDownloadStartPercent, micromambaDownloadEndPercent,
	)); err != nil {
		return fmt.Errorf("download micromamba: %w", err)
	}

	if err := os.Chmod(tempPath, 0o755); err != nil {
		return fmt.Errorf("prepare micromamba: %w", err)
	}
	if err := replaceFile(tempPath, target); err != nil {
		return fmt.Errorf("install micromamba: %w", err)
	}

	s.progress(PhaseMicromamba, micromambaDownloadEndPercent, "micromamba installed.", false)

	return nil
}

func (s *Service) installEnvironment(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.progress(PhaseEnvironment, micromambaDownloadEndPercent, condaPackagesMessage, true)

	micromamba := micromambaPath(s.root, s.platform)
	if !fileExists(micromamba) {
		return fmt.Errorf("micromamba is missing at %s", micromamba)
	}

	prefix := envDir(s.root)
	if force {
		// A forced reinstall recreates the prefix so a corrupted environment is
		// repaired from scratch instead of patched.
		if err := os.RemoveAll(prefix); err != nil {
			return fmt.Errorf("replace the conda environment: %w", err)
		}
	}
	action := "create"
	if directoryExists(filepath.Join(prefix, "conda-meta")) {
		// A previous run created the prefix: install repairs missing packages
		// instead of recreating the environment from scratch.
		action = "install"
	}

	args := []string{
		action,
		"-y",
		"-p", prefix,
		"-c", "conda-forge",
		"ffmpeg", "tesseract", "poppler", "ghostscript",
	}
	environment := withEnvironment(os.Environ(), map[string]string{
		"MAMBA_ROOT_PREFIX": mambaRootDir(s.root),
	})

	if _, err := s.runner.Run(ctx, micromamba, args, environment); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("micromamba %s failed: %w", action, err)
	}

	s.progress(PhaseEnvironment, environmentEndPercent, "FFmpeg, Tesseract, Poppler and Ghostscript installed.", false)

	return nil
}

func (s *Service) installWhisper(ctx context.Context, staging string, force bool) error {
	targetDir := whisperDir(s.root)
	if !force && fileWithContent(whisperCLIPath(s.root, s.platform)) {
		s.progress(PhaseWhisper, whisperEndPercent, "whisper.cpp is already installed.", false)

		return nil
	}

	s.progress(PhaseWhisper, whisperDownloadStartPercent, "Downloading whisper.cpp…", false)
	archivePath := filepath.Join(staging, "whisper-archive")
	if err := s.downloader.Download(ctx, s.platform.WhisperURL, archivePath, s.progressRange(
		PhaseWhisper, "Downloading whisper.cpp…",
		whisperDownloadStartPercent, whisperDownloadEndPercent,
	)); err != nil {
		return fmt.Errorf("download whisper.cpp: %w", err)
	}

	s.progress(PhaseWhisper, whisperDownloadEndPercent, "Extracting whisper.cpp…", false)
	extractDir := filepath.Join(staging, "whisper-extract")
	if err := ExtractArchive(archivePath, extractDir, s.platform.WhisperArchive); err != nil {
		return err
	}

	newDir := targetDir + ".new"
	_ = os.RemoveAll(newDir)
	if err := FlattenSingleRoot(extractDir, newDir); err != nil {
		_ = os.RemoveAll(newDir)

		return err
	}
	if !fileWithContent(filepath.Join(newDir, s.platform.WhisperCLIName())) {
		_ = os.RemoveAll(newDir)

		return fmt.Errorf("the whisper.cpp archive did not contain %s", s.platform.WhisperCLIName())
	}
	if err := os.RemoveAll(targetDir); err != nil {
		_ = os.RemoveAll(newDir)

		return fmt.Errorf("replace whisper.cpp: %w", err)
	}
	if err := os.Rename(newDir, targetDir); err != nil {
		_ = os.RemoveAll(newDir)

		return fmt.Errorf("install whisper.cpp: %w", err)
	}

	s.progress(PhaseWhisper, whisperEndPercent, "whisper.cpp installed.", false)

	return nil
}

func (s *Service) installModel(ctx context.Context, staging, model string, force bool) error {
	target := modelFilePath(s.root, model)
	if !force && fileWithContent(target) {
		s.progress(PhaseModel, modelDownloadEndPercent, "The Whisper model is already installed.", false)

		return nil
	}

	url, err := ModelURL(model)
	if err != nil {
		return err
	}

	s.progress(PhaseModel, modelDownloadStartPercent, "Downloading the Whisper model…", false)
	tempPath := filepath.Join(staging, ModelFileName(model))
	if err := s.downloader.Download(ctx, url, tempPath, s.progressRange(
		PhaseModel, "Downloading the Whisper model…",
		modelDownloadStartPercent, modelDownloadEndPercent,
	)); err != nil {
		return fmt.Errorf("download the Whisper model: %w", err)
	}

	if err := replaceFile(tempPath, target); err != nil {
		return fmt.Errorf("install the Whisper model: %w", err)
	}

	s.progress(PhaseModel, modelDownloadEndPercent, "The Whisper model is installed.", false)

	return nil
}

func (s *Service) installTessdata(ctx context.Context, staging string, languages []string, force bool) error {
	if len(languages) == 0 {
		return fmt.Errorf("at least one OCR language is required")
	}

	for index, language := range languages {
		if err := ctx.Err(); err != nil {
			return err
		}

		startPercent := tessdataStartPercent + (index*(readyPercent-tessdataStartPercent))/len(languages)
		endPercent := tessdataStartPercent + ((index+1)*(readyPercent-tessdataStartPercent))/len(languages)
		message := fmt.Sprintf("Downloading Tesseract language data (%s)…", language)

		target := tessdataFilePath(s.root, language)
		if !force && fileWithContent(target) {
			s.progress(PhaseTessdata, endPercent, fmt.Sprintf("Tesseract language data (%s) is already installed.", language), false)

			continue
		}

		url, err := TessdataURL(language)
		if err != nil {
			return err
		}

		s.progress(PhaseTessdata, startPercent, message, false)
		tempPath := filepath.Join(staging, TessdataFileName(language))
		if err := s.downloader.Download(ctx, url, tempPath, s.progressRange(
			PhaseTessdata, message, startPercent, endPercent,
		)); err != nil {
			return fmt.Errorf("download Tesseract language data (%s): %w", language, err)
		}

		if err := replaceFile(tempPath, target); err != nil {
			return fmt.Errorf("install Tesseract language data (%s): %w", language, err)
		}

		s.progress(PhaseTessdata, endPercent, fmt.Sprintf("Tesseract language data (%s) is installed.", language), false)
	}

	return nil
}

func (s *Service) finalize(model string, languages []string) error {
	s.progress(PhaseFinalize, readyPercent, "Applying the local toolchain…", true)

	if err := s.ApplyEnvironment(); err != nil {
		return fmt.Errorf("apply the managed environment: %w", err)
	}

	status := s.Status()
	if !status.Ready {
		return fmt.Errorf("the toolchain verification failed: %s", missingComponentSummary(status.Components))
	}

	// The manifest is informational; status verifies the files directly.
	_ = writeManifest(s.root, model, languages, status.Components)

	return nil
}

// finishSuccess clears the install state, emits the terminal event and releases
// the install context.
func (s *Service) finishSuccess(model string, languages []string) {
	s.mu.Lock()
	s.state.installing = false
	s.state.cancelling = false
	s.state.percent = readyPercent
	s.state.phase = PhaseReady
	s.state.detail = "Local tools are ready."
	s.state.err = ""
	s.state.model = model
	s.state.ocrLanguages = append([]string{}, languages...)
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	s.emitEvent(models.ToolchainInstallEventV1{
		Phase:   PhaseReady,
		Percent: readyPercent,
		Message: "Local tools are ready.",
		Done:    true,
	})
}

// finishError records the failure and emits a terminal event with the error.
// Components installed before the failure stay available for a retry.
func (s *Service) finishError(installErr error) {
	message := "The local toolchain could not be installed."
	if installErr != nil && strings.TrimSpace(installErr.Error()) != "" {
		message = installErr.Error()
	}

	s.mu.Lock()
	s.state.installing = false
	s.state.cancelling = false
	s.state.err = message
	phase := s.state.phase
	percent := s.state.percent
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	s.emitEvent(models.ToolchainInstallEventV1{
		Phase:   phase,
		Percent: percent,
		Message: "The local toolchain could not be installed.",
		Done:    true,
		Error:   message,
	})
}

// finishCancelled records a cancelled install. It is not an error, but the
// toolchain is never marked ready.
func (s *Service) finishCancelled() {
	s.mu.Lock()
	s.state.installing = false
	s.state.cancelling = false
	s.state.phase = PhaseCancelled
	s.state.detail = "Installation cancelled."
	s.state.err = ""
	percent := s.state.percent
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	s.emitEvent(models.ToolchainInstallEventV1{
		Phase:   PhaseCancelled,
		Percent: percent,
		Message: "Installation cancelled.",
		Done:    true,
	})
}

func (s *Service) progress(phase string, percent int, message string, indeterminate bool) {
	if percent < 0 {
		percent = 0
	}
	if percent > readyPercent {
		percent = readyPercent
	}

	s.mu.Lock()
	if !s.state.installing {
		s.mu.Unlock()

		return
	}
	s.state.phase = phase
	s.state.percent = percent
	s.state.detail = message
	s.mu.Unlock()

	s.emitEvent(models.ToolchainInstallEventV1{
		Phase:         phase,
		Percent:       percent,
		Indeterminate: indeterminate,
		Message:       message,
	})
}

// progressRange maps byte progress into a percent range, emitting only when the
// percent changes. An unknown total stays at the range start and percents are
// marked indeterminate.
func (s *Service) progressRange(phase, message string, startPercent, endPercent int) func(downloaded, total int64) {
	lastPercent := -1

	return func(downloaded, total int64) {
		if total <= 0 {
			if lastPercent != startPercent {
				lastPercent = startPercent
				s.progress(phase, startPercent, message, true)
			}

			return
		}

		percent := startPercent
		if downloaded > 0 {
			percent = startPercent + int(float64(downloaded)/float64(total)*float64(endPercent-startPercent))
		}
		if percent < startPercent {
			percent = startPercent
		}
		if percent > endPercent {
			percent = endPercent
		}
		if percent == lastPercent {
			return
		}

		lastPercent = percent
		s.progress(phase, percent, message, false)
	}
}

func (s *Service) failureStatus(message string) models.ToolchainStatusV1 {
	status := s.statusSnapshot(false)
	status.Success = false
	status.Message = message
	status.Error = message

	return status
}

func (s *Service) emitEvent(event models.ToolchainInstallEventV1) {
	if s == nil || s.emit == nil {
		return
	}

	defer func() {
		// A broken event bridge must never abort an install.
		_ = recover()
	}()

	s.emit(event)
}

func (s *Service) preferredModel() string {
	s.mu.Lock()
	model := s.state.model
	s.mu.Unlock()

	if model != "" {
		return model
	}

	if manifest, err := readManifest(manifestFilePath(s.root)); err == nil {
		if normalized, normalizeErr := NormalizeModel(manifest.Model); normalizeErr == nil {
			return normalized
		}
	}

	return DefaultModel
}

func (s *Service) preferredLanguages() []string {
	s.mu.Lock()
	languages := append([]string{}, s.state.ocrLanguages...)
	s.mu.Unlock()

	if len(languages) > 0 {
		return languages
	}

	if manifest, err := readManifest(manifestFilePath(s.root)); err == nil && len(manifest.OCRLanguages) > 0 {
		if normalized, normalizeErr := NormalizeOCRLanguages(manifest.OCRLanguages); normalizeErr == nil {
			return normalized
		}
	}

	return SupportedOCRLanguages()
}

type managedBinarySpec struct {
	id       string
	label    string
	names    []string
	required bool
	detail   string
}

var managedBinarySpecs = []managedBinarySpec{
	{id: "ffmpeg", label: "FFmpeg", names: []string{"ffmpeg"}, required: true, detail: "Video conversion, trimming and merging"},
	{id: "ffprobe", label: "FFprobe", names: []string{"ffprobe"}, required: true, detail: "Video metadata and durations"},
	{id: "tesseract", label: "Tesseract", names: []string{"tesseract"}, required: true, detail: "Local OCR engine"},
	{id: "pdftoppm", label: "pdftoppm", names: []string{"pdftoppm"}, required: true, detail: "OCR page rasterizer"},
	{id: "pdftotext", label: "pdftotext", names: []string{"pdftotext"}, required: false, detail: "PDF text layer extraction"},
	{id: "ghostscript", label: "Ghostscript", names: []string{"gs", "gswin64c", "gswin32c"}, required: false, detail: "PDF compression for target sizes"},
}

var versionArgsByComponent = map[string][]string{
	"micromamba":  {"--version"},
	"ffmpeg":      {"-version"},
	"tesseract":   {"--version"},
	"ghostscript": {"--version"},
}

func (s *Service) buildComponents(model string, languages []string, probeVersions bool) []models.ToolchainComponentV1 {
	components := make([]models.ToolchainComponentV1, 0, len(managedBinarySpecs)+5)

	micromamba := micromambaPath(s.root, s.platform)
	micromambaComponent := models.ToolchainComponentV1{
		ID:       "micromamba",
		Label:    "micromamba",
		Ready:    fileWithContent(micromamba),
		Required: false,
		Detail:   "Portable package manager",
	}
	if micromambaComponent.Ready {
		micromambaComponent.Path = micromamba
		if probeVersions {
			micromambaComponent.Version = s.versionFor("micromamba", micromamba)
		}
	}
	components = append(components, micromambaComponent)

	for _, spec := range managedBinarySpecs {
		component := models.ToolchainComponentV1{
			ID:       spec.id,
			Label:    spec.label,
			Required: spec.required,
			Detail:   spec.detail,
		}
		if path, found := managedBinaryPath(s.root, s.platform.OS, spec.names); found {
			component.Ready = true
			component.Path = path
			if probeVersions {
				component.Version = s.versionFor(spec.id, path)
			}
		}
		components = append(components, component)
	}

	cliPath := whisperCLIPath(s.root, s.platform)
	cliComponent := models.ToolchainComponentV1{
		ID:       "whisper-cli",
		Label:    "Whisper runtime",
		Ready:    fileWithContent(cliPath),
		Required: true,
		Detail:   "Local whisper.cpp transcription runtime",
	}
	if cliComponent.Ready {
		cliComponent.Path = cliPath
	}
	components = append(components, cliComponent)

	modelPath := modelFilePath(s.root, model)
	modelComponent := models.ToolchainComponentV1{
		ID:       "whisper-model",
		Label:    "Whisper model",
		Ready:    fileWithContent(modelPath),
		Required: true,
		Path:     modelPath,
		Detail:   fmt.Sprintf("Whisper %s model", model),
	}
	components = append(components, modelComponent)

	for _, language := range SupportedOCRLanguages() {
		path := tessdataFilePath(s.root, language)
		components = append(components, models.ToolchainComponentV1{
			ID:       "tesseract-" + language,
			Label:    "Tesseract " + language,
			Ready:    fileWithContent(path),
			Required: containsLanguage(languages, language),
			Path:     path,
			Detail:   fmt.Sprintf("Tesseract %s language data", language),
		})
	}

	return components
}

// versionFor probes a component version with a short timeout and caches the
// result. A failed probe is reported as an empty version, never as an error.
func (s *Service) versionFor(componentID, path string) string {
	args, known := versionArgsByComponent[componentID]
	if !known || strings.TrimSpace(path) == "" {
		return ""
	}

	s.versionsMu.Lock()
	cached, cachedOK := s.versions[path]
	s.versionsMu.Unlock()
	if cachedOK {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionProbeTimeout)
	defer cancel()

	output, err := s.runner.Run(ctx, path, args, nil)
	if err != nil {
		return ""
	}

	version := firstNonEmptyLine(string(output))
	if version == "" {
		return ""
	}

	s.versionsMu.Lock()
	s.versions[path] = version
	s.versionsMu.Unlock()

	return version
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func containsLanguage(languages []string, target string) bool {
	for _, language := range languages {
		if language == target {
			return true
		}
	}

	return false
}

func missingComponentSummary(components []models.ToolchainComponentV1) string {
	missing := make([]string, 0, len(components))
	for _, component := range components {
		if component.Required && !component.Ready {
			missing = append(missing, component.Label)
		}
	}
	if len(missing) == 0 {
		return "unknown component"
	}

	return strings.Join(missing, ", ")
}

// manifestV1 is the informational install record. Status still verifies the
// files directly, so a stale or missing manifest never marks components ready.
type manifestV1 struct {
	Version      int                   `json:"version"`
	GeneratedAt  string                `json:"generatedAt"`
	Model        string                `json:"model"`
	OCRLanguages []string              `json:"ocrLanguages"`
	Components   []manifestComponentV1 `json:"components"`
}

type manifestComponentV1 struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

func writeManifest(root, model string, languages []string, components []models.ToolchainComponentV1) error {
	manifest := manifestV1{
		Version:      1,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Model:        model,
		OCRLanguages: append([]string{}, languages...),
		Components:   make([]manifestComponentV1, 0, len(components)),
	}
	for _, component := range components {
		if !component.Ready {
			continue
		}

		manifest.Components = append(manifest.Components, manifestComponentV1{
			ID:      component.ID,
			Path:    component.Path,
			Version: component.Version,
		})
	}

	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	target := manifestFilePath(root)
	tempFile, err := os.CreateTemp(filepath.Dir(target), ".fileforge-manifest-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	published := false
	defer func() {
		_ = tempFile.Close()
		if !published {
			_ = os.Remove(tempPath)
		}
	}()

	if _, writeErr := tempFile.Write(payload); writeErr != nil {
		return writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return closeErr
	}
	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		return chmodErr
	}
	if renameErr := replaceFile(tempPath, target); renameErr != nil {
		return renameErr
	}

	published = true

	return nil
}

func readManifest(path string) (manifestV1, error) {
	var manifest manifestV1

	payload, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return manifestV1{}, err
	}

	return manifest, nil
}
