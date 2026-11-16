package recipes

import (
	"archive/zip"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	ffimage "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func newWebImageSetTestService(t *testing.T) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	return newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, ffimage.NewImageToolAdapter(ffimage.NewImageConverter()))
	})
}

func writeRecipePNG(t *testing.T, path string, width, height int, transparent bool) {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if transparent {
				img.SetNRGBA(x, y, color.NRGBA{R: 210, G: 30, B: 40, A: 0})
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x * 7) % 256),
				G: uint8((y * 5) % 256),
				B: 120,
				A: 255,
			})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png fixture %s: %v", path, err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png fixture %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png fixture %s: %v", path, err)
	}
}

func readZipNames(t *testing.T, path string) []string {
	t.Helper()

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip %s: %v", path, err)
	}
	defer func() {
		_ = reader.Close()
	}()

	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	sort.Strings(names)

	return names
}

func assertDirContainsOnly(t *testing.T, dir, name string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir %s: %v", dir, err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		t.Fatalf("expected only %q to remain, got %+v", name, entries)
	}
}

func TestPlanWebImageSetWideSource(t *testing.T) {
	service, _, _ := newWebImageSetTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 2000, 1200, false)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil || plan.Recipe.ID != RecipeIDWebImageSet {
		t.Fatalf("expected the Web image set recipe, got %+v", plan.Recipe)
	}
	if plan.OutputDir != dir {
		t.Fatalf("expected the output folder to default to the input folder %q, got %q", dir, plan.OutputDir)
	}

	want := []string{
		filepath.Join(dir, "photo-1600.webp"),
		filepath.Join(dir, "photo-1600.jpg"),
		filepath.Join(dir, "photo-800.webp"),
		filepath.Join(dir, "photo-800.jpg"),
		filepath.Join(dir, "photo-web-set.zip"),
		filepath.Join(dir, "photo-srcset.html"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		if plan.Outputs[index].OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, plan.Outputs[index].OutputPath)
		}
		if plan.Outputs[index].InputPath != input || plan.Outputs[index].OutputKind != "file" {
			t.Fatalf("output %d: unexpected metadata: %+v", index, plan.Outputs[index])
		}
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("expected no cap warnings for a 2000 px source, got %+v", plan.Warnings)
	}
	if plan.Message != "Ready to build 6 files from photo.png." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}

	// Plan must not create any output.
	for _, planned := range plan.Outputs {
		if _, err := os.Stat(planned.OutputPath); !os.IsNotExist(err) {
			t.Fatalf("plan must not create %s", planned.OutputPath)
		}
	}
}

func TestPlanWebImageSetCapsTheWidestVariant(t *testing.T) {
	service, _, _ := newWebImageSetTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 1200, 900, false)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}

	want := []string{
		filepath.Join(dir, "photo-1200.webp"),
		filepath.Join(dir, "photo-1200.jpg"),
		filepath.Join(dir, "photo-800.webp"),
		filepath.Join(dir, "photo-800.jpg"),
		filepath.Join(dir, "photo-web-set.zip"),
		filepath.Join(dir, "photo-srcset.html"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		if plan.Outputs[index].OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, plan.Outputs[index].OutputPath)
		}
	}
	if !warningsContain(plan.Warnings, "1600 px variant is capped at 1200 px") {
		t.Fatalf("expected a 1600 px cap warning, got %+v", plan.Warnings)
	}
	if warningsContain(plan.Warnings, "variants are capped at the source width") {
		t.Fatalf("did not expect the sub-800 warning for a 1200 px source, got %+v", plan.Warnings)
	}
}

func TestPlanWebImageSetNarrowSourceProducesOneWidth(t *testing.T) {
	service, _, _ := newWebImageSetTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 600, 400, false)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}

	want := []string{
		filepath.Join(dir, "photo-600.webp"),
		filepath.Join(dir, "photo-600.jpg"),
		filepath.Join(dir, "photo-web-set.zip"),
		filepath.Join(dir, "photo-srcset.html"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		if plan.Outputs[index].OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, plan.Outputs[index].OutputPath)
		}
	}
	if !warningsContain(plan.Warnings, "only a 600 px variant is generated") {
		t.Fatalf("expected a source-width cap warning, got %+v", plan.Warnings)
	}
	if plan.Message != "Ready to build 4 files from photo.png." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}
}

func TestWriteWebImageSetSnippetRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo-srcset.html")
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("seed existing snippet: %v", err)
	}

	jobErr := writeWebImageSetSnippet(path, "<picture></picture>")
	if jobErr == nil {
		t.Fatal("expected the existing snippet to be refused")
	}
	if jobErr.DetailCode != "RECIPE_OUTPUT_EXISTS" {
		t.Fatalf("unexpected error: %+v", jobErr)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read existing snippet: %v", err)
	}
	if string(content) != "keep me" {
		t.Fatalf("existing snippet was overwritten: %q", string(content))
	}
}

func TestPlanWebImageSetRejectsInvalidRequests(t *testing.T) {
	service, _, _ := newWebImageSetTestService(t)

	dir := t.TempDir()
	imageOne := filepath.Join(dir, "one.png")
	imageTwo := filepath.Join(dir, "two.png")
	writeRecipePNG(t, imageOne, 40, 30, false)
	writeRecipePNG(t, imageTwo, 40, 30, false)

	textFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}

	cases := []struct {
		name       string
		req        models.RecipeRequestV1
		wantDetail string
	}{
		{
			name:       "no inputs",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDWebImageSet},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "blank input",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDWebImageSet, InputPaths: []string{"   "}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "multiple inputs",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDWebImageSet, InputPaths: []string{imageOne, imageTwo}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "non image extension",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDWebImageSet, InputPaths: []string{textFile}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "missing image",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDWebImageSet, InputPaths: []string{filepath.Join(dir, "missing.png")}},
			wantDetail: "RECIPE_INPUT_UNREADABLE",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := service.Plan(testCase.req)
			if plan.Success {
				t.Fatalf("expected the plan to fail, got %+v", plan)
			}
			if plan.Error == nil || plan.Error.DetailCode != testCase.wantDetail {
				t.Fatalf("expected detail code %q, got %+v", testCase.wantDetail, plan.Error)
			}
			if plan.Message == "" {
				t.Fatal("expected a human readable plan message")
			}
			if plan.Outputs == nil {
				t.Fatal("expected a non-nil outputs list on failure")
			}
		})
	}
}

func TestRunWebImageSetProducesVerifiedBundle(t *testing.T) {
	service, history, recorder := newWebImageSetTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 2000, 1200, false)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}
	if result.TargetSizeBytes != 0 {
		t.Fatalf("expected no target size for Web image set, got %d", result.TargetSizeBytes)
	}

	wantOutputs := []string{
		filepath.Join(dir, "photo-1600.webp"),
		filepath.Join(dir, "photo-1600.jpg"),
		filepath.Join(dir, "photo-800.webp"),
		filepath.Join(dir, "photo-800.jpg"),
		filepath.Join(dir, "photo-web-set.zip"),
		filepath.Join(dir, "photo-srcset.html"),
	}
	if len(result.Outputs) != len(wantOutputs) {
		t.Fatalf("expected %d outputs, got %+v", len(wantOutputs), result.Outputs)
	}
	for index, wantPath := range wantOutputs {
		if result.Outputs[index] != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, result.Outputs[index])
		}
		info, err := os.Stat(wantPath)
		if err != nil || info.Size() == 0 {
			t.Fatalf("expected %s to exist and be non-empty, info=%v err=%v", wantPath, info, err)
		}
	}

	dimensionCases := []struct {
		path   string
		width  int
		height int
	}{
		{path: wantOutputs[0], width: 1600, height: 960},
		{path: wantOutputs[1], width: 1600, height: 960},
		{path: wantOutputs[2], width: 800, height: 480},
		{path: wantOutputs[3], width: 800, height: 480},
	}
	for _, testCase := range dimensionCases {
		info := inspect.InspectFile(testCase.path)
		if info.Kind != "image" || info.Width != testCase.width || info.Height != testCase.height {
			t.Fatalf(
				"expected %s to be a %dx%d image, got %+v",
				filepath.Base(testCase.path),
				testCase.width,
				testCase.height,
				info,
			)
		}
	}

	zipNames := readZipNames(t, wantOutputs[4])
	wantZipNames := []string{
		"photo-1600.jpg",
		"photo-1600.webp",
		"photo-800.jpg",
		"photo-800.webp",
		"srcset.html",
	}
	sort.Strings(wantZipNames)
	if len(zipNames) != len(wantZipNames) {
		t.Fatalf("expected %d ZIP entries, got %v", len(wantZipNames), zipNames)
	}
	for index, wantName := range wantZipNames {
		if zipNames[index] != wantName {
			t.Fatalf("ZIP entry %d: expected %q, got %q", index, wantName, zipNames[index])
		}
	}

	snippetBytes, err := os.ReadFile(wantOutputs[5])
	if err != nil {
		t.Fatalf("read snippet: %v", err)
	}
	snippet := string(snippetBytes)
	for _, needle := range []string{
		"<picture>",
		"photo-800.webp 800w, photo-1600.webp 1600w",
		"photo-800.jpg 800w, photo-1600.jpg 1600w",
		"src=\"photo-800.jpg\"",
		"sizes=\"(max-width: 800px) 100vw, 800px\"",
	} {
		if !strings.Contains(snippet, needle) {
			t.Fatalf("expected the snippet to contain %q, got:\n%s", needle, snippet)
		}
	}

	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}
	variantWidthChecks := 0
	for _, output := range result.Verification.Outputs {
		for _, check := range output.Checks {
			if check.Key != webImageSetVariantWidthCheck {
				continue
			}
			variantWidthChecks++
			if !check.Passed {
				t.Fatalf("expected every variant_width check to pass, got %+v", check)
			}
		}
	}
	if variantWidthChecks != 4 {
		t.Fatalf("expected 4 variant_width checks, got %d", variantWidthChecks)
	}

	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 step results, got %+v", result.Steps)
	}
	if result.Steps[0].Status != recipeStepStatusSuccess || result.Steps[0].Message != "4 variants created" {
		t.Fatalf("unexpected build step: %+v", result.Steps[0])
	}
	if len(result.Steps[0].Outputs) != 4 {
		t.Fatalf("expected 4 build outputs, got %+v", result.Steps[0])
	}
	if result.Steps[1].Status != recipeStepStatusSuccess || result.Steps[2].Status != recipeStepStatusSuccess {
		t.Fatalf("expected the packaging steps to succeed, got %+v", result.Steps)
	}

	entries := history.List()
	if len(entries) != 1 || entries[0].JobID != run.JobID || entries[0].ToolID != webImageSetHistoryToolID {
		t.Fatalf("unexpected history entries: %+v", entries)
	}
	if len(entries[0].Outputs) != len(wantOutputs) {
		t.Fatalf("expected all %d outputs recorded for undo, got %+v", len(wantOutputs), entries[0].Outputs)
	}

	undo := history.Undo(run.JobID)
	if !undo.Success || len(undo.Moved) != len(wantOutputs) {
		t.Fatalf("expected every output to be undone, got %+v", undo)
	}
	for _, output := range wantOutputs {
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("expected %s to leave its original path, stat err = %v", output, err)
		}
	}

	restore := history.Restore(run.JobID)
	if !restore.Success || len(restore.Moved) != len(wantOutputs) {
		t.Fatalf("expected every output to be restored, got %+v", restore)
	}
	for _, output := range wantOutputs {
		if _, err := os.Stat(output); err != nil {
			t.Fatalf("expected %s to be restored: %v", output, err)
		}
	}

	terminalEvent := waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.Status == models.JobStatusSuccess && event.Percent == 100
	})
	if terminalEvent.JobID != run.JobID || terminalEvent.RecipeID != RecipeIDWebImageSet {
		t.Fatalf("unexpected terminal event: %+v", terminalEvent)
	}
}

func TestRunWebImageSetFlattensTransparencyOnJPEG(t *testing.T) {
	service, _, _ := newWebImageSetTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "logo.png")
	writeRecipePNG(t, input, 40, 30, true)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	jpegPath := filepath.Join(dir, "logo-40.jpg")
	file, err := os.Open(jpegPath)
	if err != nil {
		t.Fatalf("open jpeg output: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()

	decoded, err := jpeg.Decode(file)
	if err != nil {
		t.Fatalf("decode jpeg output: %v", err)
	}
	pixel := color.NRGBAModel.Convert(decoded.At(20, 15)).(color.NRGBA)
	if pixel.R < 240 || pixel.G < 240 || pixel.B < 240 {
		t.Fatalf("expected transparency to be flattened onto white, got %+v", pixel)
	}
}

func TestRunWebImageSetFailureRemovesCreatedFiles(t *testing.T) {
	failing := &failingImageTool{
		inner:  ffimage.NewImageToolAdapter(ffimage.NewImageConverter()),
		failAt: 2,
	}

	service, history, _ := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, failing)
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 2000, 1200, false)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil {
		t.Fatal("expected a step error in the result")
	}
	if result.Steps[buildVariantsStepIndex].Status != models.JobStatusFailed {
		t.Fatalf("expected the build step to be marked failed, got %+v", result.Steps[buildVariantsStepIndex])
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no outputs for a failed run, got %+v", result.Outputs)
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}

	assertDirContainsOnly(t, dir, "photo.png")
}

func TestRunWebImageSetCancelRemovesPartialOutputs(t *testing.T) {
	blocking := &blockingImageTool{
		inner:   ffimage.NewImageToolAdapter(ffimage.NewImageConverter()),
		started: make(chan struct{}, 1),
	}

	service, _, recorder := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, blocking)
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 2000, 1200, false)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the image step to start")
	}

	cancelled := service.Cancel(run.JobID)
	if !cancelled.Success || !cancelled.Found {
		t.Fatalf("expected cancel to be accepted, got %+v", cancelled)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusCancelled {
		t.Fatalf("expected cancelled, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil || result.Error.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected a cancellation error, got %+v", result.Error)
	}
	waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.Status == models.JobStatusCancelled
	})

	assertDirContainsOnly(t, dir, "photo.png")
}

func TestRunWebImageSetSanitizesSnippetNames(t *testing.T) {
	service, _, _ := newWebImageSetTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo & friends.png")
	writeRecipePNG(t, input, 2000, 1000, false)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDWebImageSet,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	snippetPath := filepath.Join(dir, "photo-friends-srcset.html")
	snippetBytes, err := os.ReadFile(snippetPath)
	if err != nil {
		t.Fatalf("read snippet: %v", err)
	}
	snippet := string(snippetBytes)

	if !strings.Contains(snippet, "photo-friends-1600.webp 1600w") {
		t.Fatalf("expected sanitized WebP names in the snippet, got:\n%s", snippet)
	}
	if !strings.Contains(snippet, "photo-friends-800.jpg 800w") {
		t.Fatalf("expected sanitized JPEG names in the snippet, got:\n%s", snippet)
	}
	if strings.Contains(snippet, "photo & friends") || strings.Contains(snippet, "photo%20") {
		t.Fatalf("expected every attribute value to be web safe, got:\n%s", snippet)
	}
}

func TestWebImageSetSourceStemIsWebSafe(t *testing.T) {
	cases := map[string]string{
		"/tmp/photo.png":           "photo",
		"/tmp/Photo & Friends.png": "photo-friends",
		"/tmp/  Hero__Shot  .png":  "hero__shot",
		"/tmp/ñandú.png":           "and",
		"/tmp/!!!.png":             "image",
	}

	for input, want := range cases {
		if got := webImageSetSourceStem(input); got != want {
			t.Fatalf("webImageSetSourceStem(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildWebImageSetSnippetEscapesAttributes(t *testing.T) {
	variants := []builtWebImageSetVariant{
		{Path: "/tmp/photo & friends-800.webp", Format: "webp", Width: 800},
		{Path: "/tmp/photo & friends-1600.webp", Format: "webp", Width: 1600},
		{Path: "/tmp/photo & friends-800.jpg", Format: "jpeg", Width: 800},
		{Path: "/tmp/photo & friends-1600.jpg", Format: "jpeg", Width: 1600},
	}

	snippet := buildWebImageSetSnippet(variants)
	if !strings.Contains(snippet, "photo &amp; friends-1600.webp 1600w") {
		t.Fatalf("expected escaped WebP names in the snippet, got:\n%s", snippet)
	}
	if !strings.Contains(snippet, "photo &amp; friends-800.jpg 800w") {
		t.Fatalf("expected escaped JPEG names in the snippet, got:\n%s", snippet)
	}
	if strings.Contains(snippet, "photo & friends") {
		t.Fatalf("expected every attribute value to be escaped, got:\n%s", snippet)
	}
}

// failingImageTool wraps the real adapter and fails on a chosen call so failure
// cleanup can be asserted without breaking normal conversions.
type failingImageTool struct {
	inner  *ffimage.ImageToolAdapter
	failAt int

	mu    sync.Mutex
	calls int
}

func (t *failingImageTool) ID() string { return t.inner.ID() }

func (t *failingImageTool) Capability() string { return t.inner.Capability() }

func (t *failingImageTool) Manifest() models.ToolManifestV1 { return t.inner.Manifest() }

func (t *failingImageTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	return t.inner.RuntimeState(ctx)
}

func (t *failingImageTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	return t.inner.Validate(ctx, req)
}

func (t *failingImageTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	return t.inner.Plan(ctx, req)
}

func (t *failingImageTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	t.mu.Lock()
	t.calls++
	call := t.calls
	t.mu.Unlock()

	if call == t.failAt {
		jobErr := &models.JobErrorV1{
			Code:       "IMAGE_TEST_FAILED",
			DetailCode: "IMAGE_TEST_FAILED",
			Message:    fmt.Sprintf("image conversion failed for test on call %d", call),
		}
		return models.JobResultItemV1{
			InputPath: firstRequestInput(req),
			Success:   false,
			Message:   jobErr.Message,
			Error:     jobErr,
		}, jobErr
	}

	return t.inner.ExecuteSingle(ctx, req)
}

// blockingImageTool blocks in ExecuteSingle until its context is cancelled so
// the Web image set cancel path is deterministic.
type blockingImageTool struct {
	inner   *ffimage.ImageToolAdapter
	started chan struct{}
}

func (t *blockingImageTool) ID() string { return t.inner.ID() }

func (t *blockingImageTool) Capability() string { return t.inner.Capability() }

func (t *blockingImageTool) Manifest() models.ToolManifestV1 { return t.inner.Manifest() }

func (t *blockingImageTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	return t.inner.RuntimeState(ctx)
}

func (t *blockingImageTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	return t.inner.Validate(ctx, req)
}

func (t *blockingImageTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	return t.inner.Plan(ctx, req)
}

func (t *blockingImageTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	select {
	case t.started <- struct{}{}:
	default:
	}

	<-ctx.Done()

	return models.JobResultItemV1{
		InputPath: firstRequestInput(req),
		Success:   false,
		Message:   "image conversion canceled",
	}, &models.JobErrorV1{
		Code:       "CANCELED",
		DetailCode: "IMAGE_TEST_CANCELED",
		Message:    "image conversion canceled",
	}
}
