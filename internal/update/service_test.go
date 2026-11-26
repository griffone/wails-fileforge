package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeProvider struct {
	release        *Release
	err            error
	calls          int
	currentVersion string
}

func (f *fakeProvider) Check(_ context.Context, currentVersion string) (*Release, error) {
	f.calls++
	f.currentVersion = currentVersion
	return f.release, f.err
}

type fakeDownloader struct {
	path    string
	content []byte
	err     error
	calls   int
}

func newFakeDownloader(t *testing.T, content []byte) *fakeDownloader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "downloaded-artifact")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fake download: %v", err)
	}
	return &fakeDownloader{path: path, content: content}
}

func (f *fakeDownloader) Download(_ context.Context, _ *Release, _ string, onProgress ProgressFunc) (*Downloaded, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if onProgress != nil {
		onProgress(int64(len(f.content))/2, int64(len(f.content)))
		onProgress(int64(len(f.content)), int64(len(f.content)))
	}
	return &Downloaded{Path: f.path, Size: int64(len(f.content))}, nil
}

func serviceEnvironment(t *testing.T) Environment {
	t.Helper()
	path := filepath.Join(t.TempDir(), "FileForge.AppImage")
	if err := os.WriteFile(path, []byte("old appimage"), 0o755); err != nil {
		t.Fatalf("write fake AppImage: %v", err)
	}
	return Environment{
		GOOS:          "linux",
		GOARCH:        "amd64",
		Version:       "1.2.0",
		Release:       true,
		KeyConfigured: true,
		AppImage:      path,
	}
}

func serviceRelease(kind string) *Release {
	verification := Verification{Digest: make([]byte, 32), Signature: make([]byte, 64)}
	name := "fileforge-desktop-x86_64.AppImage"
	if kind == AssetKindInstaller {
		name = "fileforge-desktop-amd64-installer.exe"
	}
	return &Release{
		Version:      "1.3.0",
		Tag:          "v1.3.0",
		Name:         "FileForge 1.3.0",
		Notes:        "notes",
		Asset:        Asset{Name: name, Kind: kind},
		Verification: verification,
	}
}

func TestServiceCheckCachesTheRelease(t *testing.T) {
	environment := serviceEnvironment(t)
	provider := &fakeProvider{release: serviceRelease(AssetKindAppImage)}
	downloader := newFakeDownloader(t, []byte("new binary"))
	service := NewService(ServiceConfig{ReleaseProvider: provider, Downloader: downloader, Environment: &environment})

	status := service.Check(context.Background())
	if !status.Success {
		t.Fatalf("Check() failed: %s", status.Message)
	}
	if status.State != StateAvailable || !status.UpdateAvailable {
		t.Fatalf("status = %+v, want an available update", status)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if provider.currentVersion != "1.2.0" {
		t.Fatalf("provider current version = %q", provider.currentVersion)
	}
	if downloader.calls != 0 {
		t.Fatalf("Check() downloaded %d artifacts, want 0", downloader.calls)
	}

	cached := service.Status()
	if cached.Release == nil || cached.Release.Version != "1.3.0" {
		t.Fatalf("cached release = %+v", cached.Release)
	}
	if provider.calls != 1 {
		t.Fatalf("Status() hit the provider: calls = %d", provider.calls)
	}
}

func TestServiceReportsUpToDate(t *testing.T) {
	environment := serviceEnvironment(t)
	release := serviceRelease(AssetKindAppImage)
	release.Version = "1.2.0"
	release.Tag = "v1.2.0"
	provider := &fakeProvider{release: release}
	service := NewService(ServiceConfig{ReleaseProvider: provider, Downloader: newFakeDownloader(t, nil), Environment: &environment})

	status := service.Check(context.Background())
	if !status.Success {
		t.Fatalf("Check() failed: %s", status.Message)
	}
	if status.State != StateUpToDate || status.UpdateAvailable {
		t.Fatalf("status = %+v, want up to date", status)
	}
	if status.Release == nil {
		t.Fatal("the last known release was not cached")
	}
}

func TestServiceDownloadRefusesWhenUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Environment)
	}{
		{name: "development build", mutate: func(env *Environment) { env.Release = false; env.Version = "dev" }},
		{name: "no pinned key", mutate: func(env *Environment) { env.KeyConfigured = false }},
		{name: "linux without APPIMAGE", mutate: func(env *Environment) { env.AppImage = "" }},
		{name: "unsupported platform", mutate: func(env *Environment) { env.GOOS = "darwin" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := serviceEnvironment(t)
			test.mutate(&environment)
			provider := &fakeProvider{release: serviceRelease(AssetKindAppImage)}
			downloader := newFakeDownloader(t, []byte("binary"))
			service := NewService(ServiceConfig{ReleaseProvider: provider, Downloader: downloader, Environment: &environment})

			service.Check(context.Background())
			status := service.Download(context.Background())
			if status.Success {
				t.Fatalf("Download() succeeded on an unavailable build: %+v", status)
			}
			if downloader.calls != 0 {
				t.Fatalf("downloader calls = %d, want 0", downloader.calls)
			}
			if status.Reason == "" {
				t.Fatalf("status has no gating reason: %+v", status)
			}
			if !status.Success && status.State != StateUnavailable {
				t.Fatalf("state = %q, want %q", status.State, StateUnavailable)
			}
		})
	}
}

func TestServiceDownloadRequiresACheckFirst(t *testing.T) {
	environment := serviceEnvironment(t)
	downloader := newFakeDownloader(t, []byte("binary"))
	service := NewService(ServiceConfig{ReleaseProvider: &fakeProvider{}, Downloader: downloader, Environment: &environment})

	status := service.Download(context.Background())
	if status.Success || !strings.Contains(status.Error, "no release") {
		t.Fatalf("Download() = %+v, want a no-release refusal", status)
	}
	if downloader.calls != 0 {
		t.Fatalf("downloader calls = %d, want 0", downloader.calls)
	}
}

func TestServiceInstallRequiresConsentAndDownload(t *testing.T) {
	environment := serviceEnvironment(t)
	provider := &fakeProvider{release: serviceRelease(AssetKindAppImage)}
	downloader := newFakeDownloader(t, []byte("replacement appimage"))
	var started []string
	var quits int
	service := NewService(ServiceConfig{
		ReleaseProvider: provider,
		Downloader:      downloader,
		Environment:     &environment,
		Start:           func(path string) error { started = append(started, path); return nil },
		Quit:            func() { quits++ },
	})

	service.Check(context.Background())

	// No download yet: install refuses, and consent cannot be granted.
	status := service.Install(context.Background())
	if status.Success || !strings.Contains(status.Error, "downloaded") {
		t.Fatalf("Install() before download = %+v", status)
	}
	if consent := service.GrantInstallConsent(); consent.Success {
		t.Fatalf("GrantInstallConsent() succeeded without a download: %+v", consent)
	}

	// Downloaded but not consented: install refuses and nothing starts.
	if status := service.Download(context.Background()); !status.Success {
		t.Fatalf("Download() failed: %s", status.Message)
	}
	status = service.Install(context.Background())
	if status.Success || !strings.Contains(status.Error, "consent") {
		t.Fatalf("Install() without consent = %+v", status)
	}
	if len(started) != 0 || quits != 0 {
		t.Fatalf("install started %v and quit %d times without consent", started, quits)
	}

	// Consent is explicit, scoped to this download.
	if consent := service.GrantInstallConsent(); !consent.Success || !consent.ConsentGranted {
		t.Fatalf("GrantInstallConsent() = %+v", consent)
	}
	status = service.Install(context.Background())
	if !status.Success || status.State != StateInstalled {
		t.Fatalf("Install() = %+v", status)
	}
	if len(started) != 1 || started[0] != environment.AppImage {
		t.Fatalf("started = %v, want %s", started, environment.AppImage)
	}
	if quits != 1 {
		t.Fatalf("quits = %d, want 1", quits)
	}

	installed, err := os.ReadFile(environment.AppImage)
	if err != nil {
		t.Fatalf("read replaced AppImage: %v", err)
	}
	if string(installed) != "replacement appimage" {
		t.Fatalf("AppImage content = %q", installed)
	}
	info, err := os.Stat(environment.AppImage)
	if err != nil {
		t.Fatalf("stat AppImage: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("AppImage mode = %o, want 755", info.Mode().Perm())
	}
	if service.Status().ConsentGranted {
		t.Fatal("consent survived the install")
	}
}

func TestServiceInstallWindowsStartsTheInstaller(t *testing.T) {
	environment := serviceEnvironment(t)
	environment.GOOS = "windows"
	environment.AppImage = ""
	provider := &fakeProvider{release: serviceRelease(AssetKindInstaller)}
	downloader := newFakeDownloader(t, []byte("installer"))
	var started []string
	var quits int
	service := NewService(ServiceConfig{
		ReleaseProvider: provider,
		Downloader:      downloader,
		Environment:     &environment,
		Start:           func(path string) error { started = append(started, path); return nil },
		Quit:            func() { quits++ },
	})

	service.Check(context.Background())
	service.Download(context.Background())
	service.GrantInstallConsent()

	status := service.Install(context.Background())
	if !status.Success || status.State != StateInstalled {
		t.Fatalf("Install() = %+v", status)
	}
	if len(started) != 1 || started[0] != downloader.path {
		t.Fatalf("started = %v, want %s", started, downloader.path)
	}
	if quits != 1 {
		t.Fatalf("quits = %d, want 1", quits)
	}
}

func TestServiceWindowsRefusesThePortableZip(t *testing.T) {
	environment := serviceEnvironment(t)
	environment.GOOS = "windows"
	environment.AppImage = ""
	provider := &fakeProvider{release: serviceRelease(AssetKindZip)}
	downloader := newFakeDownloader(t, []byte("zip"))
	started := 0
	service := NewService(ServiceConfig{
		ReleaseProvider: provider,
		Downloader:      downloader,
		Environment:     &environment,
		Start:           func(string) error { started++; return nil },
	})

	service.Check(context.Background())
	service.Download(context.Background())
	service.GrantInstallConsent()

	status := service.Install(context.Background())
	if status.Success || !strings.Contains(status.Error, "installer") {
		t.Fatalf("Install() for a zip = %+v", status)
	}
	if started != 0 {
		t.Fatalf("started %d processes, want 0", started)
	}
}

func TestServiceEmitsStateAndProgressInOrder(t *testing.T) {
	environment := serviceEnvironment(t)
	provider := &fakeProvider{release: serviceRelease(AssetKindAppImage)}
	downloader := newFakeDownloader(t, []byte("replacement"))
	sink := &eventSink{}
	service := NewService(ServiceConfig{
		ReleaseProvider: provider,
		Downloader:      downloader,
		Environment:     &environment,
		Bus:             sink,
		Start:           func(string) error { return nil },
	})

	service.Check(context.Background())
	service.Download(context.Background())

	var states []string
	var progress []ProgressEvent
	for _, event := range sink.snapshot() {
		switch event.name {
		case StateEventName:
			status, ok := event.payload.(Status)
			if !ok {
				t.Fatalf("state payload type = %T", event.payload)
			}
			states = append(states, status.State)
		case ProgressEventName:
			progressEvent, ok := event.payload.(ProgressEvent)
			if !ok {
				t.Fatalf("progress payload type = %T", event.payload)
			}
			progress = append(progress, progressEvent)
		case ErrorEventName:
			t.Fatalf("unexpected error event: %+v", event.payload)
		}
	}

	want := []string{StateChecking, StateAvailable, StateDownloading, StateReady}
	if strings.Join(states, ",") != strings.Join(want, ",") {
		t.Fatalf("state order = %v, want %v", states, want)
	}
	if len(progress) == 0 {
		t.Fatal("no progress events were emitted")
	}
	if last := progress[len(progress)-1]; last.Percent != 100 || last.Written != int64(len(downloader.content)) {
		t.Fatalf("last progress = %+v", last)
	}
}

func TestServiceCheckFailureEmitsErrorAndState(t *testing.T) {
	environment := serviceEnvironment(t)
	provider := &fakeProvider{err: ErrNetwork}
	sink := &eventSink{}
	service := NewService(ServiceConfig{
		ReleaseProvider: provider,
		Downloader:      newFakeDownloader(t, nil),
		Environment:     &environment,
		Bus:             sink,
	})

	status := service.Check(context.Background())
	if status.Success || status.State != StateError || !strings.Contains(status.Error, "network") {
		t.Fatalf("Check() = %+v", status)
	}

	var sawError, sawState bool
	for _, event := range sink.snapshot() {
		switch event.name {
		case ErrorEventName:
			payload, ok := event.payload.(ErrorEvent)
			if !ok {
				t.Fatalf("error payload type = %T", event.payload)
			}
			sawError = payload.Stage == StageCheck && strings.Contains(payload.Message, "network")
		case StateEventName:
			payload, ok := event.payload.(Status)
			if !ok {
				t.Fatalf("state payload type = %T", event.payload)
			}
			if payload.State == StateError && !payload.Success {
				sawState = true
			}
		}
	}
	if !sawError {
		t.Fatal("no update/error event for the check failure")
	}
	if !sawState {
		t.Fatal("no terminal update/state event for the check failure")
	}
}

func TestServiceDownloadVerificationFailureUsesTheVerifyStage(t *testing.T) {
	environment := serviceEnvironment(t)
	provider := &fakeProvider{release: serviceRelease(AssetKindAppImage)}
	downloader := newFakeDownloader(t, []byte("bytes"))
	downloader.err = ErrDigestMismatch
	sink := &eventSink{}
	service := NewService(ServiceConfig{
		ReleaseProvider: provider,
		Downloader:      downloader,
		Environment:     &environment,
		Bus:             sink,
	})

	service.Check(context.Background())
	status := service.Download(context.Background())
	if status.Success {
		t.Fatal("Download() succeeded with a tampered artifact")
	}

	found := false
	for _, event := range sink.snapshot() {
		if event.name != ErrorEventName {
			continue
		}
		payload, ok := event.payload.(ErrorEvent)
		if !ok {
			t.Fatalf("error payload type = %T", event.payload)
		}
		found = payload.Stage == StageVerify
	}
	if !found {
		t.Fatal("no verify-stage error event")
	}
}
