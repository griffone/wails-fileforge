package toolchain

import (
	"bytes"
	"context"
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

func TestUpscalePackRequiresAnExplicitInstall(t *testing.T) {
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
	if terminal.Error == "" || !terminal.Done {
		t.Fatalf("expected a terminal checksum error, got %+v", terminal)
	}
	if !strings.Contains(terminal.Error, "checksum") {
		t.Fatalf("expected the checksum failure to be reported, got %q", terminal.Error)
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
}
