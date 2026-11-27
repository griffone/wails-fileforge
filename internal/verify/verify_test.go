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

// writeVerifyAudio generates a short WAV fixture with ffmpeg. Tests skip when
// the ffmpeg toolchain is missing instead of failing.
func writeVerifyAudio(t *testing.T, path string) {
	t.Helper()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not available")
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", "sine=frequency=440:sample_rate=48000:duration=2",
		"-c:a", "pcm_s16le",
		path,
	}
	if output, runErr := exec.Command(ffmpegPath, args...).CombinedOutput(); runErr != nil {
		t.Fatalf("generate audio fixture: %v (%s)", runErr, strings.TrimSpace(string(output)))
	}
}

// writeVerifyMP3 generates an MP3 fixture used by the truncation test.
func writeVerifyMP3(t *testing.T, path string) {
	t.Helper()

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not available")
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", "sine=frequency=440:sample_rate=48000:duration=2",
		"-c:a", "libmp3lame", "-b:a", "192k",
		path,
	}
	if output, runErr := exec.Command(ffmpegPath, args...).CombinedOutput(); runErr != nil {
		t.Fatalf("generate mp3 fixture: %v (%s)", runErr, strings.TrimSpace(string(output)))
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
	if output.TargetBytes != 0 || output.ActualBytes != 0 {
		t.Fatalf("expected no target bytes without a target, got %+v", output)
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
	if !tooSmall.Success {
		t.Fatalf("expected a missed target to stay verified, got %+v", tooSmall)
	}
	if tooSmall.Message != "verified 1 output with 1 advisory warning" {
		t.Fatalf("expected an honest advisory message, got %q", tooSmall.Message)
	}
	if len(tooSmall.Warnings) == 0 || !strings.Contains(tooSmall.Warnings[0], "exceeds the 1 B target") {
		t.Fatalf("expected an actionable target warning, got %v", tooSmall.Warnings)
	}
	if tooSmall.Outputs[0].TargetBytes != 1 || tooSmall.Outputs[0].ActualBytes != info.Size() {
		t.Fatalf("expected machine-readable target bytes, got %+v", tooSmall.Outputs[0])
	}
	found := false
	for _, check := range tooSmall.Outputs[0].Checks {
		if check.Key == checkTargetSize {
			found = true
			if check.Passed {
				t.Fatalf("expected target_size to fail the comparison, got %+v", check)
			}
			if !check.Advisory {
				t.Fatalf("expected target_size to be advisory, got %+v", check)
			}
			if !strings.Contains(check.Detail, "output is") || !strings.Contains(check.Detail, "target is") {
				t.Fatalf("expected an actionable target detail, got %q", check.Detail)
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
	if generous.Message != "verified 1 output" {
		t.Fatalf("expected no advisory warning when the target is met, got %q", generous.Message)
	}
	if generous.Outputs[0].TargetBytes != info.Size()+1 || generous.Outputs[0].ActualBytes != info.Size() {
		t.Fatalf("expected target bytes on the passing report, got %+v", generous.Outputs[0])
	}
	for _, check := range generous.Outputs[0].Checks {
		if check.Key == checkTargetSize {
			if !check.Passed {
				t.Fatalf("expected target_size to pass, got %+v", check)
			}
			if !check.Advisory {
				t.Fatalf("expected target_size to stay advisory when met, got %+v", check)
			}
			if check.Detail != "" {
				t.Fatalf("expected no detail on a passing target, got %q", check.Detail)
			}
		}
	}
}

func TestVerifyPathsWithTargetKeepsRealFailuresFatal(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	writeVerifyPNG(t, valid, 4, 4)
	missing := filepath.Join(dir, "missing.png")

	report := VerifyPathsWithTarget([]string{valid, missing}, 1)
	if report.Success {
		t.Fatal("expected a missing output to fail verification even with a target")
	}
	if !strings.Contains(report.Message, "checks failed") {
		t.Fatalf("expected a failure summary, got %q", report.Message)
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

func TestVerifyPathsRealAudioReportsDurationAndCodec(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.wav")
	writeVerifyAudio(t, path)

	report := VerifyPaths([]string{path})
	if !report.Success {
		t.Fatalf("expected a real audio file to verify, got %s (%v)", report.Message, report.Warnings)
	}

	output := report.Outputs[0]
	if output.Kind != "audio" {
		t.Fatalf("expected the audio kind, got %q", output.Kind)
	}
	checks := checksByKey(output)
	for _, key := range []string{checkPath, checkExists, checkRegularFile, checkReadable, checkSize, checkKind, checkAudioLength, checkAudioCodec} {
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
	if values[checkAudioCodec] != "pcm_s16le" {
		t.Fatalf("expected pcm_s16le, got %q", values[checkAudioCodec])
	}
	if values[checkAudioLength] != "2.0 s" {
		t.Fatalf("expected 2.0 s, got %q", values[checkAudioLength])
	}
}

func TestVerifyPathsTruncatedAudioFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.mp3")
	writeVerifyMP3(t, path)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mp3 fixture: %v", err)
	}
	// Cut below the first complete audio frames: ffprobe no longer finds a
	// decodable stream, which is exactly the truncated output this check stops.
	if len(content) < 2000 {
		t.Fatalf("fixture is unexpectedly small: %d bytes", len(content))
	}
	if err := os.WriteFile(path, content[:1000], 0o644); err != nil {
		t.Fatalf("truncate mp3 fixture: %v", err)
	}

	report := VerifyPaths([]string{path})
	if report.Success {
		t.Fatal("expected a truncated audio file to fail verification")
	}

	checks := checksByKey(report.Outputs[0])
	if passed, ok := checks[checkSize]; !ok || !passed {
		t.Fatalf("expected the size check to pass for a non-empty truncated file, got %v", checks)
	}
	if passed, ok := checks[checkAudioLength]; !ok || passed {
		t.Fatalf("expected the duration check to fail, got %v", checks)
	}
	if passed, ok := checks[checkAudioCodec]; !ok || passed {
		t.Fatalf("expected the codec check to fail, got %v", checks)
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
