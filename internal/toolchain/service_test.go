package toolchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

// fakeDownloader maps URLs to payloads and records every requested URL. A
// blockURL simulates a stalled download until the context is cancelled; failURL
// simulates a network failure.
type fakeDownloader struct {
	mu       sync.Mutex
	payloads map[string][]byte
	calls    []string
	failURL  string
	blockURL string
	started  chan string
}

func (d *fakeDownloader) Download(ctx context.Context, url, destPath string, onProgress func(downloaded, total int64)) error {
	d.mu.Lock()
	d.calls = append(d.calls, url)
	payload, known := d.payloads[url]
	fail := d.failURL != "" && url == d.failURL
	block := d.blockURL != "" && url == d.blockURL
	started := d.started
	d.mu.Unlock()

	if block {
		if started != nil {
			select {
			case started <- url:
			default:
			}
		}
		<-ctx.Done()

		return ctx.Err()
	}
	if fail {
		return fmt.Errorf("fake download failure for %s", url)
	}
	if !known {
		return fmt.Errorf("unexpected download URL %s", url)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(destPath, payload, 0o644); err != nil {
		return err
	}
	if onProgress != nil {
		onProgress(int64(len(payload)), int64(len(payload)))
	}

	return nil
}

func (d *fakeDownloader) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return len(d.calls)
}

func (d *fakeDownloader) callsFor(url string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	count := 0
	for _, call := range d.calls {
		if call == url {
			count++
		}
	}

	return count
}

type fakeRunnerCall struct {
	name string
	args []string
	env  []string
}

// fakeRunner answers version probes and simulates micromamba creating or
// repairing the conda prefix by writing the expected binaries.
type fakeRunner struct {
	mu           sync.Mutex
	calls        []fakeRunnerCall
	failOn       string
	failVersions bool
}

func (r *fakeRunner) Run(ctx context.Context, name string, args []string, env []string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, fakeRunnerCall{name: name, args: append([]string{}, args...), env: append([]string{}, env...)})
	fail := r.failOn != "" && len(args) > 0 && args[0] == r.failOn
	failVersion := r.failVersions && len(args) > 0 && (args[0] == "--version" || args[0] == "-version")
	r.mu.Unlock()

	if failVersion {
		return nil, fmt.Errorf("version probe failed")
	}

	if len(args) > 0 && (args[0] == "create" || args[0] == "install") {
		if fail {
			return nil, fmt.Errorf("micromamba %s failed: exit status 1", args[0])
		}

		prefix := argumentAfter(args, "-p")
		if prefix == "" {
			return nil, fmt.Errorf("fake micromamba call without a prefix: %v", args)
		}
		if err := writeFakeCondaEnvironment(prefix); err != nil {
			return nil, err
		}

		return []byte("Transaction finished\n"), nil
	}

	// The PDF to DOCX engine lives in a dedicated virtual environment created
	// from the managed Python and filled by pip.
	if len(args) >= 3 && args[0] == "-m" && args[1] == "venv" {
		venvDir := args[2]
		for _, name := range []string{"python", "pdf2docx"} {
			path := filepath.Join(venvDir, "bin", name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
				return nil, err
			}
		}

		return []byte("venv created\n"), nil
	}
	if len(args) >= 3 && args[0] == "-m" && args[1] == "pip" && args[2] == "install" {
		return []byte("Successfully installed pdf2docx-0.5.13\n"), nil
	}

	return []byte("fake-version 1.2.3\n"), nil
}

func (r *fakeRunner) recordedCalls() []fakeRunnerCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]fakeRunnerCall{}, r.calls...)
}

func argumentAfter(args []string, flag string) string {
	for index, arg := range args {
		if arg == flag && index+1 < len(args) {
			return args[index+1]
		}
	}

	return ""
}

func writeFakeCondaEnvironment(prefix string) error {
	for _, name := range []string{"ffmpeg", "ffprobe", "tesseract", "pdftoppm", "pdftotext", "gs", "python", "pip"} {
		path := filepath.Join(prefix, "bin", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			return err
		}
	}

	for _, relative := range []string{"pdf.ttf", filepath.Join("configs", "pdf"), filepath.Join("configs", "txt")} {
		path := filepath.Join(prefix, "share", "tessdata", relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("tesseract pdf asset"), 0o644); err != nil {
			return err
		}
	}

	return os.MkdirAll(filepath.Join(prefix, "conda-meta"), 0o755)
}

func linuxToolchainPayloads(t *testing.T) map[string][]byte {
	t.Helper()

	platform := PlatformFor("linux", "amd64")

	archivePath := filepath.Join(t.TempDir(), "whisper.tar.gz")
	writeTarGzFixture(t, archivePath, []archiveTestEntry{
		{name: "whisper-bin-ubuntu-x64/", dir: true, mode: 0o755},
		{name: "whisper-bin-ubuntu-x64/whisper-cli", content: "whisper binary", mode: 0o755},
		{name: "whisper-bin-ubuntu-x64/libwhisper.so.1", content: "whisper library", mode: 0o644},
		{name: "whisper-bin-ubuntu-x64/libggml.so.0", content: "ggml library", mode: 0o644},
		{name: "whisper-bin-ubuntu-x64/libggml-base.so.0", content: "ggml base library", mode: 0o644},
	})
	archivePayload, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read whisper fixture: %v", err)
	}

	smallURL, _ := ModelURL("small")
	mediumURL, _ := ModelURL("medium")
	largeURL, _ := ModelURL("large-v3")
	englishURL, _ := TessdataURL("eng")
	spanishURL, _ := TessdataURL("spa")

	return map[string][]byte{
		platform.MicromambaURL: []byte("#!/bin/sh\necho micromamba\n"),
		platform.WhisperURL:    archivePayload,
		smallURL:               bytes.Repeat([]byte("m"), 512*1024),
		mediumURL:              []byte("medium model"),
		largeURL:               []byte("large model"),
		englishURL:             []byte("english traineddata"),
		spanishURL:             []byte("spanish traineddata"),
	}
}

func preserveToolchainEnvironment(t *testing.T) {
	t.Helper()

	indexPath := os.Getenv("PATH")
	tessdata, hadTessdata := os.LookupEnv("TESSDATA_PREFIX")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", indexPath)
		if hadTessdata {
			_ = os.Setenv("TESSDATA_PREFIX", tessdata)
		} else {
			_ = os.Unsetenv("TESSDATA_PREFIX")
		}
	})
}

func newTestToolchainService(t *testing.T, dataDir string, downloader Downloader, runner CommandRunner) (*Service, <-chan models.ToolchainInstallEventV1) {
	t.Helper()

	events := make(chan models.ToolchainInstallEventV1, 512)
	service := NewService(dataDir, PlatformFor("linux", "amd64"), downloader, runner, func(event models.ToolchainInstallEventV1) {
		events <- event
	})
	t.Cleanup(func() {
		service.Cancel()
	})

	return service, events
}

func waitForTerminalEvent(t *testing.T, events <-chan models.ToolchainInstallEventV1) []models.ToolchainInstallEventV1 {
	t.Helper()

	collected := make([]models.ToolchainInstallEventV1, 0, 64)
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case event := <-events:
			collected = append(collected, event)
			if event.Done {
				return collected
			}
		case <-timeout.C:
			t.Fatalf("timed out waiting for the terminal event; collected %d events: %+v", len(collected), collected)
		}
	}
}

func componentsByID(t *testing.T, status models.ToolchainStatusV1) map[string]models.ToolchainComponentV1 {
	t.Helper()

	byID := make(map[string]models.ToolchainComponentV1, len(status.Components))
	for _, component := range status.Components {
		if _, exists := byID[component.ID]; exists {
			t.Fatalf("duplicate component id %q", component.ID)
		}
		byID[component.ID] = component
	}

	return byID
}

func TestStatusWithoutDataDir(t *testing.T) {
	service := NewService("", PlatformFor("linux", "amd64"), &fakeDownloader{}, &fakeRunner{}, nil)

	status := service.Status()
	if status.Success {
		t.Fatalf("expected an unconfigured service to fail the status: %+v", status)
	}
	if status.Ready || status.Installing {
		t.Fatalf("unexpected unconfigured status: %+v", status)
	}
	if status.Message == "" {
		t.Fatal("expected a message for an unconfigured service")
	}

	started := service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	if started.Success || started.Error == "" {
		t.Fatalf("expected Start to fail without a data directory: %+v", started)
	}
}

func TestStatusBeforeInstall(t *testing.T) {
	service, _ := newTestToolchainService(t, t.TempDir(), &fakeDownloader{}, &fakeRunner{})

	status := service.Status()
	if status.Ready {
		t.Fatal("expected a fresh data directory to be not ready")
	}
	if status.Installing {
		t.Fatal("expected no install before Start")
	}
	if status.Model != DefaultModel {
		t.Fatalf("model = %q, want %q", status.Model, DefaultModel)
	}
	if !reflect.DeepEqual(status.OCRLanguages, SupportedOCRLanguages()) {
		t.Fatalf("languages = %v, want %v", status.OCRLanguages, SupportedOCRLanguages())
	}
	if !strings.HasSuffix(status.StorageDir, filepath.Join("", ToolchainDirName)) {
		t.Fatalf("unexpected storage dir: %q", status.StorageDir)
	}

	expected := []string{
		"micromamba", "ffmpeg", "ffprobe", "tesseract", "pdftoppm", "pdftotext",
		"ghostscript", "whisper-cli", "whisper-model", "tesseract-eng", "tesseract-spa",
		"tesseract-pdf", "pdf2docx",
	}
	if len(status.Components) != len(expected) {
		t.Fatalf("expected %d components, got %d: %+v", len(expected), len(status.Components), status.Components)
	}
	byID := componentsByID(t, status)
	for _, id := range expected {
		if _, exists := byID[id]; !exists {
			t.Fatalf("missing component %q", id)
		}
		if byID[id].Ready {
			t.Fatalf("component %q must not be ready before the install", id)
		}
	}
	if !byID["ffmpeg"].Required || !byID["whisper-model"].Required || !byID["tesseract-eng"].Required || !byID["tesseract-pdf"].Required {
		t.Fatal("expected ffmpeg, whisper-model, tesseract-eng and tesseract-pdf to be required")
	}
	if byID["micromamba"].Required || byID["ghostscript"].Required || byID["pdftotext"].Required || byID["pdf2docx"].Required {
		t.Fatal("expected micromamba, ghostscript, pdftotext and pdf2docx to be optional")
	}
}

func TestInstallCreatesReadyToolchain(t *testing.T) {
	preserveToolchainEnvironment(t)
	t.Setenv("MAMBA_ROOT_PREFIX", "/ambient/mamba")

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	runner := &fakeRunner{}
	service, events := newTestToolchainService(t, dataDir, downloader, runner)

	started := service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	if !started.Installing {
		t.Fatalf("expected the install to start, got %+v", started)
	}
	if started.Model != DefaultModel || !reflect.DeepEqual(started.OCRLanguages, SupportedOCRLanguages()) {
		t.Fatalf("unexpected install defaults: %+v", started)
	}

	collected := waitForTerminalEvent(t, events)

	status := service.Status()
	if !status.Ready {
		t.Fatalf("expected a ready toolchain, got %+v", status)
	}
	if status.Installing || status.Cancelling {
		t.Fatalf("expected a finished install, got %+v", status)
	}
	if status.Error != "" {
		t.Fatalf("unexpected error: %s", status.Error)
	}
	if status.Percent != 100 || status.Phase != PhaseReady {
		t.Fatalf("unexpected final progress: %d%% %q", status.Percent, status.Phase)
	}
	if status.Model != DefaultModel || status.ModelPath != filepath.Join(dataDir, ToolchainDirName, "models", ModelFileName(DefaultModel)) {
		t.Fatalf("unexpected model status: %+v", status)
	}
	if !reflect.DeepEqual(status.OCRLanguages, []string{"eng", "spa"}) {
		t.Fatalf("unexpected languages: %v", status.OCRLanguages)
	}

	byID := componentsByID(t, status)
	for _, id := range []string{"ffmpeg", "ffprobe", "tesseract", "pdftoppm", "pdftotext", "ghostscript", "whisper-cli", "whisper-model", "tesseract-eng", "tesseract-spa", "pdf2docx"} {
		component := byID[id]
		if !component.Ready {
			t.Fatalf("component %q is not ready: %+v", id, component)
		}
		if component.Path == "" {
			t.Fatalf("component %q has no path", id)
		}
	}
	for _, id := range []string{"ffmpeg", "tesseract", "ghostscript"} {
		if byID[id].Version == "" {
			t.Fatalf("expected a detected version for %q", id)
		}
	}

	root := filepath.Join(dataDir, ToolchainDirName)
	requiredFiles := []string{
		filepath.Join(root, "micromamba"),
		filepath.Join(root, "env", "bin", "ffmpeg"),
		filepath.Join(root, "env", "bin", "tesseract"),
		filepath.Join(root, "env", "bin", "python"),
		filepath.Join(root, "whisper", "whisper-cli"),
		filepath.Join(root, "models", ModelFileName(DefaultModel)),
		filepath.Join(root, "tessdata", "eng.traineddata"),
		filepath.Join(root, "tessdata", "spa.traineddata"),
		filepath.Join(root, "pdf2docx-venv", "bin", "python"),
		filepath.Join(root, "pdf2docx-venv", "bin", "pdf2docx"),
	}
	for _, path := range requiredFiles {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if info.IsDir() {
			t.Fatalf("expected a file at %s", path)
		}
	}

	micromambaInfo, err := os.Stat(filepath.Join(root, "micromamba"))
	if err != nil {
		t.Fatalf("stat micromamba: %v", err)
	}
	if micromambaInfo.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected the micromamba executable bit, got %v", micromambaInfo.Mode().Perm())
	}

	if _, err := os.Stat(filepath.Join(root, "staging")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the staging folder to be cleaned, got %v", err)
	}

	// The conda environment is created with the managed mamba root.
	createCall := findRunnerCall(t, runner.recordedCalls(), "create")
	if !containsAll(createCall.args, "ffmpeg", "tesseract", "poppler", "ghostscript", "conda-forge", "python=3.11", "pip") {
		t.Fatalf("unexpected micromamba create args: %v", createCall.args)
	}
	if !containsAll(createCall.env, "MAMBA_ROOT_PREFIX="+filepath.Join(root, "mamba-root")) {
		t.Fatalf("expected MAMBA_ROOT_PREFIX in the create environment")
	}
	if count := countEnvironmentKey(createCall.env, "MAMBA_ROOT_PREFIX"); count != 1 {
		t.Fatalf("expected one MAMBA_ROOT_PREFIX entry, got %d in %v", count, createCall.env)
	}
	if containsAll(createCall.env, "MAMBA_ROOT_PREFIX=/ambient/mamba") {
		t.Fatal("an ambient MAMBA_ROOT_PREFIX must not win over the managed root")
	}

	// The PDF to DOCX venv is created from the managed Python and filled by pip
	// with the pinned package set.
	venvCall := findRunnerCallStartingWith(t, runner.recordedCalls(), "-m", "venv")
	if venvCall.name != filepath.Join(root, "env", "bin", "python") {
		t.Fatalf("unexpected venv interpreter: %s", venvCall.name)
	}
	pipCall := findRunnerCallStartingWith(t, runner.recordedCalls(), "-m", "pip", "install")
	if pipCall.name != filepath.Join(root, "pdf2docx-venv", "bin", "python") {
		t.Fatalf("pip must run inside the pdf2docx venv, got %s", pipCall.name)
	}
	if !containsAll(pipCall.args, pdf2docxPins...) || !containsAll(pipCall.args, "--no-cache-dir", "--disable-pip-version-check") {
		t.Fatalf("unexpected pinned package install: %v", pipCall.args)
	}

	manifest := readManifestForTest(t, filepath.Join(root, "manifest.json"))
	if manifest.Model != DefaultModel {
		t.Fatalf("manifest model = %q", manifest.Model)
	}
	if !reflect.DeepEqual(manifest.OCRLanguages, []string{"eng", "spa"}) {
		t.Fatalf("manifest languages = %v", manifest.OCRLanguages)
	}
	if manifest.GeneratedAt == "" {
		t.Fatal("expected a manifest timestamp")
	}
	if len(manifest.Components) == 0 {
		t.Fatal("expected manifest components")
	}

	// Events: starting first, terminal last, percents never decrease.
	if len(collected) < 2 || collected[0].Phase != PhaseStarting {
		t.Fatalf("unexpected first events: %+v", collected)
	}
	terminal := collected[len(collected)-1]
	if !terminal.Done || terminal.Error != "" || terminal.Percent != 100 || terminal.Phase != PhaseReady {
		t.Fatalf("unexpected terminal event: %+v", terminal)
	}
	lastPercent := -1
	for _, event := range collected {
		if event.Percent < lastPercent {
			t.Fatalf("percent went backwards: %d after %d in %+v", event.Percent, lastPercent, event)
		}
		lastPercent = event.Percent
	}

	// ApplyEnvironment ran during finalize.
	if tessdata := os.Getenv("TESSDATA_PREFIX"); tessdata != filepath.Join(root, "tessdata") {
		t.Fatalf("TESSDATA_PREFIX = %q", tessdata)
	}
	managedBin := filepath.Join(root, "env", "bin")
	if path := os.Getenv("PATH"); !strings.HasPrefix(path, managedBin+string(filepath.ListSeparator)) {
		t.Fatalf("expected the managed bin folder at the front of PATH, got %q", path)
	}
}

func TestStatusToleratesVersionProbeFailures(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	service, events := newTestToolchainService(t, dataDir, &fakeDownloader{payloads: linuxToolchainPayloads(t)}, &fakeRunner{failVersions: true})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)

	status := service.Status()
	if !status.Ready {
		t.Fatalf("version failures must not fail the status: %+v", status)
	}
	for _, component := range status.Components {
		if component.Required && component.Version != "" {
			t.Fatalf("expected no version for %q after a probe failure, got %q", component.ID, component.Version)
		}
	}
}

func TestInstallSkipsAlreadyPresentComponents(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	runner := &fakeRunner{}
	service, events := newTestToolchainService(t, dataDir, downloader, runner)

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	firstModelCalls := downloader.callsFor(mustModelURL(t, DefaultModel))

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)

	if got := downloader.callsFor(mustModelURL(t, DefaultModel)); got != firstModelCalls {
		t.Fatalf("model downloaded again without Force: %d calls, want %d", got, firstModelCalls)
	}
	if !service.Status().Ready {
		t.Fatal("expected the repaired install to stay ready")
	}

	// The prefix already exists on the second run, so micromamba repairs it
	// with install instead of recreating it with create.
	installCalls := 0
	for _, call := range runner.recordedCalls() {
		if len(call.args) > 0 && call.args[0] == "install" {
			installCalls++
		}
	}
	if installCalls != 1 {
		t.Fatalf("expected one micromamba install call on the repair run, got %d", installCalls)
	}

	// The pdf2docx virtual environment is reused instead of reinstalled.
	pipCalls := 0
	for _, call := range runner.recordedCalls() {
		if len(call.args) >= 3 && call.args[0] == "-m" && call.args[1] == "pip" && call.args[2] == "install" {
			pipCalls++
		}
	}
	if pipCalls != 1 {
		t.Fatalf("expected the pdf2docx venv to be reused without Force, got %d pip installs", pipCalls)
	}
}

func TestInstallForceDownloadsAgain(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	runner := &fakeRunner{}
	service, events := newTestToolchainService(t, dataDir, downloader, runner)

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	firstModelCalls := downloader.callsFor(mustModelURL(t, DefaultModel))

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	waitForTerminalEvent(t, events)

	if got := downloader.callsFor(mustModelURL(t, DefaultModel)); got != firstModelCalls+1 {
		t.Fatalf("expected a forced model download, got %d calls, want %d", got, firstModelCalls+1)
	}
	if !service.Status().Ready {
		t.Fatal("expected the forced install to be ready")
	}

	// Force recreates the conda prefix instead of repairing it.
	createCalls := 0
	for _, call := range runner.recordedCalls() {
		if len(call.args) > 0 && call.args[0] == "create" {
			createCalls++
		}
	}
	if createCalls != 2 {
		t.Fatalf("expected the forced install to recreate the environment, got %d create calls", createCalls)
	}
}

func TestStartIfNeededInstallsAndThenSkips(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.StartIfNeeded(context.Background())
	if !service.Status().Installing {
		t.Fatal("expected StartIfNeeded to start an install")
	}
	waitForTerminalEvent(t, events)

	if !service.Status().Ready {
		t.Fatal("expected the toolchain to be ready")
	}
	calls := downloader.callCount()

	service.StartIfNeeded(context.Background())
	time.Sleep(50 * time.Millisecond)

	if service.Status().Installing {
		t.Fatal("StartIfNeeded must not reinstall a ready toolchain")
	}
	if downloader.callCount() != calls {
		t.Fatalf("StartIfNeeded downloaded again: %d calls, want %d", downloader.callCount(), calls)
	}
}

func TestInstallFailureKeepsValidComponentsForRetry(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	platform := PlatformFor("linux", "amd64")
	downloader := &fakeDownloader{
		payloads: linuxToolchainPayloads(t),
		failURL:  platform.WhisperURL,
	}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	collected := waitForTerminalEvent(t, events)

	terminal := collected[len(collected)-1]
	if terminal.Error == "" || !terminal.Done {
		t.Fatalf("expected a terminal error event, got %+v", terminal)
	}

	status := service.Status()
	if status.Ready {
		t.Fatal("a failed install must not be ready")
	}
	if status.Installing || status.Cancelling {
		t.Fatalf("unexpected install flags: %+v", status)
	}
	if status.Error == "" {
		t.Fatal("expected the status to keep the install error")
	}
	byID := componentsByID(t, status)
	if byID["whisper-cli"].Ready || byID["whisper-model"].Ready {
		t.Fatal("incomplete components must not be marked ready")
	}
	if !byID["ffmpeg"].Ready {
		t.Fatal("components installed before the failure must stay ready for a retry")
	}

	root := filepath.Join(dataDir, ToolchainDirName)
	if _, err := os.Stat(filepath.Join(root, "staging")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the staging folder to be cleaned after a failure, got %v", err)
	}
}

func TestInstallEnvironmentFailureReportsError(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	service, events := newTestToolchainService(t, dataDir, &fakeDownloader{payloads: linuxToolchainPayloads(t)}, &fakeRunner{failOn: "create"})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	collected := waitForTerminalEvent(t, events)

	terminal := collected[len(collected)-1]
	if terminal.Error == "" {
		t.Fatalf("expected an error terminal event, got %+v", terminal)
	}
	status := service.Status()
	if status.Ready || status.Error == "" {
		t.Fatalf("unexpected status after a failed environment: %+v", status)
	}
	if componentsByID(t, status)["ffmpeg"].Ready {
		t.Fatal("an environment failure must not mark ffmpeg ready")
	}
}

func TestCancelStopsInstallAndCleansStaging(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	downloader.blockURL = mustModelURL(t, DefaultModel)
	downloader.started = make(chan string, 1)

	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	select {
	case <-downloader.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the install did not reach the model download")
	}

	cancelling := service.Cancel()
	if !cancelling.Cancelling {
		t.Fatalf("expected a cancelling status, got %+v", cancelling)
	}

	collected := waitForTerminalEvent(t, events)
	terminal := collected[len(collected)-1]
	if terminal.Phase != PhaseCancelled || terminal.Error != "" {
		t.Fatalf("unexpected terminal event after cancel: %+v", terminal)
	}

	status := service.Status()
	if status.Installing || status.Cancelling {
		t.Fatalf("expected the install to stop: %+v", status)
	}
	if status.Ready {
		t.Fatal("a cancelled install must not be ready")
	}
	if status.Phase != PhaseCancelled {
		t.Fatalf("phase = %q, want %q", status.Phase, PhaseCancelled)
	}
	if status.Error != "" {
		t.Fatalf("a cancellation is not an error, got %q", status.Error)
	}

	root := filepath.Join(dataDir, ToolchainDirName)
	if _, err := os.Stat(filepath.Join(root, "staging")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the staging folder to be cleaned, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "models", ModelFileName(DefaultModel))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no model file after cancel, got %v", err)
	}
}

func TestStartIsNotDuplicatedWhileInstalling(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	downloader.blockURL = mustModelURL(t, DefaultModel)
	downloader.started = make(chan string, 1)
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	<-downloader.started

	second := service.Start(context.Background(), models.ToolchainInstallOptionsV1{Model: "medium"})
	if !second.Installing {
		t.Fatalf("unexpected second start status: %+v", second)
	}
	if second.Model != DefaultModel {
		t.Fatalf("a second Start must not change the running model, got %q", second.Model)
	}
	if calls := downloader.callsFor(mustModelURL(t, DefaultModel)); calls != 1 {
		t.Fatalf("model download calls = %d, want 1", calls)
	}

	service.Cancel()
	waitForTerminalEvent(t, events)
}

func TestStartValidatesOptions(t *testing.T) {
	service, _ := newTestToolchainService(t, t.TempDir(), &fakeDownloader{}, &fakeRunner{})

	tests := []models.ToolchainInstallOptionsV1{
		{Model: "tiny"},
		{OCRLanguages: []string{"fra"}},
	}

	for _, options := range tests {
		status := service.Start(context.Background(), options)
		if status.Success {
			t.Fatalf("expected a validation failure for %+v", options)
		}
		if status.Error == "" {
			t.Fatalf("expected an error message for %+v", options)
		}
		if status.Installing {
			t.Fatalf("validation failures must not start an install: %+v", options)
		}
	}
}

func TestStartRejectsUnsupportedPlatform(t *testing.T) {
	downloader := &fakeDownloader{}
	service := NewService(t.TempDir(), PlatformFor("windows", "arm64"), downloader, &fakeRunner{}, nil)

	status := service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	if status.Success {
		t.Fatal("expected an unsupported platform failure")
	}
	if !strings.Contains(status.Error, "Windows on ARM") {
		t.Fatalf("unexpected error: %q", status.Error)
	}
	if downloader.callCount() != 0 {
		t.Fatal("an unsupported platform must not download anything")
	}

	service.StartIfNeeded(context.Background())
	if downloader.callCount() != 0 {
		t.Fatal("StartIfNeeded must not download on an unsupported platform")
	}
}

func TestInstallSelectedModelAndLanguages(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{
		Model:        "medium",
		OCRLanguages: []string{"eng"},
	})
	waitForTerminalEvent(t, events)

	status := service.Status()
	if !status.Ready {
		t.Fatalf("expected a ready toolchain, got %+v", status)
	}
	if status.Model != "medium" {
		t.Fatalf("model = %q, want medium", status.Model)
	}
	if !reflect.DeepEqual(status.OCRLanguages, []string{"eng"}) {
		t.Fatalf("languages = %v, want [eng]", status.OCRLanguages)
	}
	byID := componentsByID(t, status)
	if !byID["whisper-model"].Ready || byID["whisper-model"].Path != filepath.Join(dataDir, ToolchainDirName, "models", "ggml-medium.bin") {
		t.Fatalf("unexpected model component: %+v", byID["whisper-model"])
	}
	if !byID["tesseract-eng"].Ready {
		t.Fatal("expected eng language data")
	}
	if byID["tesseract-spa"].Ready || byID["tesseract-spa"].Required {
		t.Fatalf("unexpected spa component for an eng-only install: %+v", byID["tesseract-spa"])
	}
	if downloader.callsFor(mustModelURL(t, "medium")) != 1 {
		t.Fatal("expected the medium model to be downloaded")
	}
	if downloader.callsFor(mustModelURL(t, "small")) != 0 {
		t.Fatal("the non-selected small model must not be downloaded when medium is selected")
	}
}

func TestMissingWhisperSharedLibraryIsNotReadyAndRepairs(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	service, events := newTestToolchainService(t, dataDir, &fakeDownloader{payloads: linuxToolchainPayloads(t)}, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	if !service.Status().Ready {
		t.Fatal("expected the initial install to be ready")
	}

	library := filepath.Join(dataDir, ToolchainDirName, "whisper", "libwhisper.so.1")
	if err := os.Remove(library); err != nil {
		t.Fatalf("remove whisper library: %v", err)
	}

	status := service.Status()
	if status.Ready {
		t.Fatal("a runtime without libwhisper.so.1 must not be ready")
	}
	component := componentsByID(t, status)["whisper-cli"]
	if component.Ready || !strings.Contains(component.Detail, "shared libraries") {
		t.Fatalf("expected an incomplete-runtime detail, got %+v", component)
	}

	service.StartIfNeeded(context.Background())
	if !service.Status().Installing {
		t.Fatal("expected StartIfNeeded to repair the broken runtime")
	}
	waitForTerminalEvent(t, events)
	if !service.Status().Ready {
		t.Fatal("expected the repaired runtime to be ready")
	}
	if _, err := os.Lstat(library); err != nil {
		t.Fatalf("expected the soname symlink/library to be restored: %v", err)
	}
}

func TestMissingTessdataPDFAssetsIsNotReadyAndRepairs(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	service, events := newTestToolchainService(t, dataDir, &fakeDownloader{payloads: linuxToolchainPayloads(t)}, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	if !service.Status().Ready {
		t.Fatal("expected the initial install to be ready")
	}

	pdfConfig := filepath.Join(dataDir, ToolchainDirName, "tessdata", "configs", "pdf")
	if err := os.Remove(pdfConfig); err != nil {
		t.Fatalf("remove tessdata pdf config: %v", err)
	}

	status := service.Status()
	if status.Ready {
		t.Fatal("a tessdata folder without the PDF config must not be ready")
	}
	component := componentsByID(t, status)["tesseract-pdf"]
	if component.Ready || !strings.Contains(component.Detail, "incomplete") {
		t.Fatalf("expected an incomplete Tesseract PDF detail, got %+v", component)
	}

	service.StartIfNeeded(context.Background())
	if !service.Status().Installing {
		t.Fatal("expected StartIfNeeded to repair the missing PDF assets")
	}
	waitForTerminalEvent(t, events)
	if !service.Status().Ready {
		t.Fatal("expected the repaired toolchain to be ready")
	}
	if _, err := os.Lstat(pdfConfig); err != nil {
		t.Fatalf("expected the PDF config to be restored: %v", err)
	}
}

func TestPdf2DocxVenvIsOptionalAndRepairs(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	runner := &fakeRunner{}
	service, events := newTestToolchainService(t, dataDir, &fakeDownloader{payloads: linuxToolchainPayloads(t)}, runner)

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	if !service.Status().Ready {
		t.Fatal("expected the initial install to be ready")
	}

	root := filepath.Join(dataDir, ToolchainDirName)
	venvDir := filepath.Join(root, "pdf2docx-venv")
	if err := os.RemoveAll(venvDir); err != nil {
		t.Fatalf("remove the pdf2docx venv: %v", err)
	}

	status := service.Status()
	if !status.Ready {
		t.Fatalf("an optional pdf2docx engine must not block readiness: %+v", status)
	}
	component := componentsByID(t, status)["pdf2docx"]
	if component.Ready || component.Required {
		t.Fatalf("expected an optional, not ready pdf2docx component: %+v", component)
	}
	if !strings.Contains(component.Detail, "Install / repair") {
		t.Fatalf("expected a repair detail, got %+v", component)
	}
	if _, found := ManagedPDF2DOCXPath(dataDir); found {
		t.Fatal("the managed pdf2docx binary must not be reported without the venv")
	}
	if _, found := ManagedPDF2DOCXPythonPath(dataDir); found {
		t.Fatal("the managed pdf2docx interpreter must not be reported without the venv")
	}

	// An optional component alone must not trigger a background reinstall.
	service.StartIfNeeded(context.Background())
	time.Sleep(50 * time.Millisecond)
	if service.Status().Installing {
		t.Fatal("StartIfNeeded must not reinstall when only the optional pdf2docx venv is missing")
	}

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	if !service.Status().Installing {
		t.Fatal("expected the forced install to start")
	}
	waitForTerminalEvent(t, events)

	status = service.Status()
	if !status.Ready {
		t.Fatalf("expected the repaired toolchain to be ready: %+v", status)
	}
	component = componentsByID(t, status)["pdf2docx"]
	if !component.Ready || component.Path != filepath.Join(venvDir, "bin", "python") {
		t.Fatalf("expected a ready pdf2docx component pointing at the venv interpreter: %+v", component)
	}
	for _, path := range []string{
		filepath.Join(venvDir, "bin", "python"),
		filepath.Join(venvDir, "bin", "pdf2docx"),
	} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			t.Fatalf("expected the restored venv file %s: %v", path, err)
		}
	}
	if path, found := ManagedPDF2DOCXPath(dataDir); !found || path != filepath.Join(venvDir, "bin", "pdf2docx") {
		t.Fatalf("unexpected managed pdf2docx path: %q %v", path, found)
	}
}

func findRunnerCall(t *testing.T, calls []fakeRunnerCall, action string) fakeRunnerCall {
	t.Helper()

	for _, call := range calls {
		if len(call.args) > 0 && call.args[0] == action {
			return call
		}
	}
	t.Fatalf("no micromamba %q call found in %+v", action, calls)

	return fakeRunnerCall{}
}

func findRunnerCallStartingWith(t *testing.T, calls []fakeRunnerCall, prefix ...string) fakeRunnerCall {
	t.Helper()

	for _, call := range calls {
		if len(call.args) < len(prefix) {
			continue
		}

		matches := true
		for index, value := range prefix {
			if call.args[index] != value {
				matches = false
				break
			}
		}
		if matches {
			return call
		}
	}
	t.Fatalf("no runner call starting with %v found in %+v", prefix, calls)

	return fakeRunnerCall{}
}

func containsAll(values []string, expected ...string) bool {
	for _, wanted := range expected {
		found := false
		for _, value := range values {
			if value == wanted {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func countEnvironmentKey(values []string, key string) int {
	count := 0
	prefix := key + "="
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			count++
		}
	}

	return count
}

func TestWithEnvironmentReplacesExistingKeys(t *testing.T) {
	merged := withEnvironment(
		[]string{"PATH=/usr/bin", "MAMBA_ROOT_PREFIX=/old", "HOME=/home/ivan"},
		map[string]string{"MAMBA_ROOT_PREFIX": "/managed"},
	)

	if len(merged) != 3 {
		t.Fatalf("unexpected environment: %v", merged)
	}
	if merged[2] != "MAMBA_ROOT_PREFIX=/managed" {
		t.Fatalf("expected the managed value last, got %v", merged)
	}
	if countEnvironmentKey(merged, "MAMBA_ROOT_PREFIX") != 1 {
		t.Fatalf("expected a single MAMBA_ROOT_PREFIX, got %v", merged)
	}
}

func readManifestForTest(t *testing.T, path string) manifestV1 {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var manifest manifestV1
	if err := json.Unmarshal(payload, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	return manifest
}

func mustModelURL(t *testing.T, model string) string {
	t.Helper()

	url, err := ModelURL(model)
	if err != nil {
		t.Fatalf("model URL for %q: %v", model, err)
	}

	return url
}

// TestToolchainRealInstallE2E downloads and installs the real toolchain into a
// temporary data directory. It is opt-in because it uses the network and
// downloads the Whisper model (hundreds of MB).
func TestToolchainRealInstallE2E(t *testing.T) {
	if os.Getenv("FILEFORGE_TOOLCHAIN_E2E") != "1" {
		t.Skip("set FILEFORGE_TOOLCHAIN_E2E=1 to run the real toolchain install")
	}

	preserveToolchainEnvironment(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	service := NewService(t.TempDir(), CurrentPlatform(), &HTTPDownloader{}, &ExecCommandRunner{}, nil)
	if !service.platform.Supported {
		t.Skipf("the managed toolchain is not supported here: %s", service.platform.Reason)
	}

	service.Start(ctx, models.ToolchainInstallOptionsV1{})
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		status := service.Status()
		if !status.Installing {
			if !status.Ready {
				t.Fatalf("real install did not become ready: %+v", status)
			}

			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("timed out waiting for the real toolchain install")
}
