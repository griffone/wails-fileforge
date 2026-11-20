package verify

import (
	"archive/zip"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"

	"github.com/h2non/bimg"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func writeVerifyPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

func writeVerifyWebP(t *testing.T, path string, width, height int) {
	t.Helper()

	pngPath := filepath.Join(t.TempDir(), "source.png")
	writeVerifyPNG(t, pngPath, width, height)

	content, err := os.ReadFile(pngPath)
	if err != nil {
		t.Fatalf("read webp source png: %v", err)
	}

	converted, err := bimg.NewImage(content).Convert(bimg.WEBP)
	if err != nil {
		if !bimg.IsTypeSupportedSave(bimg.WEBP) {
			t.Skipf("this libvips build cannot save WebP: %v", err)
		}
		t.Fatalf("convert webp fixture: %v", err)
	}
	if err := os.WriteFile(path, converted, 0o644); err != nil {
		t.Fatalf("write webp fixture: %v", err)
	}
}

func writeVerifyZip(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("readme.txt")
	if err != nil {
		_ = file.Close()
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("bundle")); err != nil {
		_ = file.Close()
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		t.Fatalf("close zip writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func writeVerifyPDF(t *testing.T, path string, pages int) {
	t.Helper()

	pngPaths := make([]string, 0, pages)
	for page := 0; page < pages; page++ {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("page-%d.png", page+1))
		writeVerifyPNG(t, pngPath, 24, 24)
		pngPaths = append(pngPaths, pngPath)
	}
	if err := api.ImportImagesFile(context.Background(), pngPaths, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

// writeVerifyVideo generates a tiny H.264/AAC clip with ffmpeg so the video
// metadata checks can be asserted against real media. Tests skip when the
// ffmpeg toolchain is missing instead of failing.
func writeVerifyVideo(t *testing.T, path string) {
	t.Helper()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not available")
	}

	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "testsrc=size=160x120:rate=10",
		"-f", "lavfi",
		"-i", "sine=frequency=440:sample_rate=48000",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-preset", "ultrafast",
		"-c:a", "aac",
		"-t", "1",
		"-shortest",
		path,
	}
	if output, runErr := exec.Command(ffmpegPath, args...).CombinedOutput(); runErr != nil {
		t.Fatalf("generate video fixture: %v (%s)", runErr, strings.TrimSpace(string(output)))
	}
}

func checksByKey(output models.VerifiedOutput) map[string]bool {
	checks := make(map[string]bool, len(output.Checks))
	for _, check := range output.Checks {
		checks[check.Key] = check.Passed
	}

	return checks
}

func TestVerifyPathsValidImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writeVerifyPNG(t, path, 8, 6)

	report := VerifyPaths([]string{path})
	if !report.Success {
		t.Fatalf("expected a valid image to verify, got %s (%v)", report.Message, report.Warnings)
	}
	if len(report.Outputs) != 1 {
		t.Fatalf("expected one output, got %d", len(report.Outputs))
	}

	output := report.Outputs[0]
	if !output.Exists || output.SizeBytes <= 0 || output.Kind != "image" {
		t.Fatalf("expected an existing image output, got %+v", output)
	}

	checks := checksByKey(output)
	for _, key := range []string{checkPath, checkExists, checkRegularFile, checkReadable, checkSize, checkKind, checkImageDims} {
		passed, ok := checks[key]
		if !ok {
			t.Fatalf("expected check %q, got %v", key, checks)
		}
		if !passed {
			t.Fatalf("expected check %q to pass, got %v", key, checks)
		}
	}
}

func TestVerifyPathsWebPArchiveAndHTML(t *testing.T) {
	dir := t.TempDir()
	webpPath := filepath.Join(dir, "photo.webp")
	writeVerifyWebP(t, webpPath, 10, 6)
	zipPath := filepath.Join(dir, "bundle.zip")
	writeVerifyZip(t, zipPath)
	htmlPath := filepath.Join(dir, "srcset.html")
	if err := os.WriteFile(htmlPath, []byte("<!doctype html><p>snippet</p>"), 0o644); err != nil {
		t.Fatalf("write html fixture: %v", err)
	}

	report := VerifyPaths([]string{webpPath, zipPath, htmlPath})
	if !report.Success {
		t.Fatalf("expected the bundle to verify, got %s (%v)", report.Message, report.Warnings)
	}
	if len(report.Outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(report.Outputs))
	}

	webpOutput := report.Outputs[0]
	if webpOutput.Kind != "image" {
		t.Fatalf("expected image kind for the WebP, got %q", webpOutput.Kind)
	}
	webpChecks := checksByKey(webpOutput)
	if passed, ok := webpChecks[checkImageDims]; !ok || !passed {
		t.Fatalf("expected the WebP dimensions check to pass, got %v", webpChecks)
	}
	for _, check := range webpOutput.Checks {
		if check.Key == checkImageDims && check.Value != "10 × 6 px" {
			t.Fatalf("expected 10 × 6 px, got %q", check.Value)
		}
	}

	zipOutput := report.Outputs[1]
	if zipOutput.Kind != "archive" {
		t.Fatalf("expected archive kind for the ZIP, got %q", zipOutput.Kind)
	}
	zipChecks := checksByKey(zipOutput)
	if passed, ok := zipChecks[checkKind]; !ok || !passed {
		t.Fatalf("expected the ZIP kind check to pass, got %v", zipChecks)
	}

	htmlOutput := report.Outputs[2]
	if htmlOutput.Kind != "document" {
		t.Fatalf("expected document kind for the HTML, got %q", htmlOutput.Kind)
	}
	htmlChecks := checksByKey(htmlOutput)
	if passed, ok := htmlChecks[checkKind]; !ok || !passed {
		t.Fatalf("expected the HTML kind check to pass, got %v", htmlChecks)
	}
}

func TestVerifyPathsValidPDFReportsPageCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "document.pdf")
	writeVerifyPDF(t, path, 3)

	report := VerifyPaths([]string{path})
	if !report.Success {
		t.Fatalf("expected a valid PDF to verify, got %s (%v)", report.Message, report.Warnings)
	}

	output := report.Outputs[0]
	if output.Kind != "pdf" {
		t.Fatalf("expected pdf kind, got %q", output.Kind)
	}

	found := false
	for _, check := range output.Checks {
		if check.Key == checkPageCount {
			found = true
			if !check.Passed || check.Value != "3 pages" {
				t.Fatalf("expected 3 pages, got %+v", check)
			}
		}
	}
	if !found {
		t.Fatalf("expected a page_count check, got %+v", output.Checks)
	}
}

func TestVerifyPathsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.pdf")

	report := VerifyPaths([]string{path})
	if report.Success {
		t.Fatal("expected a missing file to fail verification")
	}
	if len(report.Outputs) != 1 || report.Outputs[0].Exists {
		t.Fatalf("expected one missing output, got %+v", report.Outputs)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "file not found") {
		t.Fatalf("expected a per-output warning, got %v", report.Warnings)
	}

	checks := checksByKey(report.Outputs[0])
	if passed, ok := checks[checkExists]; !ok || passed {
		t.Fatalf("expected a failed exists check, got %v", checks)
	}
}

func TestVerifyPathsKeepsEveryOutputWhenOneFails(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	writeVerifyPNG(t, valid, 4, 4)
	missing := filepath.Join(dir, "missing.png")

	report := VerifyPaths([]string{valid, missing})
	if report.Success {
		t.Fatal("expected the report to fail when one output is missing")
	}
	if len(report.Outputs) != 2 {
		t.Fatalf("expected both outputs, got %d", len(report.Outputs))
	}
	if !report.Outputs[0].Exists || report.Outputs[1].Exists {
		t.Fatalf("expected the first output to exist and the second to be missing: %+v", report.Outputs)
	}
	if !strings.Contains(report.Message, "checks failed") {
		t.Fatalf("expected a failure summary, got %q", report.Message)
	}
}

func TestVerifyPathsEmptyList(t *testing.T) {
	report := VerifyPaths(nil)
	if report.Success {
		t.Fatal("expected an empty list to fail verification")
	}
	if report.Message != "no outputs to verify" {
		t.Fatalf("expected an explicit empty message, got %q", report.Message)
	}
	if len(report.Outputs) != 0 {
		t.Fatalf("expected no outputs, got %d", len(report.Outputs))
	}
}

func TestVerifyPathsWithTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writeVerifyPNG(t, path, 8, 6)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	tooSmall := VerifyPathsWithTarget([]string{path}, 1)
	if tooSmall.Success {
		t.Fatal("expected a 1-byte target to fail verification")
	}
	found := false
	for _, check := range tooSmall.Outputs[0].Checks {
		if check.Key == checkTargetSize {
			found = true
			if check.Passed {
				t.Fatalf("expected target_size to fail, got %+v", check)
			}
		}
	}
	if !found {
		t.Fatalf("expected a target_size check, got %+v", tooSmall.Outputs[0].Checks)
	}

	generous := VerifyPathsWithTarget([]string{path}, info.Size()+1)
	if !generous.Success {
		t.Fatalf("expected a generous target to pass, got %s (%v)", generous.Message, generous.Warnings)
	}
}

func TestVerifyPathsEmptyFileFailsSizeCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	report := VerifyPaths([]string{path})
	if report.Success {
		t.Fatal("expected an empty file to fail verification")
	}
	checks := checksByKey(report.Outputs[0])
	if passed, ok := checks[checkSize]; !ok || passed {
		t.Fatalf("expected a failed size check, got %v", checks)
	}
}

func TestVerifyPathsVideoDurationIsTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write video fixture: %v", err)
	}

	report := VerifyPaths([]string{path})
	if len(report.Outputs) != 1 {
		t.Fatalf("expected one output, got %d", len(report.Outputs))
	}
	if report.Outputs[0].Kind != "video" {
		t.Fatalf("expected video kind, got %q", report.Outputs[0].Kind)
	}

	for _, check := range report.Outputs[0].Checks {
		if check.Key == checkVideoLength && !check.Passed {
			t.Fatalf("expected a missing duration to stay tolerant, got %+v", check)
		}
	}

	hasDuration := false
	for _, check := range report.Outputs[0].Checks {
		if check.Key == checkVideoLength {
			hasDuration = true
		}
	}
	if !hasDuration {
		foundWarning := false
		for _, warning := range report.Outputs[0].Warnings {
			if strings.Contains(warning, "duration could not be determined") {
				foundWarning = true
			}
		}
		if !foundWarning {
			t.Fatalf("expected a duration warning when ffprobe cannot read the file, got %v", report.Outputs[0].Warnings)
		}
	}
}

func TestVerifyPathsRealVideoReportsDimensionsAndCodecs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	writeVerifyVideo(t, path)

	report := VerifyPaths([]string{path})
	if !report.Success {
		t.Fatalf("expected a real video to verify, got %s (%v)", report.Message, report.Warnings)
	}

	output := report.Outputs[0]
	checks := checksByKey(output)
	for _, key := range []string{checkVideoLength, checkVideoDims, checkVideoCodec, checkAudioCodec} {
		passed, ok := checks[key]
		if !ok {
			t.Fatalf("expected check %q, got %v", key, checks)
		}
		if !passed {
			t.Fatalf("expected check %q to pass, got %v", key, checks)
		}
	}

	values := make(map[string]string, len(output.Checks))
	for _, check := range output.Checks {
		values[check.Key] = check.Value
	}
	if values[checkVideoDims] != "160 × 120 px" {
		t.Fatalf("expected 160 × 120 px, got %q", values[checkVideoDims])
	}
	if values[checkVideoCodec] != "h264" {
		t.Fatalf("expected h264, got %q", values[checkVideoCodec])
	}
	if values[checkAudioCodec] != "aac" {
		t.Fatalf("expected aac, got %q", values[checkAudioCodec])
	}
}
