package toolchain

import (
	"bytes"
	"context"
	"crypto/sha256"
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

// DownloadArtifact delegates to Download so the install tests keep exercising
// the service wiring with small fake payloads instead of the real checksums.
func (d *fakeDownloader) DownloadArtifact(ctx context.Context, artifact Artifact, destPath string, onProgress func(downloaded, total int64)) error {
	return d.Download(ctx, artifact.URL, destPath, onProgress)
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
	// failRembgPip fails only the pip install that targets rembg-venv, so a
	// forced rebuild can be tested without breaking the pdf2docx install.
	failRembgPip bool
}

func (r *fakeRunner) Run(ctx context.Context, name string, args []string, env []string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, fakeRunnerCall{name: name, args: append([]string{}, args...), env: append([]string{}, env...)})
	fail := r.failOn != "" && len(args) > 0 && args[0] == r.failOn
	failVersion := r.failVersions && len(args) > 0 && (args[0] == "--version" || args[0] == "-version")
	failRembgPip := r.failRembgPip
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

	// The engine virtual environments are created from the managed Python and
	// filled by pip. The background removal venv also needs the package markers
	// the installer verifies before writing its readiness file.
	if len(args) >= 3 && args[0] == "-m" && args[1] == "venv" {
		venvDir := args[2]
		names := []string{"python"}
		if filepath.Base(venvDir) != "rembg-venv" {
			names = append(names, "pdf2docx")
		}
		for _, name := range names {
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
		venvDir := filepath.Dir(filepath.Dir(name))
		if filepath.Base(venvDir) == "rembg-venv" {
			if failRembgPip {
				return nil, fmt.Errorf("fake rembg pip install failed")
			}
			if err := writeFakeRembgPackages(venvDir); err != nil {
				return nil, err
			}

			return []byte("Successfully installed rembg-2.0.87\n"), nil
		}

		return []byte("Successfully installed pdf2docx-0.5.13\n"), nil
	}

	return []byte("fake-version 1.2.3\n"), nil
}

// writeFakeRembgPackages simulates a pip install of rembg and onnxruntime by
// creating the package file and the pip receipt the installer checks.
func writeFakeRembgPackages(venvDir string) error {
	sitePackages := filepath.Join(venvDir, "lib", "python3.11", "site-packages")

	initPath := filepath.Join(sitePackages, "rembg", "__init__.py")
	if err := os.MkdirAll(filepath.Dir(initPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(initPath, []byte("__version__ = '2.0.87'\n"), 0o644); err != nil {
		return err
	}

	return os.MkdirAll(filepath.Join(sitePackages, "onnxruntime-1.32.0.dist-info"), 0o755)
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
	rembgURL, err := RembgModelURL(DefaultRembgModel)
	if err != nil {
		t.Fatalf("background removal model URL: %v", err)
	}

	return map[string][]byte{
		platform.MicromambaURL: []byte("#!/bin/sh\necho micromamba\n"),
		platform.WhisperURL:    archivePayload,
		smallURL:               bytes.Repeat([]byte("m"), 512*1024),
		mediumURL:              []byte("medium model"),
		largeURL:               []byte("large model"),
		englishURL:             []byte("english traineddata"),
		spanishURL:             []byte("spanish traineddata"),
		rembgURL:               fakeRembgModelPayload(t),
	}
}

// fakeRembgModelPayload returns a small stand-in for the 179 MB ONNX model and
// points the expected size and checksum at it for the duration of the test, so
// the real verification path runs without writing the real weights.
func fakeRembgModelPayload(t *testing.T) []byte {
	t.Helper()

	payload := []byte("fake isnet-general-use onnx payload")
	original := rembgModelSpec
	rembgModelSpec.SizeBytes = int64(len(payload))
	rembgModelSpec.SHA256 = fmt.Sprintf("%x", sha256.Sum256(payload))
	t.Cleanup(func() {
		rembgModelSpec = original
	})

	return payload
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
		"tesseract-pdf", "pdf2docx", "rembg", "rembg-model",
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
	if byID["micromamba"].Required || byID["ghostscript"].Required || byID["pdftotext"].Required || byID["pdf2docx"].Required || byID["rembg"].Required || byID["rembg-model"].Required {
		t.Fatal("expected micromamba, ghostscript, pdftotext, pdf2docx, rembg and rembg-model to be optional")
	}
	if byID["rembg"].Path != "" || byID["rembg-model"].Path != "" {
		t.Fatal("optional pack components must not expose a path before they are ready")
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
	for _, id := range []string{"ffmpeg", "ffprobe", "tesseract", "pdftoppm", "pdftotext", "ghostscript", "whisper-cli", "whisper-model", "tesseract-eng", "tesseract-spa", "pdf2docx", "rembg", "rembg-model"} {
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
		filepath.Join(root, "rembg-venv", "bin", "python"),
		filepath.Join(root, "rembg-venv", "lib", "python3.11", "site-packages", "rembg", "__init__.py"),
		filepath.Join(root, "rembg", ".ready"),
		filepath.Join(root, "rembg", "models", DefaultRembgModel, RembgModelFileName(DefaultRembgModel)),
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
	if !directoryExists(filepath.Join(root, "rembg-venv", "lib", "python3.11", "site-packages", "onnxruntime-1.32.0.dist-info")) {
		t.Fatal("expected the onnxruntime dist-info directory in the background removal venv")
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

	// The background removal venv is created the same way and filled with its
	// own pinned package set, then imported before the readiness marker.
	rembgPython := filepath.Join(root, "rembg-venv", "bin", "python")
	rembgCalls := runnerCallsForBinary(runner.recordedCalls(), rembgPython)
	rembgPipCall := findRunnerCallStartingWith(t, rembgCalls, "-m", "pip", "install")
	if !containsAll(rembgPipCall.args, rembgPins...) || !containsAll(rembgPipCall.args, "--no-cache-dir", "--disable-pip-version-check") {
		t.Fatalf("unexpected background removal package install: %v", rembgPipCall.args)
	}
	importCall := findRunnerCallStartingWith(t, rembgCalls, "-c")
	if len(importCall.args) != 2 || importCall.args[1] != "import rembg, onnxruntime" {
		t.Fatalf("unexpected background removal import check: %v", importCall.args)
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

	// Both engine virtual environments are reused instead of reinstalled.
	root := filepath.Join(dataDir, ToolchainDirName)
	if calls := countRunnerCallsForBinary(runner.recordedCalls(), filepath.Join(root, "pdf2docx-venv", "bin", "python")); calls != 1 {
		t.Fatalf("expected the pdf2docx venv to be reused without Force, got %d pip installs", calls)
	}
	if calls := countRunnerCallsForBinary(runner.recordedCalls(), filepath.Join(root, "rembg-venv", "bin", "python")); calls != 2 {
		// One pip install plus one import check on the first run; the repair
		// run must add nothing because the marker exists.
		t.Fatalf("expected the background removal venv to be reused without Force, got %d calls", calls)
	}
	rembgURL, err := RembgModelURL(DefaultRembgModel)
	if err != nil {
		t.Fatalf("background removal model URL: %v", err)
	}
	if calls := downloader.callsFor(rembgURL); calls != 1 {
		t.Fatalf("background removal model downloaded again without Force: %d calls, want 1", calls)
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
	// The fake installer can finish before Start returns, so wait for the new
	// terminal event instead of racing the in-memory install status.
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

func TestRembgVenvIsOptionalAndRepairs(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	runner := &fakeRunner{}
	service, events := newTestToolchainService(t, dataDir, downloader, runner)

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	if !service.Status().Ready {
		t.Fatal("expected the initial install to be ready")
	}

	root := filepath.Join(dataDir, ToolchainDirName)
	venvDir := filepath.Join(root, "rembg-venv")
	modelPath := filepath.Join(root, "rembg", "models", DefaultRembgModel, RembgModelFileName(DefaultRembgModel))
	modelURL, err := RembgModelURL(DefaultRembgModel)
	if err != nil {
		t.Fatalf("background removal model URL: %v", err)
	}

	if info, err := os.Stat(modelPath); err != nil || info.IsDir() {
		t.Fatalf("expected the installed model at %s: %v", modelPath, err)
	}
	// Pin the mtime so a forced repair that keeps the model is provable.
	oldTime := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(modelPath, oldTime, oldTime); err != nil {
		t.Fatalf("pin the model mtime: %v", err)
	}

	if err := os.RemoveAll(venvDir); err != nil {
		t.Fatalf("remove the background removal venv: %v", err)
	}

	status := service.Status()
	if !status.Ready {
		t.Fatalf("an optional background removal engine must not block readiness: %+v", status)
	}
	byID := componentsByID(t, status)
	component := byID["rembg"]
	if component.Ready || component.Required {
		t.Fatalf("expected an optional, not ready rembg component: %+v", component)
	}
	if component.Path != "" {
		t.Fatalf("a not ready rembg component must not expose a path: %+v", component)
	}
	if !strings.Contains(component.Detail, "Install / repair") {
		t.Fatalf("expected a repair detail, got %+v", component)
	}
	modelComponent := byID["rembg-model"]
	if !modelComponent.Ready || modelComponent.Required {
		t.Fatalf("expected an optional, ready rembg model component: %+v", modelComponent)
	}
	if modelComponent.Path != modelPath {
		t.Fatalf("model component path = %q, want %q", modelComponent.Path, modelPath)
	}
	if _, found := ManagedRembgPythonPath(dataDir); found {
		t.Fatal("the managed background removal interpreter must not be reported without the venv")
	}
	if home, found := ManagedRembgHome(dataDir); !found || home != filepath.Join(root, "rembg") {
		t.Fatalf("unexpected managed background removal home: %q %v", home, found)
	}
	if path, found := ManagedRembgModelPath(dataDir, DefaultRembgModel); !found || path != modelPath {
		t.Fatalf("unexpected managed background removal model path: %q %v", path, found)
	}

	// An optional component alone must not trigger a background reinstall.
	service.StartIfNeeded(context.Background())
	time.Sleep(50 * time.Millisecond)
	if service.Status().Installing {
		t.Fatal("StartIfNeeded must not reinstall when only the optional background removal venv is missing")
	}

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	// The fake installer can finish before Start returns, so wait for the new
	// terminal event instead of racing the in-memory install status.
	waitForTerminalEvent(t, events)

	status = service.Status()
	if !status.Ready {
		t.Fatalf("expected the repaired toolchain to be ready: %+v", status)
	}
	byID = componentsByID(t, status)
	component = byID["rembg"]
	if !component.Ready || component.Path != filepath.Join(venvDir, "bin", "python") {
		t.Fatalf("expected a ready rembg component pointing at the venv interpreter: %+v", component)
	}
	for _, path := range []string{
		filepath.Join(venvDir, "bin", "python"),
		filepath.Join(venvDir, "lib", "python3.11", "site-packages", "rembg", "__init__.py"),
		filepath.Join(root, "rembg", ".ready"),
	} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			t.Fatalf("expected the restored background removal file %s: %v", path, err)
		}
	}
	if !directoryExists(filepath.Join(venvDir, "lib", "python3.11", "site-packages", "onnxruntime-1.32.0.dist-info")) {
		t.Fatal("expected the restored onnxruntime dist-info directory")
	}

	repairedInfo, err := os.Stat(modelPath)
	if err != nil {
		t.Fatalf("stat the model after the forced repair: %v", err)
	}
	if !repairedInfo.ModTime().Equal(oldTime) {
		t.Fatalf("a forced repair must keep the installed model, mtime changed from %v to %v", oldTime, repairedInfo.ModTime())
	}
	if calls := downloader.callsFor(modelURL); calls != 1 {
		t.Fatalf("a forced repair re-downloaded the model: %d calls, want 1", calls)
	}

	// A checksum mismatch must fail the install without deleting the old model.
	oldPayload, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read the installed model: %v", err)
	}
	injectedSpec := rembgModelSpec
	rembgModelSpec.SHA256 = strings.Repeat("0", 64)
	t.Cleanup(func() {
		rembgModelSpec = injectedSpec
	})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	collected := waitForTerminalEvent(t, events)
	terminal := collected[len(collected)-1]
	if terminal.Error == "" || !terminal.Done {
		t.Fatalf("expected a terminal checksum error, got %+v", terminal)
	}
	if !strings.Contains(terminal.Error, "checksum") {
		t.Fatalf("expected the checksum failure to be reported, got %q", terminal.Error)
	}
	if calls := downloader.callsFor(modelURL); calls != 2 {
		t.Fatalf("expected the failed repair to download the model once more, got %d calls", calls)
	}
	afterPayload, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read the model after the failed repair: %v", err)
	}
	if !bytes.Equal(afterPayload, oldPayload) {
		t.Fatal("a failed checksum must keep the previous model file")
	}
	if !componentsByID(t, service.Status())["rembg-model"].Ready {
		t.Fatal("the previous model must stay ready after a failed download")
	}
}

func TestRembgVenvSkipAndForceContract(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	runner := &fakeRunner{}
	service, events := newTestToolchainService(t, dataDir, downloader, runner)

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)

	root := filepath.Join(dataDir, ToolchainDirName)
	venvDir := filepath.Join(root, "rembg-venv")
	venvPython := filepath.Join(venvDir, "bin", "python")
	basePython := filepath.Join(root, "env", "bin", "python")
	marker := filepath.Join(root, "rembg", ".ready")
	modelURL, err := RembgModelURL(DefaultRembgModel)
	if err != nil {
		t.Fatalf("background removal model URL: %v", err)
	}

	// The venv is created exactly like the pdf2docx venv: the managed
	// interpreter with `-m venv <rembg-venv>` as its only arguments.
	creationCalls := runnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir)
	if len(creationCalls) != 1 {
		t.Fatalf("expected one background removal venv creation, got %d", len(creationCalls))
	}
	if creationCalls[0].name != basePython {
		t.Fatalf("venv created with %q, want the managed interpreter %q", creationCalls[0].name, basePython)
	}
	if !reflect.DeepEqual(creationCalls[0].args, []string{"-m", "venv", venvDir}) {
		t.Fatalf("unexpected background removal venv argv: %v", creationCalls[0].args)
	}

	// A present venv with its marker is reused without touching pip or venv.
	creationsBefore := countRunnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir)
	venvCallsBefore := countRunnerCallsForBinary(runner.recordedCalls(), venvPython)
	modelCallsBefore := downloader.callsFor(modelURL)
	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	if got := countRunnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir); got != creationsBefore {
		t.Fatalf("a skip install recreated the venv: %d creations, want %d", got, creationsBefore)
	}
	if got := countRunnerCallsForBinary(runner.recordedCalls(), venvPython); got != venvCallsBefore {
		t.Fatalf("a skip install ran pip or the import check: %d calls, want %d", got, venvCallsBefore)
	}
	if got := downloader.callsFor(modelURL); got != modelCallsBefore {
		t.Fatalf("a skip install downloaded the model again: %d calls, want %d", got, modelCallsBefore)
	}

	// Losing only the marker must force a rebuild: the venv alone is not proof
	// that the engine imported.
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove the readiness marker: %v", err)
	}
	creationsBefore = countRunnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir)
	venvCallsBefore = countRunnerCallsForBinary(runner.recordedCalls(), venvPython)
	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)
	if got := countRunnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir); got != creationsBefore+1 {
		t.Fatalf("a missing marker must rebuild the venv: %d creations, want %d", got, creationsBefore+1)
	}
	// A rebuild runs pip and the import check on the venv interpreter.
	if got := countRunnerCallsForBinary(runner.recordedCalls(), venvPython); got != venvCallsBefore+2 {
		t.Fatalf("a missing marker must reinstall the packages: %d calls, want %d", got, venvCallsBefore+2)
	}
	if !fileWithContent(marker) {
		t.Fatal("expected the readiness marker to be rewritten")
	}

	// Force rebuilds a present venv and rewrites the marker.
	oldTime := time.Unix(1_600_000_000, 0)
	if err := os.Chtimes(marker, oldTime, oldTime); err != nil {
		t.Fatalf("pin the readiness marker mtime: %v", err)
	}
	creationsBefore = countRunnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir)
	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	waitForTerminalEvent(t, events)
	if got := countRunnerCallsForRembgVenvCreation(runner.recordedCalls(), venvDir); got != creationsBefore+1 {
		t.Fatalf("a forced install must rebuild the venv: %d creations, want %d", got, creationsBefore+1)
	}
	markerInfo, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("stat the rewritten marker: %v", err)
	}
	if markerInfo.ModTime().Equal(oldTime) {
		t.Fatal("a forced install must rewrite the readiness marker")
	}

	// The stale marker is removed before pip: a failed rebuild must not leave
	// the previous success behind.
	runner.failRembgPip = true
	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	collected := waitForTerminalEvent(t, events)
	terminal := collected[len(collected)-1]
	if terminal.Error == "" {
		t.Fatalf("expected the pip failure to be reported, got %+v", terminal)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a failed rebuild must not leave the readiness marker, got %v", err)
	}
	if componentsByID(t, service.Status())["rembg"].Ready {
		t.Fatal("a failed rebuild must not report the background removal engine as ready")
	}
}

func TestRembgModelFailureKeepsPreviousModelAndCleansTemp(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptionsV1{})
	waitForTerminalEvent(t, events)

	root := filepath.Join(dataDir, ToolchainDirName)
	modelPath := rembgModelPath(root, DefaultRembgModel)
	modelDir := filepath.Dir(modelPath)
	modelURL, err := RembgModelURL(DefaultRembgModel)
	if err != nil {
		t.Fatalf("background removal model URL: %v", err)
	}
	installedPayload, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read the installed model: %v", err)
	}

	// A checksum that does not match the installed file skips the reuse fast
	// path, so a forced repair actually attempts the download.
	injectedSpec := rembgModelSpec
	rembgModelSpec.SHA256 = strings.Repeat("0", 64)
	t.Cleanup(func() {
		rembgModelSpec = injectedSpec
	})

	assertModelUntouchedNoTemp := func(stage string) {
		t.Helper()

		payload, err := os.ReadFile(modelPath)
		if err != nil {
			t.Fatalf("%s: read the model: %v", stage, err)
		}
		if !bytes.Equal(payload, installedPayload) {
			t.Fatalf("%s: the previous model was modified", stage)
		}
		matches, err := filepath.Glob(filepath.Join(modelDir, ".fileforge-rembg-model-*"))
		if err != nil {
			t.Fatalf("%s: glob the model temp files: %v", stage, err)
		}
		if len(matches) != 0 {
			t.Fatalf("%s: expected no model temp files, got %v", stage, matches)
		}
	}

	// A failed download keeps the previous model and cleans its temp file.
	downloader.failURL = modelURL
	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	collected := waitForTerminalEvent(t, events)
	if terminal := collected[len(collected)-1]; terminal.Error == "" {
		t.Fatalf("expected the download failure to be reported, got %+v", terminal)
	}
	assertModelUntouchedNoTemp("failed download")

	// A failed checksum also keeps the previous model and cleans its temp file.
	downloader.failURL = ""
	service.Start(context.Background(), models.ToolchainInstallOptionsV1{Force: true})
	collected = waitForTerminalEvent(t, events)
	terminal := collected[len(collected)-1]
	if terminal.Error == "" || !strings.Contains(terminal.Error, "checksum") {
		t.Fatalf("expected a checksum failure, got %+v", terminal)
	}
	assertModelUntouchedNoTemp("failed checksum")
}

func runnerCallsForBinary(calls []fakeRunnerCall, binary string) []fakeRunnerCall {
	filtered := make([]fakeRunnerCall, 0, len(calls))
	for _, call := range calls {
		if call.name == binary {
			filtered = append(filtered, call)
		}
	}

	return filtered
}

func countRunnerCallsForBinary(calls []fakeRunnerCall, binary string) int {
	return len(runnerCallsForBinary(calls, binary))
}

// runnerCallsForRembgVenvCreation returns the `python -m venv <rembg-venv>`
// calls, so tests can assert the exact argv and how often the venv was rebuilt.
func runnerCallsForRembgVenvCreation(calls []fakeRunnerCall, venvDir string) []fakeRunnerCall {
	filtered := make([]fakeRunnerCall, 0, len(calls))
	for _, call := range calls {
		if len(call.args) == 3 && call.args[0] == "-m" && call.args[1] == "venv" && call.args[2] == venvDir {
			filtered = append(filtered, call)
		}
	}

	return filtered
}

func countRunnerCallsForRembgVenvCreation(calls []fakeRunnerCall, venvDir string) int {
	return len(runnerCallsForRembgVenvCreation(calls, venvDir))
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
// downloads the Whisper model, the background removal venv (~450-550 MB) and
// its model (171 MB), so the run is large and slow.
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
