package toolchain

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestUpscaleModelMetadataIsPinned(t *testing.T) {
	spec := DefaultUpscaleModelSpec()

	if spec.Model != UpscaleModelGeneral {
		t.Fatalf("model = %q, want %q", spec.Model, UpscaleModelGeneral)
	}
	if !strings.HasPrefix(spec.URL, "https://") || !strings.HasSuffix(spec.ArchiveName, ".zip") {
		t.Fatalf("unexpected archive metadata: %+v", spec)
	}
	if spec.SizeBytes <= 0 || len(spec.SHA256) != 64 {
		t.Fatalf("unexpected archive size or checksum: %+v", spec)
	}
	if spec.ArchiveDir == "" || spec.ONNXFileName == "" {
		t.Fatalf("archive layout is not pinned: %+v", spec)
	}
	if len(spec.Files) != 2 {
		t.Fatalf("expected the graph and its external data file, got %+v", spec.Files)
	}
	for _, file := range spec.Files {
		if file.Name == "" || file.SizeBytes <= 0 || len(file.SHA256) != 64 {
			t.Fatalf("unexpected pinned model file: %+v", file)
		}
	}

	artifact := UpscaleModelArtifact()
	if artifact.URL != spec.URL || artifact.SHA256 != spec.SHA256 || artifact.SizeBytes != spec.SizeBytes {
		t.Fatalf("artifact drifted from the spec: %+v", artifact)
	}
}

func TestUpscaleStatusNeverDownloadsOnItsOwn(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, _ := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	// A status read only stats files. It must never start a download.
	status := service.Status()
	if downloader.callCount() != 0 {
		t.Fatalf("status downloaded %d artifacts without consent", downloader.callCount())
	}
	byID := componentsByID(t, status)
	for _, id := range []string{"upscale", "upscale-model"} {
		if byID[id].Ready {
			t.Fatalf("component %q must not be ready before an explicit install", id)
		}
	}
}

func TestUpscaleModelChecksumMismatchKeepsPreviousModel(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptions{})
	waitForTerminalEvent(t, events)

	root := filepath.Join(dataDir, ToolchainDirName)
	onnxPath := upscaleModelONNXPath(root)
	dataPath := filepath.Join(upscaleModelDir(root), upscaleModelDataFileName)
	onnxBefore, err := os.ReadFile(onnxPath)
	if err != nil {
		t.Fatalf("read the installed graph: %v", err)
	}
	dataBefore, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read the installed weights: %v", err)
	}

	original := upscaleModelSpec
	injected := upscaleModelSpec
	injected.Files = append([]UpscaleModelFile{}, original.Files...)
	for index := range injected.Files {
		if injected.Files[index].Name == upscaleModelONNXFileName {
			injected.Files[index].SHA256 = strings.Repeat("0", 64)
		}
	}
	upscaleModelSpec = injected
	t.Cleanup(func() {
		upscaleModelSpec = original
	})

	service.Start(context.Background(), models.ToolchainInstallOptions{Force: true})
	collected := waitForTerminalEvent(t, events)
	terminal := collected[len(collected)-1]
	if terminal.Error != "" || terminal.Phase != PhaseReady {
		t.Fatalf("a model checksum failure must not fail the setup, got %+v", terminal)
	}

	onnxAfter, err := os.ReadFile(onnxPath)
	if err != nil {
		t.Fatalf("read the graph after the failed repair: %v", err)
	}
	dataAfter, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("read the weights after the failed repair: %v", err)
	}
	if !bytes.Equal(onnxBefore, onnxAfter) || !bytes.Equal(dataBefore, dataAfter) {
		t.Fatal("a failed checksum must keep the previous model files")
	}
	if !upscaleModelFilesMatch(root) {
		t.Fatal("the previous model must stay ready after a failed repair")
	}

	status := service.Status()
	if !status.Ready {
		t.Fatalf("the rest of the toolchain must stay ready: %+v", status)
	}
	component := componentsByID(t, status)["upscale-model"]
	if !component.Ready || !strings.Contains(component.Error, "checksum") {
		t.Fatalf("expected the model component to keep the ready file and record the checksum failure: %+v", component)
	}
}

func TestUpscaleAnimeModelMetadataIsPinnedAndDistinct(t *testing.T) {
	generalSpec := DefaultUpscaleModelSpec()
	animeSpec := DefaultUpscaleAnimeModelSpec()

	if animeSpec.Model != UpscaleModelAnime {
		t.Fatalf("model = %q, want %q", animeSpec.Model, UpscaleModelAnime)
	}
	if !strings.HasPrefix(animeSpec.URL, "https://huggingface.co/") || !strings.Contains(animeSpec.URL, "/resolve/") {
		t.Fatalf("the illustration model must be commit-pinned, got %q", animeSpec.URL)
	}
	if !strings.HasSuffix(animeSpec.FileName, ".onnx") {
		t.Fatalf("unexpected illustration model file name: %q", animeSpec.FileName)
	}
	if animeSpec.SizeBytes != 17939975 {
		t.Fatalf("illustration model size = %d, want 17939975", animeSpec.SizeBytes)
	}
	if animeSpec.SHA256 != "9a029dae374b2cd4c3eb2dbbed857e22e6d785687c767aa12828e7356b9fb378" {
		t.Fatalf("illustration model checksum = %q", animeSpec.SHA256)
	}

	// The two models keep independent metadata: a future update of one must not
	// change the other download.
	if generalSpec.URL == animeSpec.URL {
		t.Fatal("the photo and illustration models must not share a URL")
	}
	if generalSpec.SHA256 == animeSpec.SHA256 {
		t.Fatal("the photo and illustration models must not share a checksum")
	}
	if generalSpec.SizeBytes == animeSpec.SizeBytes {
		t.Fatal("the photo and illustration models must not share a size")
	}

	artifact := UpscaleAnimeModelArtifact()
	if artifact.URL != animeSpec.URL || artifact.SHA256 != animeSpec.SHA256 || artifact.SizeBytes != animeSpec.SizeBytes || artifact.FileName != animeSpec.FileName {
		t.Fatalf("artifact drifted from the spec: %+v", artifact)
	}

	// The install affordances report the combined size of both models.
	if got, want := UpscaleModelsSizeBytes(), generalSpec.SizeBytes+animeSpec.SizeBytes; got != want {
		t.Fatalf("combined model size = %d, want %d", got, want)
	}
	if got := UpscaleModelsSizeBytes(); got < 22_000_000 || got > 23_000_000 {
		t.Fatalf("combined model size = %d bytes, want about 22.5 MB", got)
	}
}

func TestUpscaleAnimeModelChecksumMismatchKeepsPreviousModel(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptions{})
	waitForTerminalEvent(t, events)

	root := filepath.Join(dataDir, ToolchainDirName)
	animePath := upscaleAnimeModelPath(root)
	generalPath := upscaleModelONNXPath(root)
	animeBefore, err := os.ReadFile(animePath)
	if err != nil {
		t.Fatalf("read the installed illustration model: %v", err)
	}
	generalBefore, err := os.ReadFile(generalPath)
	if err != nil {
		t.Fatalf("read the installed photo model: %v", err)
	}

	original := upscaleAnimeModelSpec
	injected := original
	injected.SHA256 = strings.Repeat("0", 64)
	upscaleAnimeModelSpec = injected
	t.Cleanup(func() {
		upscaleAnimeModelSpec = original
	})

	service.Start(context.Background(), models.ToolchainInstallOptions{Force: true})
	collected := waitForTerminalEvent(t, events)
	if terminal := collected[len(collected)-1]; terminal.Error != "" || terminal.Phase != PhaseReady {
		t.Fatalf("an illustration model checksum failure must not fail the setup, got %+v", terminal)
	}

	animeAfter, err := os.ReadFile(animePath)
	if err != nil {
		t.Fatalf("read the illustration model after the failed repair: %v", err)
	}
	if !bytes.Equal(animeBefore, animeAfter) {
		t.Fatal("a failed checksum must keep the previous illustration model")
	}
	generalAfter, err := os.ReadFile(generalPath)
	if err != nil {
		t.Fatalf("read the photo model after the failed repair: %v", err)
	}
	if !bytes.Equal(generalBefore, generalAfter) {
		t.Fatal("failing the illustration model must not touch the photo model")
	}
	if !upscaleAnimeModelFilesMatch(root) {
		t.Fatal("the previous illustration model must stay ready after a failed repair")
	}

	matches, err := filepath.Glob(filepath.Join(upscaleAnimeModelDir(root), ".fileforge-upscale-anime-model-*"))
	if err != nil {
		t.Fatalf("glob the illustration model temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no illustration model temp files, got %v", matches)
	}

	status := service.Status()
	if !status.Ready {
		t.Fatalf("the rest of the toolchain must stay ready: %+v", status)
	}
	component := componentsByID(t, status)["upscale-anime-model"]
	if !component.Ready || !strings.Contains(component.Error, "checksum") {
		t.Fatalf("expected the illustration component to keep the ready file and record the checksum failure: %+v", component)
	}
}

func TestSharedONNXVenvServesBothEngines(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	service.Start(context.Background(), models.ToolchainInstallOptions{})
	waitForTerminalEvent(t, events)

	root := filepath.Join(dataDir, ToolchainDirName)
	sharedPython := filepath.Join(root, "onnx-venv", "bin", "python")

	for name, resolve := range map[string]func(string) (string, bool){
		"rembg":   ManagedRembgPythonPath,
		"upscale": ManagedUpscalePythonPath,
	} {
		path, found := resolve(dataDir)
		if !found || path != sharedPython {
			t.Fatalf("%s interpreter = %q, %t; want the shared %q", name, path, found, sharedPython)
		}
	}

	// Every upscale pin must be present, with the exact same version, in the
	// set the shared environment installs. That is what makes one venv safe.
	for _, pin := range upscalePins {
		if !containsAll(rembgPins, pin) {
			t.Fatalf("upscale pin %q is missing from the shared ONNX package set", pin)
		}
	}

	// Removing the shared venv must report both engines as missing.
	if err := os.RemoveAll(onnxVenvDir(root)); err != nil {
		t.Fatalf("remove the shared ONNX venv: %v", err)
	}
	if path, found := ManagedRembgPythonPath(dataDir); found {
		t.Fatalf("the background removal interpreter must not resolve without the shared venv: %q", path)
	}
	if path, found := ManagedUpscalePythonPath(dataDir); found {
		t.Fatalf("the upscale interpreter must not resolve without the shared venv: %q", path)
	}
	for name, state := range map[string]func(string) (bool, string){
		"rembg":   ManagedRembgRuntimeState,
		"upscale": ManagedUpscaleRuntimeState,
	} {
		ready, reason := state(dataDir)
		if ready {
			t.Fatalf("%s must report the missing shared ONNX venv", name)
		}
		if !strings.Contains(reason, "ONNX engine") || !strings.Contains(reason, "Install / repair") {
			t.Fatalf("%s reason = %q, want a shared ONNX/repair message", name, reason)
		}
	}

	// The downloaded models survive: only the engine has to be rebuilt.
	if _, found := ManagedUpscaleModelPath(dataDir); !found {
		t.Fatal("the photo model must survive a missing shared venv")
	}
	if _, found := ManagedUpscaleAnimeModelPath(dataDir); !found {
		t.Fatal("the illustration model must survive a missing shared venv")
	}
}

func TestStandardUpscaleComponentsInstallByDefault(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	// A default install, with no options and no opt-in step, must download the
	// shared engine and both models together with the rest of the toolchain.
	service.Start(context.Background(), models.ToolchainInstallOptions{})
	waitForTerminalEvent(t, events)

	status := service.Status()
	if !status.Ready {
		t.Fatalf("expected the default install to be ready: %+v", status)
	}
	byID := componentsByID(t, status)
	for _, id := range []string{"upscale", "upscale-model", "upscale-anime-model"} {
		component := byID[id]
		if !component.Standard {
			t.Fatalf("component %q must be part of the standard install set: %+v", id, component)
		}
		if !component.Ready {
			t.Fatalf("component %q must be installed by the default run: %+v", id, component)
		}
		if component.Required {
			t.Fatalf("component %q must not gate readiness: %+v", id, component)
		}
	}

	for name, url := range map[string]string{
		"photo model":        upscaleModelSpec.URL,
		"illustration model": upscaleAnimeModelSpec.URL,
	} {
		if calls := downloader.callsFor(url); calls != 1 {
			t.Fatalf("the default install must download the %s once, got %d calls", name, calls)
		}
	}
}

func TestRepairPicksUpUpscaleOnLegacyInstall(t *testing.T) {
	preserveToolchainEnvironment(t)

	dataDir := t.TempDir()
	root := filepath.Join(dataDir, ToolchainDirName)

	// Simulate the previous layout: a rembg-only install with its own venv and
	// marker, and no upscale engine or models.
	legacyVenv := filepath.Join(root, "rembg-venv")
	sitePackages := filepath.Join(legacyVenv, "lib", "python3.11", "site-packages")
	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		t.Fatalf("create the legacy site-packages: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(legacyVenv, "bin"), 0o755); err != nil {
		t.Fatalf("create the legacy bin folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyVenv, "bin", "python"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write the legacy interpreter: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sitePackages, "rembg"), 0o755); err != nil {
		t.Fatalf("create the legacy rembg package folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sitePackages, "rembg", "__init__.py"), nil, 0o644); err != nil {
		t.Fatalf("write the legacy rembg package: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "rembg"), 0o755); err != nil {
		t.Fatalf("create the legacy rembg home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "rembg", ".ready"), []byte("ready"), 0o644); err != nil {
		t.Fatalf("write the legacy marker: %v", err)
	}
	for _, receipt := range []string{"onnxruntime-1.32.0.dist-info", "numpy-2.3.5.dist-info", "pillow-12.3.0.dist-info"} {
		if err := os.MkdirAll(filepath.Join(sitePackages, receipt), 0o755); err != nil {
			t.Fatalf("create the legacy %s: %v", receipt, err)
		}
	}

	downloader := &fakeDownloader{payloads: linuxToolchainPayloads(t)}
	service, events := newTestToolchainService(t, dataDir, downloader, &fakeRunner{})

	// The legacy install does not report the new components, and the upscale
	// pieces are part of the normal repair path.
	status := service.Status()
	byID := componentsByID(t, status)
	for _, id := range []string{"upscale", "upscale-model", "upscale-anime-model"} {
		if byID[id].Ready {
			t.Fatalf("component %q must not be ready on the legacy layout: %+v", id, byID[id])
		}
	}

	service.Start(context.Background(), models.ToolchainInstallOptions{})
	waitForTerminalEvent(t, events)

	status = service.Status()
	byID = componentsByID(t, status)
	for _, id := range []string{"rembg", "upscale", "upscale-model", "upscale-anime-model"} {
		if !byID[id].Ready {
			t.Fatalf("expected %q to be ready after the repair: %+v", id, byID[id])
		}
	}
	if _, err := os.Stat(legacyVenv); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the repair must replace the legacy rembg venv, got %v", err)
	}
	if _, found := ManagedRembgPythonPath(dataDir); !found {
		t.Fatal("background removal must keep working after the repair")
	}
	if _, found := ManagedUpscaleModelPath(dataDir); !found {
		t.Fatal("the repair must install the photo model")
	}
	if _, found := ManagedUpscaleAnimeModelPath(dataDir); !found {
		t.Fatal("the repair must install the illustration model")
	}
}
