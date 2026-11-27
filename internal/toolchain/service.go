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
	"fileforge-desktop/internal/platform/fsx"
)

// InstallEventName is the Wails event emitted while the toolchain installs.
const InstallEventName = "toolchain/install"

// Install phases reported in status snapshots and progress events.
const (
	PhaseStarting    = "starting"
	PhaseMicromamba  = "micromamba"
	PhaseEnvironment = "environment"
	PhaseDownloader  = "downloader"
	PhaseWhisper     = "whisper"
	PhaseModel       = "model"
	PhaseTessdata    = "tessdata"
	PhasePdf2Docx    = "pdf2docx"
	PhaseONNX        = "onnx"
	PhaseRembg       = "rembg"
	PhaseUpscale     = "upscale"
	PhaseFinalize    = "finalize"
	PhaseReady       = "ready"
	PhaseCancelled   = "cancelled"
	PhaseIdle        = "idle"
)

// Percent ranges partition the install progress bar. Each phase starts where the
// previous one ends so the reported percent never goes backwards.
const (
	micromambaDownloadStartPercent = 0
	micromambaDownloadEndPercent   = 5
	environmentEndPercent          = 37
	downloaderStartPercent         = 37
	downloaderEndPercent           = 38
	whisperDownloadStartPercent    = 38
	whisperDownloadEndPercent      = 45
	whisperEndPercent              = 48
	modelDownloadStartPercent      = 48
	modelDownloadEndPercent        = 88
	tessdataStartPercent           = 88
	tessdataEndPercent             = 91
	pdf2docxStartPercent           = 91
	pdf2docxInstallPercent         = 92
	pdf2docxEndPercent             = 94
	onnxVenvStartPercent           = 94
	onnxVenvInstallPercent         = 95
	onnxVenvEndPercent             = 96
	rembgModelStartPercent         = 96
	rembgModelEndPercent           = 97
	upscaleModelStartPercent       = 97
	upscaleModelEndPercent         = 98
	upscaleAnimeModelStartPercent  = 98
	upscaleAnimeModelEndPercent    = 99
	readyPercent                   = 100

	condaPackagesMessage = "Installing FFmpeg, Tesseract, Poppler, Ghostscript and Python…"
	versionProbeTimeout  = 1500 * time.Millisecond

	// rembgImportTimeout bounds the import smoke test. Importing onnxruntime is
	// the slowest step, so a generous but finite timeout catches a broken venv.
	rembgImportTimeout = 90 * time.Second
)

// pdf2docxPins is the validated package set installed into the dedicated PDF to
// DOCX virtual environment. These versions are tested together, so they are
// pinned instead of resolved at install time.
var pdf2docxPins = []string{
	"numpy==1.26.4",
	"opencv-python-headless==4.10.0.84",
	"PyMuPDF==1.26.7",
	"python-docx==1.1.2",
	"fonttools==4.53.1",
	"pdf2docx==0.5.13",
}

// rembgPins is the validated package set installed into the shared ONNX
// virtual environment. rembg pulls onnxruntime for CPU inference, so the
// runtime, the array/image stack and its transitive pins are fixed rather than
// resolved at install time. The upscale engine imports onnxruntime, numpy and
// Pillow; all three appear here with the exact versions upscalePins pins, so
// one environment serves both engines.
var rembgPins = []string{
	"rembg[cpu]==2.0.87",
	"onnxruntime==1.32.0",
	"numpy==2.3.5",
	"scipy==1.16.3",
	"pillow==12.3.0",
	"numba==0.68.1",
	"llvmlite==0.50.0",
	"scikit-image==0.26.0",
	"pymatting==1.1.16",
	"pooch==1.9.0",
	"tqdm==4.70.1",
	"jsonschema==4.26.0",
}

// downloaderPins is the package set installed into the managed conda Python
// environment for URL downloads. yt-dlp is deliberately unpinned: YouTube
// extraction changes constantly and an older release stops returning every
// format, so the installer always asks pip for the current release. The "deno"
// extra installs the yt-dlp-ejs challenge scripts and the Deno JavaScript
// runtime that yt-dlp >= 2025.11 needs; without it YouTube formats are
// truncated to low resolutions.
var downloaderPins = []string{
	"yt-dlp[default,deno]",
}

// Service owns the managed toolchain: status verification, environment
// application and the cancelable install state machine. All state is guarded
// by a mutex; Status is safe to poll while an install runs.
type Service struct {
	dataDir    string
	root       string
	platform   Platform
	downloader Downloader
	runner     CommandRunner
	emit       func(models.ToolchainInstallEvent)

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

	// componentErrors holds the failure of a standard component (the ONNX
	// engine and its models) so a partial install can surface the exact
	// component instead of an opaque setup failure.
	componentErrors map[string]string
}

// NewService builds the installer for one application data directory and
// platform. A nil downloader or runner uses the real implementations; a nil
// emitter disables events.
func NewService(dataDir string, platform Platform, downloader Downloader, runner CommandRunner, emit func(models.ToolchainInstallEvent)) *Service {
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
func (s *Service) Status() models.ToolchainStatus {
	if s == nil {
		return models.ToolchainStatus{
			Success: false,
			Message: "The toolchain installer is not configured.",
		}
	}

	return s.statusSnapshot(true)
}

// statusSnapshot builds the status. Version probes are skipped on hot paths
// (startup readiness, install verification, start/cancel responses) because
// they spawn processes; the UI-facing Status fills them in.
func (s *Service) statusSnapshot(probeVersions bool) models.ToolchainStatus {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return models.ToolchainStatus{
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

	status := models.ToolchainStatus{
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

	s.Start(ctx, models.ToolchainInstallOptions{})
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
func (s *Service) Start(ctx context.Context, options models.ToolchainInstallOptions) models.ToolchainStatus {
	if s == nil {
		return models.ToolchainStatus{Success: false, Message: "The toolchain installer is not configured."}
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
		installing:      true,
		percent:         0,
		phase:           PhaseStarting,
		detail:          "Preparing the local toolchain…",
		model:           model,
		ocrLanguages:    append([]string{}, languages...),
		componentErrors: make(map[string]string),
	}
	s.mu.Unlock()

	s.emitEvent(models.ToolchainInstallEvent{
		Phase:   PhaseStarting,
		Percent: 0,
		Message: "Preparing the local toolchain…",
	})

	go s.runInstall(installContext, model, languages, options.Force)

	return s.statusSnapshot(false)
}

// Cancel stops the active install or download. The install goroutine cleans the
// staging files and finishes with a cancelled state that is not ready.
func (s *Service) Cancel() models.ToolchainStatus {
	if s == nil {
		return models.ToolchainStatus{Success: false, Message: "The toolchain installer is not configured."}
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
		s.emitEvent(models.ToolchainInstallEvent{
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
	if err := s.installDownloader(ctx, force); err != nil {
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
	if err := s.installPdf2Docx(ctx, force); err != nil {
		return err
	}

	// The ONNX engine and its models are part of the standard install, but each
	// one is an independent component: a failure is recorded against it and the
	// rest of the toolchain still finishes, so the app starts degraded instead
	// of failing as a whole.
	if err := s.installONNXPacks(ctx, force); err != nil {
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
	if err := s.downloader.DownloadArtifact(ctx, s.platform.MicromambaArtifact(), tempPath, s.progressRange(
		PhaseMicromamba, "Downloading micromamba…",
		micromambaDownloadStartPercent, micromambaDownloadEndPercent,
	)); err != nil {
		return fmt.Errorf("download micromamba: %w", err)
	}

	if err := os.Chmod(tempPath, 0o755); err != nil {
		return fmt.Errorf("prepare micromamba: %w", err)
	}
	if err := fsx.EnsureDir(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("install micromamba: %w", err)
	}
	if err := fsx.ReplaceFile(tempPath, target); err != nil {
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
	if !fsx.IsRegularFile(micromamba) {
		return fmt.Errorf("micromamba is missing at %s", micromamba)
	}

	prefix := envDir(s.root)
	if force {
		// A forced reinstall recreates the prefix so a corrupted environment is
		// repaired from scratch instead of patched. The virtual environments are
		// derived from this prefix, so they are recreated too. The RemBG model
		// folder is kept: re-downloading ~179 MB on every repair is wasteful.
		if err := os.RemoveAll(prefix); err != nil {
			return fmt.Errorf("replace the conda environment: %w", err)
		}
		if err := os.RemoveAll(pdf2docxVenvDir(s.root)); err != nil {
			return fmt.Errorf("replace the PDF to DOCX environment: %w", err)
		}
		if err := os.RemoveAll(onnxVenvDir(s.root)); err != nil {
			return fmt.Errorf("replace the shared ONNX environment: %w", err)
		}
		if err := os.Remove(onnxReadyFilePath(s.root)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reset the shared ONNX readiness marker: %w", err)
		}
		s.removeLegacyVenvs()
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
		"ffmpeg", "tesseract", "poppler", "ghostscript", "python=3.11", "pip",
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

	s.progress(PhaseEnvironment, environmentEndPercent, "FFmpeg, Tesseract, Poppler, Ghostscript and Python installed.", false)

	return nil
}

// installDownloader installs yt-dlp with its JavaScript runtime into the
// managed conda Python environment. The packages live in that prefix on
// purpose: the downloader tool resolves them through the managed PATH, and the
// prefix is recreated on a forced install, which also upgrades the unpinned
// yt-dlp release.
func (s *Service) installDownloader(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	basePython := managedBasePythonPath(s.root, s.platform.OS)
	if !fileWithContent(basePython) {
		return fmt.Errorf("the managed Python is missing; reinstall the toolchain")
	}

	binaries := []string{"yt-dlp", "deno"}
	if !force && len(missingManagedBinaries(s.root, s.platform.OS, binaries)) == 0 {
		s.progress(PhaseDownloader, downloaderEndPercent, "The media downloader is already installed.", false)

		return nil
	}

	environment := withEnvironment(os.Environ(), map[string]string{
		"PATH": prependPath(os.Getenv("PATH"), managedPathDirs(s.root, s.platform.OS), pathListSeparatorFor(s.platform.OS)),
	})

	s.progress(PhaseDownloader, downloaderStartPercent, "Installing the media downloader…", false)
	pipArgs := append([]string{"-m", "pip", "install", "--no-cache-dir", "--disable-pip-version-check", "--upgrade"}, downloaderPins...)
	if _, err := s.runner.Run(ctx, basePython, pipArgs, environment); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("install the media downloader: %w", err)
	}

	// yt-dlp and Deno are installed by the same pip run, but each one is a
	// separate managed binary. Name the missing side so a partial install
	// explains exactly what broke instead of a generic failure.
	if missing := missingManagedBinaries(s.root, s.platform.OS, binaries); len(missing) > 0 {
		return fmt.Errorf("the media downloader did not install correctly: missing %s", strings.Join(missing, " and "))
	}

	s.progress(PhaseDownloader, downloaderEndPercent, "Media downloader is ready.", false)

	return nil
}

// missingManagedBinaries returns the requested names that do not resolve in the
// managed prefix binary folders, preserving the caller's order.
func missingManagedBinaries(root, goos string, names []string) []string {
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if _, found := managedBinaryPath(root, goos, []string{name}); !found {
			missing = append(missing, name)
		}
	}

	return missing
}

func (s *Service) installWhisper(ctx context.Context, staging string, force bool) error {
	targetDir := whisperDir(s.root)
	if !force && whisperRuntimeReadyInDir(targetDir, s.platform) {
		s.progress(PhaseWhisper, whisperEndPercent, "whisper.cpp is already installed.", false)

		return nil
	}

	s.progress(PhaseWhisper, whisperDownloadStartPercent, "Downloading whisper.cpp…", false)
	archivePath := filepath.Join(staging, "whisper-archive")
	if err := s.downloader.DownloadArtifact(ctx, s.platform.WhisperArtifact(), archivePath, s.progressRange(
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
	if !whisperRuntimeReadyInDir(newDir, s.platform) {
		_ = os.RemoveAll(newDir)

		return fmt.Errorf("the whisper.cpp archive did not contain a usable runtime (%s and its shared libraries)", s.platform.WhisperCLIName())
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

	artifact, err := ModelArtifact(model)
	if err != nil {
		return err
	}

	s.progress(PhaseModel, modelDownloadStartPercent, "Downloading the Whisper model…", false)
	tempPath := filepath.Join(staging, artifact.FileName)
	if err := s.downloader.DownloadArtifact(ctx, artifact, tempPath, s.progressRange(
		PhaseModel, "Downloading the Whisper model…",
		modelDownloadStartPercent, modelDownloadEndPercent,
	)); err != nil {
		return fmt.Errorf("download the Whisper model: %w", err)
	}

	if err := fsx.EnsureDir(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("install the Whisper model: %w", err)
	}
	if err := fsx.ReplaceFile(tempPath, target); err != nil {
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

		startPercent := tessdataStartPercent + (index*(tessdataEndPercent-tessdataStartPercent))/len(languages)
		endPercent := tessdataStartPercent + ((index+1)*(tessdataEndPercent-tessdataStartPercent))/len(languages)
		message := fmt.Sprintf("Downloading Tesseract language data (%s)…", language)

		target := tessdataFilePath(s.root, language)
		if !force && fileWithContent(target) {
			s.progress(PhaseTessdata, endPercent, fmt.Sprintf("Tesseract language data (%s) is already installed.", language), false)

			continue
		}

		artifact, err := TessdataArtifact(language)
		if err != nil {
			return err
		}

		s.progress(PhaseTessdata, startPercent, message, false)
		tempPath := filepath.Join(staging, artifact.FileName)
		if err := s.downloader.DownloadArtifact(ctx, artifact, tempPath, s.progressRange(
			PhaseTessdata, message, startPercent, endPercent,
		)); err != nil {
			return fmt.Errorf("download Tesseract language data (%s): %w", language, err)
		}

		if err := fsx.EnsureDir(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("install Tesseract language data (%s): %w", language, err)
		}
		if err := fsx.ReplaceFile(tempPath, target); err != nil {
			return fmt.Errorf("install Tesseract language data (%s): %w", language, err)
		}

		s.progress(PhaseTessdata, endPercent, fmt.Sprintf("Tesseract language data (%s) is installed.", language), false)
	}

	return s.installTessdataPDFAssets(force)
}

// installTessdataPDFAssets writes the embedded Tesseract PDF config and font
// into the managed tessdata folder. Without them tesseract exits successfully
// but writes no searchable PDF, and the OCR pipeline later fails while merging
// the missing page files.
//
// The assets are embedded because the conda-forge win-64 tesseract package
// omits pdf.ttf; relying on the conda prefix made Windows installs fail.
func (s *Service) installTessdataPDFAssets(force bool) error {
	targetDir := tessdataDir(s.root)

	for _, relative := range tessdataPDFAssets {
		target := filepath.Join(targetDir, relative)
		if !force && fileWithContent(target) {
			continue
		}

		assetPath := "assets/" + filepath.ToSlash(relative)
		payload, err := tessdataPDFAssetsFS.ReadFile(assetPath)
		if err != nil {
			return fmt.Errorf("read embedded Tesseract PDF asset %s: %w", relative, err)
		}
		if err := fsx.WriteAtomic(target, payload, 0o644); err != nil {
			return fmt.Errorf("install Tesseract PDF support (%s): %w", relative, err)
		}
	}

	s.progress(PhaseTessdata, tessdataEndPercent, "Tesseract PDF support is installed.", false)

	return nil
}

// installPdf2Docx creates the dedicated virtual environment for the PDF to DOCX
// engine and installs the pinned package set into it. The conda prefix only
// contributes the base interpreter: installing these packages there would
// upgrade shared libraries used by the rest of the toolchain.
func (s *Service) installPdf2Docx(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	basePython := managedBasePythonPath(s.root, s.platform.OS)
	if !fileWithContent(basePython) {
		return fmt.Errorf("the managed Python is missing; reinstall the toolchain")
	}

	venv := pdf2docxVenvDir(s.root)
	venvPython := pdf2docxVenvPythonPath(s.root, s.platform.OS)
	venvBinary := pdf2docxVenvBinaryPath(s.root, s.platform.OS)
	if !force && fileWithContent(venvPython) && fileWithContent(venvBinary) {
		s.progress(PhasePdf2Docx, pdf2docxEndPercent, "The PDF to DOCX engine is already installed.", false)

		return nil
	}

	if err := os.RemoveAll(venv); err != nil {
		return fmt.Errorf("replace the PDF to DOCX environment: %w", err)
	}

	environment := withEnvironment(os.Environ(), map[string]string{
		"PATH": prependPath(os.Getenv("PATH"), managedPathDirs(s.root, s.platform.OS), pathListSeparatorFor(s.platform.OS)),
	})

	s.progress(PhasePdf2Docx, pdf2docxStartPercent, "Creating the PDF to DOCX environment…", false)
	if _, err := s.runner.Run(ctx, basePython, []string{"-m", "venv", venv}, environment); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("create the PDF to DOCX environment: %w", err)
	}

	s.progress(PhasePdf2Docx, pdf2docxInstallPercent, "Installing the PDF to DOCX engine…", false)
	pipArgs := append([]string{"-m", "pip", "install", "--no-cache-dir", "--disable-pip-version-check"}, pdf2docxPins...)
	if _, err := s.runner.Run(ctx, venvPython, pipArgs, environment); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("install the PDF to DOCX engine: %w", err)
	}

	if !fileWithContent(venvPython) || !fileWithContent(venvBinary) {
		return fmt.Errorf("the PDF to DOCX engine did not install correctly: the virtual environment is incomplete")
	}

	s.progress(PhasePdf2Docx, pdf2docxEndPercent, "PDF to DOCX engine is ready.", false)

	return nil
}

// componentIDsONNXVenv lists the components that depend on the shared ONNX
// virtual environment. A venv failure is recorded against all of them.
var componentIDsONNXVenv = []string{"rembg", "upscale"}

// installONNXPacks creates the shared ONNX environment once and installs the
// pinned models for both engines. Each engine and each model is an independent
// component: a failure is recorded against the affected ids and the install
// continues, so one broken download never blocks the rest of the toolchain.
func (s *Service) installONNXPacks(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := s.ensureONNXVenv(ctx, force); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordComponentFailure(err.Error(), componentIDsONNXVenv...)

		return nil
	}
	s.clearComponentFailure(componentIDsONNXVenv...)

	if err := s.installRembgModel(ctx, force); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordComponentFailure(err.Error(), "rembg-model")
	} else {
		s.clearComponentFailure("rembg-model")
	}

	if err := s.installUpscaleModel(ctx, force); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordComponentFailure(err.Error(), "upscale-model")
	} else {
		s.clearComponentFailure("upscale-model")
	}

	if err := s.installUpscaleAnimeModel(ctx, force); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		s.recordComponentFailure(err.Error(), "upscale-anime-model")
	} else {
		s.clearComponentFailure("upscale-anime-model")
	}

	return nil
}

// ensureONNXVenv creates the virtual environment shared by background removal
// and upscaling, installs the pinned package set and verifies it with one real
// import. Both engines import the same ONNX stack, so a second venv would only
// duplicate 150–200 MB. The weights live outside the venv, so repairing the
// engine never discards downloaded models.
func (s *Service) ensureONNXVenv(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	basePython := managedBasePythonPath(s.root, s.platform.OS)
	if !fileWithContent(basePython) {
		return fmt.Errorf("the managed Python is missing; reinstall the toolchain")
	}

	venv := onnxVenvDir(s.root)
	venvPython := onnxVenvPythonPath(s.root, s.platform.OS)
	readyMarker := onnxReadyFilePath(s.root)
	if !force && fileWithContent(venvPython) && fileWithContent(readyMarker) && onnxPackagesInstalled(s.root, s.platform.OS) {
		s.progress(PhaseONNX, onnxVenvEndPercent, "The shared ONNX engine is already installed.", false)
		s.removeLegacyVenvs()

		return nil
	}

	if err := os.RemoveAll(venv); err != nil {
		return fmt.Errorf("replace the shared ONNX environment: %w", err)
	}
	// A rebuilt venv must prove itself again: leaving the old marker behind
	// would let a failed pip or import hide behind a stale success.
	if err := os.Remove(readyMarker); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset the shared ONNX readiness marker: %w", err)
	}
	s.removeLegacyVenvs()

	environment := withEnvironment(os.Environ(), map[string]string{
		"PATH": prependPath(os.Getenv("PATH"), managedPathDirs(s.root, s.platform.OS), pathListSeparatorFor(s.platform.OS)),
	})

	s.progress(PhaseONNX, onnxVenvStartPercent, "Creating the shared ONNX engine…", false)
	if _, err := s.runner.Run(ctx, basePython, []string{"-m", "venv", venv}, environment); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("create the shared ONNX environment: %w", err)
	}

	s.progress(PhaseONNX, onnxVenvInstallPercent, "Installing the shared ONNX engine…", false)
	pipArgs := append([]string{"-m", "pip", "install", "--no-cache-dir", "--disable-pip-version-check"}, rembgPins...)
	if _, err := s.runner.Run(ctx, venvPython, pipArgs, environment); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("install the shared ONNX engine: %w", err)
	}

	if !fileWithContent(venvPython) || !onnxPackagesInstalled(s.root, s.platform.OS) {
		return fmt.Errorf("the shared ONNX engine did not install correctly: the virtual environment is incomplete")
	}

	// The marker is written only after a real import succeeds, so a venv with
	// missing shared libraries is never reported as ready. The import covers
	// both engines: rembg for background removal, onnxruntime/numpy/PIL for
	// upscaling.
	importContext, cancel := context.WithTimeout(ctx, rembgImportTimeout)
	_, importErr := s.runner.Run(importContext, venvPython, []string{"-c", "import rembg, onnxruntime, numpy, PIL"}, environment)
	cancel()
	if importErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		return fmt.Errorf("the shared ONNX engine could not be imported: %w", importErr)
	}

	if err := os.MkdirAll(rembgHomeDir(s.root), 0o755); err != nil {
		return fmt.Errorf("create the background removal home: %w", err)
	}
	if err := os.MkdirAll(upscaleHomeDir(s.root), 0o755); err != nil {
		return fmt.Errorf("create the upscale home: %w", err)
	}
	if err := os.WriteFile(readyMarker, []byte("onnxruntime 1.32.0\n"), 0o644); err != nil {
		return fmt.Errorf("mark the shared ONNX engine as ready: %w", err)
	}

	s.progress(PhaseONNX, onnxVenvEndPercent, "The shared ONNX engine is verified.", false)

	return nil
}

// removeLegacyVenvs deletes the per-engine virtual environments used before the
// shared ONNX venv existed. It reclaims 150 MB or more on upgraded installs;
// the downloaded models live elsewhere and are kept.
func (s *Service) removeLegacyVenvs() {
	for _, legacy := range []string{"rembg-venv", "upscale-venv"} {
		_ = os.RemoveAll(filepath.Join(s.root, legacy))
	}
}

// recordComponentFailure stores the failure of standard components so Status
// can report each one and the UI can offer a targeted retry.
func (s *Service) recordComponentFailure(message string, ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.componentErrors == nil {
		s.state.componentErrors = make(map[string]string, len(ids))
	}
	for _, id := range ids {
		s.state.componentErrors[id] = message
	}
}

func (s *Service) clearComponentFailure(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, id := range ids {
		delete(s.state.componentErrors, id)
	}
}

func (s *Service) componentFailure(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state.componentErrors[id]
}

// installRembgModel downloads the pinned model with size and checksum
// verification. A failed download or a bad checksum leaves the previous model
// untouched, so a botched repair never destroys a working installation.
func (s *Service) installRembgModel(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	spec := rembgModelSpec
	target := rembgModelPath(s.root, spec.Model)
	if fileHasExactSize(target, spec.SizeBytes) && (!force || verifyRembgModel(target, spec) == nil) {
		s.progress(PhaseRembg, rembgModelEndPercent, "The background removal model is already installed.", false)

		return nil
	}

	s.progress(PhaseRembg, rembgModelStartPercent, "Downloading the background removal model…", false)
	modelDir := filepath.Dir(target)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return fmt.Errorf("create the background removal model folder: %w", err)
	}

	tempFile, err := os.CreateTemp(modelDir, ".fileforge-rembg-model-*")
	if err != nil {
		return fmt.Errorf("create a temporary background removal model file: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := s.downloader.DownloadArtifact(ctx, RembgModelArtifact(), tempPath, s.progressRange(
		PhaseRembg, "Downloading the background removal model…",
		rembgModelStartPercent, rembgModelEndPercent,
	)); err != nil {
		return fmt.Errorf("download the background removal model: %w", err)
	}

	if err := verifyRembgModel(tempPath, spec); err != nil {
		return fmt.Errorf("verify the background removal model: %w", err)
	}
	if err := fsx.ReplaceFile(tempPath, target); err != nil {
		return fmt.Errorf("install the background removal model: %w", err)
	}

	s.progress(PhaseRembg, rembgModelEndPercent, "The background removal model is installed.", false)

	return nil
}

// verifyRembgModel checks the exact size and SHA-256 of a downloaded model file.
// The shared downloader verifies pinned artifacts too; this local check keeps
// the model contract explicit because the rembg spec is mutable in tests and
// status reports readiness from the same pinned size.
func verifyRembgModel(path string, spec RembgModelSpec) error {
	artifact := Artifact{
		URL:       spec.URL,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  spec.FileName,
	}

	return artifact.Verify(path)
}

// installUpscaleModel downloads the pinned photo model archive with size and
// checksum verification, extracts the graph and its external weights, verifies
// each file and publishes the model folder atomically. A failed download or a
// bad checksum leaves the previous model untouched.
func (s *Service) installUpscaleModel(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	spec := upscaleModelSpec
	if !force && upscaleModelFilesMatch(s.root) {
		s.progress(PhaseUpscale, upscaleModelEndPercent, "The photo upscale model is already installed.", false)

		return nil
	}

	s.progress(PhaseUpscale, upscaleModelStartPercent, "Downloading the photo upscale model…", false)

	modelsParent := filepath.Dir(upscaleModelDir(s.root))
	if err := os.MkdirAll(modelsParent, 0o755); err != nil {
		return fmt.Errorf("create the upscale model folder: %w", err)
	}

	tempDir, err := os.MkdirTemp(modelsParent, ".fileforge-upscale-model-*")
	if err != nil {
		return fmt.Errorf("create a temporary upscale model folder: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	archivePath := filepath.Join(tempDir, spec.ArchiveName)
	if err := s.downloader.DownloadArtifact(ctx, UpscaleModelArtifact(), archivePath, s.progressRange(
		PhaseUpscale, "Downloading the photo upscale model…",
		upscaleModelStartPercent, upscaleModelEndPercent,
	)); err != nil {
		return fmt.Errorf("download the upscale model: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	extractDir := filepath.Join(tempDir, "extract")
	if err := ExtractZip(archivePath, extractDir); err != nil {
		return fmt.Errorf("extract the upscale model: %w", err)
	}

	sourceDir := filepath.Join(extractDir, filepath.FromSlash(spec.ArchiveDir))
	stagedDir := filepath.Join(tempDir, "model")
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		return fmt.Errorf("stage the upscale model: %w", err)
	}
	for _, file := range spec.Files {
		source := filepath.Join(sourceDir, file.Name)
		if err := (Artifact{SHA256: file.SHA256, SizeBytes: file.SizeBytes}).Verify(source); err != nil {
			return fmt.Errorf("verify the upscale model file %s: %w", file.Name, err)
		}
		if err := fsx.ReplaceFile(source, filepath.Join(stagedDir, file.Name)); err != nil {
			return fmt.Errorf("stage the upscale model file %s: %w", file.Name, err)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	targetDir := upscaleModelDir(s.root)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("replace the upscale model: %w", err)
	}
	if err := os.Rename(stagedDir, targetDir); err != nil {
		return fmt.Errorf("install the upscale model: %w", err)
	}

	s.progress(PhaseUpscale, upscaleModelEndPercent, "The photo upscale model is installed.", false)

	return nil
}

// installUpscaleAnimeModel downloads the pinned illustration model file with
// size and checksum verification. It stages the download next to its
// destination and publishes it atomically, so a failed download or a bad
// checksum leaves the previous model untouched.
func (s *Service) installUpscaleAnimeModel(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	spec := upscaleAnimeModelSpec
	target := upscaleAnimeModelPath(s.root)
	if !force && upscaleAnimeModelFilesMatch(s.root) {
		s.progress(PhaseUpscale, upscaleAnimeModelEndPercent, "The illustration upscale model is already installed.", false)

		return nil
	}

	s.progress(PhaseUpscale, upscaleAnimeModelStartPercent, "Downloading the illustration upscale model…", false)

	modelDir := upscaleAnimeModelDir(s.root)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return fmt.Errorf("create the illustration upscale model folder: %w", err)
	}

	tempFile, err := os.CreateTemp(modelDir, ".fileforge-upscale-anime-model-*")
	if err != nil {
		return fmt.Errorf("create a temporary illustration upscale model file: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := s.downloader.DownloadArtifact(ctx, UpscaleAnimeModelArtifact(), tempPath, s.progressRange(
		PhaseUpscale, "Downloading the illustration upscale model…",
		upscaleAnimeModelStartPercent, upscaleAnimeModelEndPercent,
	)); err != nil {
		return fmt.Errorf("download the illustration upscale model: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	artifact := Artifact{FileName: spec.FileName, SHA256: spec.SHA256, SizeBytes: spec.SizeBytes}
	if err := artifact.Verify(tempPath); err != nil {
		return fmt.Errorf("verify the illustration upscale model: %w", err)
	}
	if err := fsx.ReplaceFile(tempPath, target); err != nil {
		return fmt.Errorf("install the illustration upscale model: %w", err)
	}

	s.progress(PhaseUpscale, upscaleAnimeModelEndPercent, "The illustration upscale model is installed.", false)

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

	s.emitEvent(models.ToolchainInstallEvent{
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

	s.emitEvent(models.ToolchainInstallEvent{
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

	s.emitEvent(models.ToolchainInstallEvent{
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

	s.emitEvent(models.ToolchainInstallEvent{
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
		if percent <= lastPercent {
			// A retried download restarts its byte count; keep the UI
			// monotonic instead of rewinding the progress bar.
			return
		}

		lastPercent = percent
		s.progress(phase, percent, message, false)
	}
}

func (s *Service) failureStatus(message string) models.ToolchainStatus {
	status := s.statusSnapshot(false)
	status.Success = false
	status.Message = message
	status.Error = message

	return status
}

func (s *Service) emitEvent(event models.ToolchainInstallEvent) {
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
	{id: "yt-dlp", label: "yt-dlp", names: []string{"yt-dlp"}, required: false, detail: "Media downloader for URL sources"},
	{id: "deno", label: "Deno", names: []string{"deno"}, required: false, detail: "JavaScript runtime for YouTube challenge solving"},
}

var versionArgsByComponent = map[string][]string{
	"micromamba":  {"--version"},
	"ffmpeg":      {"-version"},
	"tesseract":   {"--version"},
	"ghostscript": {"--version"},
	"yt-dlp":      {"--version"},
	"deno":        {"--version"},
}

func (s *Service) buildComponents(model string, languages []string, probeVersions bool) []models.ToolchainComponent {
	components := make([]models.ToolchainComponent, 0, len(managedBinarySpecs)+8)

	micromamba := micromambaPath(s.root, s.platform)
	micromambaComponent := models.ToolchainComponent{
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
		component := models.ToolchainComponent{
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
	cliReady, cliDetail := whisperRuntimeState(s.root, s.platform)
	cliComponent := models.ToolchainComponent{
		ID:       "whisper-cli",
		Label:    "Whisper runtime",
		Ready:    cliReady,
		Required: true,
		Detail:   cliDetail,
	}
	if cliComponent.Ready {
		cliComponent.Path = cliPath
	}
	components = append(components, cliComponent)

	modelPath := modelFilePath(s.root, model)
	modelComponent := models.ToolchainComponent{
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
		components = append(components, models.ToolchainComponent{
			ID:       "tesseract-" + language,
			Label:    "Tesseract " + language,
			Ready:    fileWithContent(path),
			Required: containsLanguage(languages, language),
			Path:     path,
			Detail:   fmt.Sprintf("Tesseract %s language data", language),
		})
	}

	pdfAssetsReady, pdfAssetsDetail := tessdataPDFAssetsState(tessdataDir(s.root))
	pdfAssetsComponent := models.ToolchainComponent{
		ID:       "tesseract-pdf",
		Label:    "Tesseract PDF output",
		Ready:    pdfAssetsReady,
		Required: len(languages) > 0,
		Detail:   pdfAssetsDetail,
	}
	if pdfAssetsReady {
		pdfAssetsComponent.Path = filepath.Join(tessdataDir(s.root), "pdf.ttf")
	}
	components = append(components, pdfAssetsComponent)

	// The PDF to DOCX engine is optional: a missing virtual environment must not
	// block the rest of the application.
	venvPython := pdf2docxVenvPythonPath(s.root, s.platform.OS)
	venvBinary := pdf2docxVenvBinaryPath(s.root, s.platform.OS)
	pdf2docxReady := fileWithContent(venvPython) && fileWithContent(venvBinary)
	pdf2docxDetail := "PDF to DOCX engine is not installed. Run Install / repair."
	if pdf2docxReady {
		pdf2docxDetail = "Local PDF to DOCX conversion engine"
	}
	pdf2docxComponent := models.ToolchainComponent{
		ID:       "pdf2docx",
		Label:    "PDF to DOCX engine",
		Ready:    pdf2docxReady,
		Required: false,
		Detail:   pdf2docxDetail,
	}
	if pdf2docxReady {
		pdf2docxComponent.Path = venvPython
	}
	components = append(components, pdf2docxComponent)

	// The background removal engine and the upscale engine share one ONNX
	// virtual environment. Each engine keeps its own component so the UI can
	// report it separately, and neither gates the rest of the application. A
	// failure recorded during an install is attached to the component.
	rembgReady, rembgDetail := rembgRuntimeState(s.root, s.platform.OS)
	rembgComponent := models.ToolchainComponent{
		ID:       "rembg",
		Label:    "Background removal engine",
		Ready:    rembgReady,
		Required: false,
		Detail:   rembgDetail,
		Error:    s.componentFailure("rembg"),
	}
	if rembgReady {
		rembgComponent.Path = onnxVenvPythonPath(s.root, s.platform.OS)
	}
	components = append(components, rembgComponent)

	rembgModelReady, rembgModelDetail := rembgModelState(s.root)
	rembgModelComponent := models.ToolchainComponent{
		ID:       "rembg-model",
		Label:    "Background removal model",
		Ready:    rembgModelReady,
		Required: false,
		Detail:   rembgModelDetail,
		Error:    s.componentFailure("rembg-model"),
	}
	if rembgModelReady {
		rembgModelComponent.Path = rembgModelPath(s.root, rembgModelSpec.Model)
	}
	components = append(components, rembgModelComponent)

	// The upscale engine and both pinned models are part of the standard
	// install set. They are not Required: a failed download degrades these
	// tools instead of blocking startup.
	upscaleReady, upscaleDetail := upscaleRuntimeState(s.root, s.platform.OS)
	upscaleComponent := models.ToolchainComponent{
		ID:       "upscale",
		Label:    "Upscale engine",
		Ready:    upscaleReady,
		Required: false,
		Standard: true,
		Detail:   upscaleDetail,
		Error:    s.componentFailure("upscale"),
	}
	if upscaleReady {
		upscaleComponent.Path = onnxVenvPythonPath(s.root, s.platform.OS)
	}
	components = append(components, upscaleComponent)

	upscaleModelReady, upscaleModelDetail := upscaleModelState(s.root)
	upscaleModelComponent := models.ToolchainComponent{
		ID:       "upscale-model",
		Label:    "Photo upscale model",
		Ready:    upscaleModelReady,
		Required: false,
		Standard: true,
		Detail:   upscaleModelDetail,
		Error:    s.componentFailure("upscale-model"),
	}
	if upscaleModelReady {
		upscaleModelComponent.Path = upscaleModelONNXPath(s.root)
	}
	components = append(components, upscaleModelComponent)

	upscaleAnimeModelReady, upscaleAnimeModelDetail := upscaleAnimeModelState(s.root)
	upscaleAnimeModelComponent := models.ToolchainComponent{
		ID:       "upscale-anime-model",
		Label:    "Illustration upscale model",
		Ready:    upscaleAnimeModelReady,
		Required: false,
		Standard: true,
		Detail:   upscaleAnimeModelDetail,
		Error:    s.componentFailure("upscale-anime-model"),
	}
	if upscaleAnimeModelReady {
		upscaleAnimeModelComponent.Path = upscaleAnimeModelPath(s.root)
	}
	components = append(components, upscaleAnimeModelComponent)

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

func missingComponentSummary(components []models.ToolchainComponent) string {
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

// manifest is the informational install record. Status still verifies the
// files directly, so a stale or missing manifest never marks components ready.
type manifest struct {
	Version      int                 `json:"version"`
	GeneratedAt  string              `json:"generatedAt"`
	Model        string              `json:"model"`
	OCRLanguages []string            `json:"ocrLanguages"`
	Components   []manifestComponent `json:"components"`
}

type manifestComponent struct {
	ID      string `json:"id"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
}

func writeManifest(root, model string, languages []string, components []models.ToolchainComponent) error {
	record := manifest{
		Version:      1,
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Model:        model,
		OCRLanguages: append([]string{}, languages...),
		Components:   make([]manifestComponent, 0, len(components)),
	}
	for _, component := range components {
		if !component.Ready {
			continue
		}

		record.Components = append(record.Components, manifestComponent{
			ID:      component.ID,
			Path:    component.Path,
			Version: component.Version,
		})
	}

	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	return fsx.WriteAtomic(manifestFilePath(root), payload, 0o644)
}

func readManifest(path string) (manifest, error) {
	var record manifest

	payload, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(payload, &record); err != nil {
		return manifest{}, err
	}

	return record, nil
}
