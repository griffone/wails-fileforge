package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"fileforge-desktop/internal/platform/eventbus"
)

// Event names. They follow the existing jobs/*, recipes/* and toolchain/*
// convention: a progress event while bytes move, a full status snapshot on
// every transition, and an error event with the failing stage.
const (
	ProgressEventName = "update/progress"
	StateEventName    = "update/state"
	ErrorEventName    = "update/error"
)

// Lifecycle states reported by Status and carried by StateEventName.
const (
	StateIdle        = "idle"
	StateUnavailable = "unavailable"
	StateChecking    = "checking"
	StateUpToDate    = "up-to-date"
	StateAvailable   = "available"
	StateDownloading = "downloading"
	StateReady       = "ready"
	StateInstalling  = "installing"
	StateInstalled   = "installed"
	StateError       = "error"
)

// Stage names which part of the flow failed. It is also the stage of a
// progress event, so one string tells the UI what is happening.
type Stage string

const (
	StageCheck    Stage = "check"
	StageDownload Stage = "download"
	StageVerify   Stage = "verify"
	StageInstall  Stage = "install"
)

// ProgressEvent is emitted while an artifact downloads.
type ProgressEvent struct {
	Stage   Stage `json:"stage"`
	Written int64 `json:"written"`
	Total   int64 `json:"total"`
	Percent int   `json:"percent"`
}

// ErrorEvent is emitted once per failed operation.
type ErrorEvent struct {
	Stage   Stage  `json:"stage"`
	Message string `json:"message"`
}

// ReleaseProvider checks the release feed. The GitHub provider implements it; tests
// substitute a fake.
type ReleaseProvider interface {
	Check(ctx context.Context, currentVersion string) (*Release, error)
}

// Downloader streams and verifies one release artifact into dir.
type Downloader interface {
	Download(ctx context.Context, release *Release, dir string, onProgress ProgressFunc) (*Downloaded, error)
}

// Status is the snapshot returned by every service method and emitted on
// StateEventName. It always carries the gating reason, so the UI cannot show
// an update on a build that must not install one.
type Status struct {
	Success         bool        `json:"success"`
	Message         string      `json:"message"`
	State           string      `json:"state"`
	CurrentVersion  string      `json:"currentVersion"`
	UpdatesEnabled  bool        `json:"updatesEnabled"`
	UpdateAvailable bool        `json:"updateAvailable"`
	Reason          string      `json:"reason,omitempty"`
	Method          string      `json:"method,omitempty"`
	Release         *Release    `json:"release,omitempty"`
	Download        *Downloaded `json:"download,omitempty"`
	ConsentGranted  bool        `json:"consentGranted"`
	Error           string      `json:"error,omitempty"`
	Stage           Stage       `json:"stage,omitempty"`
}

// ServiceConfig wires the update service. The composition root supplies the
// real provider, the Wails bus and the quit action; tests substitute fakes.
type ServiceConfig struct {
	ReleaseProvider ReleaseProvider
	Downloader      Downloader
	Bus             eventbus.Bus
	// Environment overrides the running build identity. Nil reads the real
	// build and $APPIMAGE.
	Environment *Environment
	// DownloadRoot is the parent of the temporary download directory. Empty
	// uses the OS temporary directory.
	DownloadRoot string
	// Start launches a file and returns once it has started. Nil uses
	// StartProcess.
	Start func(path string) error
	// Quit ends the app after an install. Nil is a no-op, which keeps headless
	// runs and tests safe.
	Quit func()
}

// Service owns update state and the check/download/install flow. It is the
// headless core; the Wails binding in internal/app only forwards RPCs and the
// application lifetime context.
type Service struct {
	provider     ReleaseProvider
	downloader   Downloader
	bus          eventbus.Bus
	environment  *Environment
	downloadRoot string
	start        func(path string) error
	quit         func()

	mu         sync.Mutex
	busy       bool
	state      string
	release    *Release
	downloaded *Downloaded
	consent    bool
	lastError  string
	lastStage  Stage
}

// NewService builds an update service.
func NewService(cfg ServiceConfig) *Service {
	downloader := cfg.Downloader
	if downloader == nil {
		if candidate, ok := cfg.ReleaseProvider.(Downloader); ok {
			downloader = candidate
		}
	}
	start := cfg.Start
	if start == nil {
		start = StartProcess
	}

	return &Service{
		provider:     cfg.ReleaseProvider,
		downloader:   downloader,
		bus:          cfg.Bus,
		environment:  cfg.Environment,
		downloadRoot: cfg.DownloadRoot,
		start:        start,
		quit:         cfg.Quit,
		state:        StateIdle,
	}
}

// Status returns the cached snapshot. It never touches the network: a manual
// check is the only way to refresh the release.
func (s *Service) Status() Status {
	environment := s.environmentValue()
	gate := EvaluateGate(environment)

	s.mu.Lock()
	status := Status{
		Success:         true,
		State:           s.state,
		CurrentVersion:  environment.Version,
		UpdatesEnabled:  gate.Available,
		Reason:          gate.Reason,
		Method:          gate.Method,
		Release:         s.release,
		Download:        s.downloaded,
		ConsentGranted:  s.consent,
		Error:           s.lastError,
		Stage:           s.lastStage,
		UpdateAvailable: gate.Available && s.release != nil && IsNewer(s.release.Version, environment.Version),
	}
	s.mu.Unlock()

	if !gate.Available {
		status.State = StateUnavailable
		status.Message = gate.Reason
		return status
	}
	if status.State == "" {
		status.State = StateIdle
	}

	switch status.State {
	case StateAvailable:
		status.Message = "An update is available."
	case StateUpToDate:
		status.Message = "FileForge is up to date."
	case StateReady:
		status.Message = "The update was downloaded and verified."
	case StateInstalling:
		status.Message = "Installing the update."
	case StateInstalled:
		status.Message = "The update was installed."
	case StateError:
		status.Message = status.Error
	case StateChecking:
		status.Message = "Checking for updates."
	case StateDownloading:
		status.Message = "Downloading the update."
	default:
		status.Message = "No update check has run yet."
	}
	return status
}

// Check fetches the latest stable release and caches it. It never downloads.
// The release is cached even when it is not newer, so Status keeps showing the
// last known release without going back to the network.
func (s *Service) Check(ctx context.Context) Status {
	if !s.begin() {
		return s.fail(StageCheck, ErrBusy)
	}
	defer s.end()

	environment := s.environmentValue()
	gate := EvaluateGate(environment)
	if !gate.Available {
		s.mu.Lock()
		s.state = StateUnavailable
		s.lastError = ""
		s.lastStage = ""
		s.mu.Unlock()
		s.emitState()
		status := s.Status()
		status.Success = false
		status.Error = gate.Reason
		status.Stage = StageCheck
		return status
	}
	if s.provider == nil {
		return s.fail(StageCheck, fmt.Errorf("%w: no release provider configured", ErrUnavailable))
	}

	s.setState(StateChecking)
	s.emitState()

	release, err := s.provider.Check(ctx, environment.Version)
	if err != nil {
		return s.fail(StageCheck, err)
	}
	if release == nil {
		return s.fail(StageCheck, fmt.Errorf("%w: provider returned no release", ErrMalformedRelease))
	}

	s.mu.Lock()
	s.release = release
	s.downloaded = nil
	s.consent = false
	s.lastError = ""
	s.lastStage = ""
	if IsNewer(release.Version, environment.Version) {
		s.state = StateAvailable
	} else {
		s.state = StateUpToDate
	}
	s.mu.Unlock()

	s.emitState()
	return s.Status()
}

// Download fetches and verifies the cached release. It refuses when the gate
// is closed, when nothing was checked, or when the release is not an upgrade.
func (s *Service) Download(ctx context.Context) Status {
	if !s.begin() {
		return s.fail(StageDownload, ErrBusy)
	}
	defer s.end()

	environment := s.environmentValue()
	gate := EvaluateGate(environment)
	if !gate.Available {
		return s.fail(StageDownload, fmt.Errorf("%w: %s", ErrUnavailable, gate.Reason))
	}
	if s.downloader == nil {
		return s.fail(StageDownload, fmt.Errorf("%w: no artifact downloader configured", ErrUnavailable))
	}

	s.mu.Lock()
	release := s.release
	s.mu.Unlock()
	if release == nil {
		return s.fail(StageDownload, ErrNoRelease)
	}
	if !IsNewer(release.Version, environment.Version) {
		return s.fail(StageDownload, ErrNotNewer)
	}

	dir, err := os.MkdirTemp(s.downloadRoot, "fileforge-update-*")
	if err != nil {
		return s.fail(StageDownload, fmt.Errorf("%w: %v", ErrDownloadFailed, err))
	}

	s.mu.Lock()
	s.state = StateDownloading
	s.downloaded = nil
	s.consent = false
	s.lastError = ""
	s.lastStage = ""
	s.mu.Unlock()
	s.emitState()

	downloaded, err := s.downloader.Download(ctx, release, dir, func(written, total int64) {
		percent := 0
		if total > 0 {
			percent = int(written * 100 / total)
			if percent > 100 {
				percent = 100
			}
		}
		s.emit(ProgressEventName, ProgressEvent{
			Stage:   StageDownload,
			Written: written,
			Total:   total,
			Percent: percent,
		})
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return s.fail(stageForDownloadError(err), err)
	}

	s.mu.Lock()
	s.downloaded = downloaded
	s.state = StateReady
	s.mu.Unlock()
	s.emitState()
	return s.Status()
}

// GrantInstallConsent records the user's explicit decision to install the
// verified download. It is scoped to that download: a new check or download
// clears it, and Install consumes it.
func (s *Service) GrantInstallConsent() Status {
	s.mu.Lock()
	downloaded := s.downloaded
	if downloaded == nil {
		s.mu.Unlock()
		return s.refuse(StageInstall, ErrNotDownloaded)
	}
	s.consent = true
	s.mu.Unlock()
	return s.Status()
}

// Install performs the platform install. It requires a verified download and
// recorded consent; there is no argument that makes it install silently.
func (s *Service) Install(_ context.Context) Status {
	if !s.begin() {
		return s.fail(StageInstall, ErrBusy)
	}
	defer s.end()

	environment := s.environmentValue()
	gate := EvaluateGate(environment)
	if !gate.Available {
		return s.fail(StageInstall, fmt.Errorf("%w: %s", ErrUnavailable, gate.Reason))
	}

	s.mu.Lock()
	release := s.release
	download := s.downloaded
	consent := s.consent
	s.mu.Unlock()

	if download == nil {
		return s.fail(StageInstall, ErrNotDownloaded)
	}
	if !consent {
		return s.fail(StageInstall, ErrConsentRequired)
	}
	if release == nil {
		return s.fail(StageInstall, ErrNoRelease)
	}

	switch gate.Method {
	case MethodAppImage:
		if release.Asset.Kind != AssetKindAppImage {
			return s.fail(StageInstall, fmt.Errorf("%w: %s", ErrNoInstaller, release.Asset.Name))
		}
		if filepath.Clean(environment.AppImage) == "" || environment.AppImage == "." {
			return s.fail(StageInstall, ErrAppImageMissing)
		}

		s.setState(StateInstalling)
		s.emitState()

		if err := ReplaceAppImage(environment.AppImage, download.Path); err != nil {
			return s.fail(StageInstall, err)
		}
		if s.start != nil {
			if err := s.start(environment.AppImage); err != nil {
				return s.fail(StageInstall, err)
			}
		}
		return s.finishInstall()

	case MethodWindowsInstaller:
		if release.Asset.Kind != AssetKindInstaller {
			return s.fail(StageInstall, fmt.Errorf("%w: %s is not an installer", ErrNoInstaller, release.Asset.Name))
		}

		s.setState(StateInstalling)
		s.emitState()

		if s.start == nil {
			return s.fail(StageInstall, fmt.Errorf("%w: no process launcher configured", ErrNoInstaller))
		}
		if err := s.start(download.Path); err != nil {
			return s.fail(StageInstall, err)
		}
		return s.finishInstall()

	default:
		return s.fail(StageInstall, fmt.Errorf("%w: no install method for %s", ErrUnavailable, gate.Method))
	}
}

// finishInstall publishes the terminal state and asks the app to quit so the
// relaunched AppImage or the installer can take over.
func (s *Service) finishInstall() Status {
	s.mu.Lock()
	s.state = StateInstalled
	s.consent = false
	s.mu.Unlock()
	s.emitState()

	if s.quit != nil {
		s.quit()
	}
	return s.Status()
}

// begin marks the service busy. Only one network or install operation runs at
// a time, so a second click cannot start two downloads.
func (s *Service) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *Service) end() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *Service) setState(state string) {
	s.mu.Lock()
	s.state = state
	s.lastError = ""
	s.lastStage = ""
	s.mu.Unlock()
}

// fail records a failure, emits the error event, then emits the resulting
// status snapshot so a UI attached late still sees it.
func (s *Service) fail(stage Stage, err error) Status {
	s.mu.Lock()
	s.state = StateError
	s.lastError = err.Error()
	s.lastStage = stage
	s.mu.Unlock()

	s.emit(ErrorEventName, ErrorEvent{Stage: stage, Message: err.Error()})

	status := s.Status()
	status.Success = false
	status.Message = err.Error()
	s.emit(StateEventName, status)
	return status
}

// refuse reports a rejected action without moving the lifecycle to error, so a
// stray Install call does not hide a valid downloaded update.
func (s *Service) refuse(stage Stage, err error) Status {
	status := s.Status()
	status.Success = false
	status.Message = err.Error()
	status.Error = err.Error()
	status.Stage = stage
	return status
}

func (s *Service) emitState() {
	s.emit(StateEventName, s.Status())
}

func (s *Service) emit(name string, payload any) {
	if s.bus != nil {
		s.bus.Emit(name, payload)
	}
}

func (s *Service) environmentValue() Environment {
	if s.environment != nil {
		return *s.environment
	}
	return CurrentEnvironment()
}

// stageForDownloadError classifies a download failure as a verification
// failure when the bytes arrived but did not authenticate.
func stageForDownloadError(err error) Stage {
	switch {
	case errors.Is(err, ErrDigestMismatch),
		errors.Is(err, ErrSignatureInvalid),
		errors.Is(err, ErrSignatureMissing),
		errors.Is(err, ErrMalformedChecksums),
		errors.Is(err, ErrMalformedSignature),
		errors.Is(err, ErrNoPublicKey):
		return StageVerify
	default:
		return StageDownload
	}
}
