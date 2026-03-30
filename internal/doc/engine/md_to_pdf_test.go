package engine

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
	"time"

	"github.com/jung-kurt/gofpdf"
)

func TestBuildRenderPlanAppliesAllMarginsDeterministically(t *testing.T) {
	cfg := RenderConfig{
		Header: HeaderFooterConfig{
			Enabled:      true,
			Text:         "header",
			Align:        "center",
			Font:         "times",
			MarginTop:    12,
			MarginBottom: 8,
			Color:        "#112233",
		},
		Footer: HeaderFooterConfig{
			Enabled:      true,
			Text:         "footer",
			Align:        "right",
			Font:         "courier",
			MarginTop:    9,
			MarginBottom: 7,
			Color:        "#AABBCC",
		},
	}

	plan := buildRenderPlan(cfg)

	if plan.TopMargin != 26 {
		t.Fatalf("expected top margin 26, got %v", plan.TopMargin)
	}

	if plan.BottomMargin != 22 {
		t.Fatalf("expected bottom margin 22, got %v", plan.BottomMargin)
	}

	if plan.Header.Y != 12 {
		t.Fatalf("expected header y 12, got %v", plan.Header.Y)
	}

	if plan.Footer.Y != -13 {
		t.Fatalf("expected footer y -13, got %v", plan.Footer.Y)
	}

	if plan.Header.Align != "C" {
		t.Fatalf("expected header align C, got %s", plan.Header.Align)
	}

	if plan.Footer.Align != "R" {
		t.Fatalf("expected footer align R, got %s", plan.Footer.Align)
	}

	if plan.Header.Font != "Times" {
		t.Fatalf("expected header font Times, got %s", plan.Header.Font)
	}

	if plan.Footer.Font != "Courier" {
		t.Fatalf("expected footer font Courier, got %s", plan.Footer.Font)
	}

	if plan.Header.R != 17 || plan.Header.G != 34 || plan.Header.B != 51 {
		t.Fatalf("unexpected header color rgb: %d,%d,%d", plan.Header.R, plan.Header.G, plan.Header.B)
	}

	if plan.Footer.R != 170 || plan.Footer.G != 187 || plan.Footer.B != 204 {
		t.Fatalf("unexpected footer color rgb: %d,%d,%d", plan.Footer.R, plan.Footer.G, plan.Footer.B)
	}
}

func TestBuildRenderPlanKeepsBaseBodyMarginsWhenHeaderFooterDisabled(t *testing.T) {
	plan := buildRenderPlan(RenderConfig{})

	if plan.TopMargin != baseTopMarginMM {
		t.Fatalf("expected top margin %v, got %v", baseTopMarginMM, plan.TopMargin)
	}

	if plan.BottomMargin != baseBottomMarginMM {
		t.Fatalf("expected bottom margin %v, got %v", baseBottomMarginMM, plan.BottomMargin)
	}
}

func TestRenderMarkdownToPDFSupportsRemoteHTTPImageAndEmbedsIt(t *testing.T) {
	tmpDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/img.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(makePNGFixtureBytes(t))
	}))
	defer server.Close()

	inputPath := filepath.Join(tmpDir, "remote.md")
	outputPath := filepath.Join(tmpDir, "remote.pdf")
	markdown := "# Remote image\n\n![](" + server.URL + "/img.png)"
	if err := os.WriteFile(inputPath, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}

	err := RenderMarkdownToPDF(context.Background(), RenderConfig{InputPath: inputPath, OutputPath: outputPath})
	if err != nil {
		t.Fatalf("expected successful render with remote image, got: %v", err)
	}

	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read generated pdf: %v", readErr)
	}

	if len(content) < 5 || string(content[:5]) != "%PDF-" {
		t.Fatalf("generated file is not a valid PDF")
	}

	if !strings.Contains(string(content), "/Subtype /Image") {
		t.Fatalf("expected embedded image marker in generated pdf")
	}

	if strings.Contains(string(content), server.URL) {
		t.Fatalf("expected generated pdf not to reference remote URL directly")
	}
}

func TestDrawImageReturnsErrorForRemoteURLWhenUnreachable(t *testing.T) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", defaultBodyFontSize)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := drawImage(ctx, pdf, "http://127.0.0.1:1/not-found.png", t.TempDir())
	if err == nil {
		t.Fatalf("expected error for unreachable remote image URL")
	}

	if !strings.Contains(err.Error(), "remote image download failed") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRenderMarkdownToPDFKeepsLocalRelativeImageSupport(t *testing.T) {
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets dir: %v", err)
	}

	imgPath := filepath.Join(assetsDir, "local.png")
	if err := os.WriteFile(imgPath, makePNGFixtureBytes(t), 0o644); err != nil {
		t.Fatalf("write local image fixture: %v", err)
	}

	inputPath := filepath.Join(tmpDir, "local.md")
	outputPath := filepath.Join(tmpDir, "local.pdf")
	markdown := "# Local image\n\n![local](assets/local.png)"
	if err := os.WriteFile(inputPath, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}

	err := RenderMarkdownToPDF(context.Background(), RenderConfig{InputPath: inputPath, OutputPath: outputPath})
	if err != nil {
		t.Fatalf("expected successful render with local relative image, got: %v", err)
	}

	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read generated pdf: %v", readErr)
	}

	if len(content) < 5 || string(content[:5]) != "%PDF-" {
		t.Fatalf("generated file is not a valid PDF")
	}
}

func makePNGFixtureBytes(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 12, G: 140, B: 220, A: 255})
		}
	}

	buf := bytes.NewBuffer(nil)
	if err := png.Encode(buf, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}

	return buf.Bytes()
}
