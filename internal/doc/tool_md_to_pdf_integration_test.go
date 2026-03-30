package doc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestMDToPDFToolIntegration_WithAndWithoutHeaderFooter(t *testing.T) {
	tmpDir := t.TempDir()

	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}

	imgPath := filepath.Join(assetsDir, "dot.png")
	writePNGFixture(t, imgPath)

	inputPath := filepath.Join(tmpDir, "doc.md")
	markdown := "# Title\n\nParagraph with <strong>HTML</strong>.\n\n![dot](assets/dot.png)"
	if err := os.WriteFile(inputPath, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}

	tool := NewMDToPDFTool()

	withHF := filepath.Join(tmpDir, "with-hf.pdf")
	itemA, errA := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDDocMDToPDFV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath": withHF,
			"header": map[string]any{
				"enabled":      true,
				"text":         "{fileName} {date}",
				"align":        "left",
				"font":         "helvetica",
				"marginTop":    3,
				"marginBottom": 4,
				"color":        "#112233",
			},
			"footer": map[string]any{
				"enabled":      true,
				"text":         "{page}/{totalPages}",
				"align":        "right",
				"font":         "courier",
				"marginTop":    2,
				"marginBottom": 5,
				"color":        "#000000",
			},
		},
	})

	if errA != nil || !itemA.Success {
		t.Fatalf("expected success with header/footer, item=%+v err=%+v", itemA, errA)
	}
	assertValidPDFFile(t, withHF)

	withoutHF := filepath.Join(tmpDir, "without-hf.pdf")
	itemB, errB := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDDocMDToPDFV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath": withoutHF,
		},
	})

	if errB != nil || !itemB.Success {
		t.Fatalf("expected success without header/footer, item=%+v err=%+v", itemB, errB)
	}
	assertValidPDFFile(t, withoutHF)
}

func TestMDToPDFToolIntegration_RemoteImageHTTP(t *testing.T) {
	tmpDir := t.TempDir()

	imgPayload := pngFixtureBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dot.png" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imgPayload)
	}))
	defer server.Close()

	inputPath := filepath.Join(tmpDir, "remote.md")
	markdown := "# Remote image\n\n![remote](" + server.URL + "/dot.png)"
	if err := os.WriteFile(inputPath, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}

	outputPath := filepath.Join(tmpDir, "remote.pdf")
	tool := NewMDToPDFTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDDocMDToPDFV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath": outputPath,
		},
	})

	if jobErr != nil || !item.Success {
		t.Fatalf("expected success with remote image, item=%+v err=%+v", item, jobErr)
	}

	assertValidPDFFile(t, outputPath)
	assertPDFContainsImageMarker(t, outputPath)
}

func assertValidPDFFile(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output pdf: %v", err)
	}

	if len(content) < 6 {
		t.Fatalf("pdf too small")
	}

	if string(content[:5]) != "%PDF-" {
		t.Fatalf("output is not a pdf file")
	}
}

func writePNGFixture(t *testing.T, path string) {
	t.Helper()

	img := buildPNGFixtureImage()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
}

func pngFixtureBytes(t *testing.T) []byte {
	t.Helper()

	img := buildPNGFixtureImage()
	var buf bytes.Buffer

	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png payload: %v", err)
	}

	return buf.Bytes()
}

func buildPNGFixtureImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 32, G: 180, B: 64, A: 255})
		}
	}

	return img
}

func assertPDFContainsImageMarker(t *testing.T, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output pdf: %v", err)
	}

	if !strings.Contains(string(content), "/Subtype /Image") {
		t.Fatalf("expected embedded image marker in generated pdf")
	}
}
