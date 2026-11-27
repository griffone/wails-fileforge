package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	// Importing the domain packages runs their init functions, which register
	// every tool in the global registry.
	"fileforge-desktop/internal/audio"
	"fileforge-desktop/internal/doc"
	"fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/video"
)

// updateGolden refreshes the golden files instead of comparing them.
var updateGolden = flag.Bool("update", false, "update golden files")

// toolIDs is the characterized catalog. It is explicit so the golden test can
// read manifests through GetTool without evaluating runtime state.
var toolIDs = []string{
	image.ToolIDImageConvert,
	image.ToolIDImageCrop,
	image.ToolIDImageAnnotate,
	image.ToolIDImageCompress,
	image.ToolIDImageWatermark,
	image.ToolIDImageOCR,
	image.ToolIDImageBackground,
	image.ToolIDImageUpscale,
	pdf.ToolIDPDFMerge,
	pdf.ToolIDPDFSplit,
	pdf.ToolIDPDFCrop,
	pdf.ToolIDPDFOptimize,
	pdf.ToolIDPDFOCR,
	pdf.ToolIDPDFImages,
	pdf.ToolIDPDFToDOCX,
	pdf.ToolIDPDFRotate,
	pdf.ToolIDPDFRemovePages,
	pdf.ToolIDPDFReorder,
	pdf.ToolIDPDFExtractImages,
	video.ToolIDVideoConvert,
	video.ToolIDVideoTrim,
	video.ToolIDVideoMerge,
	video.ToolIDVideoTranscribe,
	audio.ToolIDAudioExtract,
	audio.ToolIDAudioConvert,
	audio.ToolIDAudioNormalize,
	doc.ToolIDDocMDToPDF,
	doc.ToolIDDocDOCXToPDF,
}

func TestToolRegistryContract(t *testing.T) {
	reg := registry.GetGlobalRegistry()

	if errs := reg.GetInitializationErrors(); len(errs) > 0 {
		t.Fatalf("registry reported initialization errors: %v", errs)
	}

	catalog := reg.ListTools(context.Background())
	if len(catalog) != 28 {
		t.Fatalf("registered tools = %d, want 28", len(catalog))
	}

	seen := make(map[string]struct{}, len(catalog))
	for _, entry := range catalog {
		manifest := entry.Manifest

		if manifest.ToolID == "" {
			t.Fatalf("tool with empty ID in catalog: %+v", entry)
		}
		if _, exists := seen[manifest.ToolID]; exists {
			t.Fatalf("duplicate tool ID %q in catalog", manifest.ToolID)
		}
		seen[manifest.ToolID] = struct{}{}

		if manifest.Name == "" {
			t.Errorf("tool %q has an empty Name", manifest.ToolID)
		}
		if manifest.Domain == "" {
			t.Errorf("tool %q has an empty Domain", manifest.ToolID)
		}
		if manifest.Capability == "" {
			t.Errorf("tool %q has an empty Capability", manifest.ToolID)
		}
		if manifest.Version == "" {
			t.Errorf("tool %q has an empty Version", manifest.ToolID)
		}
		if len(manifest.InputExtensions) == 0 {
			t.Errorf("tool %q declares no input extensions", manifest.ToolID)
		}
	}
}

func TestToolManifestsGolden(t *testing.T) {
	manifests := collectManifests(t)

	payload, err := json.MarshalIndent(manifests, "", "  ")
	if err != nil {
		t.Fatalf("marshal tool manifests: %v", err)
	}
	payload = append(payload, '\n')

	goldenPath := filepath.Join("testdata", "tool_manifests.golden.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(goldenPath, payload, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file: %v (run `go test ./internal/tools -update` to create it)", err)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("tool manifests changed; run `go test ./internal/tools -update` to refresh %s", goldenPath)
	}
}

// collectManifests reads every manifest through GetTool so the golden file
// never depends on RuntimeState or on the tools installed on the machine.
func collectManifests(t *testing.T) []models.ToolManifest {
	t.Helper()

	reg := registry.GetGlobalRegistry()
	manifests := make([]models.ToolManifest, 0, len(toolIDs))
	for _, toolID := range toolIDs {
		tool, err := reg.GetTool(toolID)
		if err != nil {
			t.Fatalf("get tool %q: %v", toolID, err)
		}

		manifest := tool.Manifest()
		if manifest.ToolID != toolID {
			t.Fatalf("tool %q reported manifest ID %q", toolID, manifest.ToolID)
		}
		manifests = append(manifests, manifest)
	}

	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].ToolID < manifests[j].ToolID
	})

	return manifests
}
