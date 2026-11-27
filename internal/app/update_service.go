package app

import (
	"log"
	"os"
	"runtime"

	"fileforge-desktop/internal/update"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// UpdateService exposes the signed auto-update flow to the UI. Every method
// returns the full status snapshot, and every transition is also emitted as an
// update/* event, matching the existing job and toolchain services.
//
// The service never installs without consent: the UI must call
// GrantInstallConsent after the user accepts the version, notes and size, then
// InstallUpdate. A new check or download clears the consent.
type UpdateService struct {
	runtime *Runtime
	service *update.Service
}

// newUpdateService wires the GitHub release provider and the Wails bus.
func newUpdateService(lifetime *Runtime) *UpdateService {
	providerConfig := update.ProviderConfig{
		Repository: update.DefaultRepository,
		Token:      os.Getenv(update.DefaultTokenEnv),
		Platform:   runtime.GOOS,
		Arch:       runtime.GOARCH,
	}
	if update.Configured() {
		key, err := update.Key()
		if err != nil {
			log.Printf("app.update.key.invalid err=%v", err)
		} else {
			providerConfig.PublicKey = key
		}
	}

	var releaseProvider update.ReleaseProvider
	var downloader update.Downloader
	provider, err := update.NewProvider(providerConfig)
	if err != nil {
		// A misconfigured repository leaves updates unavailable, never
		// silently trusted.
		log.Printf("app.update.provider.failed err=%v", err)
	} else {
		releaseProvider = provider
		downloader = provider
	}

	service := update.NewService(update.ServiceConfig{
		ReleaseProvider: releaseProvider,
		Downloader:      downloader,
		Bus:             WailsBus{},
		Quit:            quitForUpdate,
	})

	return &UpdateService{runtime: lifetime, service: service}
}

// GetUpdateStatus returns the build identity, the gating reason and the last
// known release. It never touches the network.
func (s *UpdateService) GetUpdateStatus() update.Status {
	return s.service.Status()
}

// CheckForUpdates refreshes the cached release. It downloads nothing.
func (s *UpdateService) CheckForUpdates() update.Status {
	return s.service.Check(s.runtime.ContextOrBackground())
}

// DownloadUpdate downloads the cached release and verifies its digest and
// signature. It refuses when the gate is closed.
func (s *UpdateService) DownloadUpdate() update.Status {
	return s.service.Download(s.runtime.ContextOrBackground())
}

// GrantInstallConsent records the user's explicit decision to install the
// verified download. Call it only after showing version, notes and size.
func (s *UpdateService) GrantInstallConsent() update.Status {
	return s.service.GrantInstallConsent()
}

// InstallUpdate replaces the AppImage and relaunches, or starts the Windows
// installer. The app quits once the new process has started.
func (s *UpdateService) InstallUpdate() update.Status {
	return s.service.Install(s.runtime.ContextOrBackground())
}

func quitForUpdate() {
	app := application.Get()
	if app != nil {
		app.Quit()
	}
}
