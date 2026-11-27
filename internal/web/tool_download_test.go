package web

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/execx"
	"fileforge-desktop/internal/web/engine"
)

const (
	testVideoURL = "https://www.youtube.com/watch?v=abc123"
	testAudioURL = "https://www.youtube.com/watch?v=def456"
)

// stubWebProbe reports a fixed runtime state without invoking yt-dlp.
type stubWebProbe struct {
	err error
}

func (p stubWebProbe) Check(context.Context) error {
	return p.err
}

// stubWebBinaries fakes the PATH lookup for ffmpeg and deno so plan and runtime
// tests never depend on the machine.
func stubWebBinaries(t *testing.T, missing ...string) {
	t.Helper()

	missingSet := make(map[string]struct{}, len(missing))
	for _, name := range missing {
		missingSet[name] = struct{}{}
	}

	original := webLookPath
	webLookPath = func(name string) (string, error) {
		if _, absent := missingSet[name]; absent {
			return "", exec.ErrNotFound
		}
		return filepath.Join("/usr/bin", name), nil
	}
	t.Cleanup(func() {
		webLookPath = original
	})
}

func downloadTestTool(t *testing.T) *DownloadTool {
	t.Helper()
	stubWebBinaries(t)

	return NewDownloadToolWithDeps(stubWebProbe{}, nil)
}

func downloadItemOption(url, title, id, ext string) map[string]any {
	return map[string]any{"url": url, "title": title, "id": id, "ext": ext}
}

func downloadRequest(mode, outputDir string, urls []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDWebDownload,
		Mode:       mode,
		InputPaths: urls,
		OutputDir:  outputDir,
		Options:    options,
	}
}

func TestDownloadToolManifest(t *testing.T) {
	tool := NewDownloadTool()
	manifest := tool.Manifest()

	if tool.ID() != ToolIDWebDownload || tool.Capability() != ToolIDWebDownload {
		t.Fatalf("unexpected tool identity: %s / %s", tool.ID(), tool.Capability())
	}
	if manifest.ToolID != ToolIDWebDownload {
		t.Fatalf("unexpected manifest id: %q", manifest.ToolID)
	}
	if manifest.Name != "Download Media" {
		t.Fatalf("unexpected name: %q", manifest.Name)
	}
	if manifest.Domain != "web" {
		t.Fatalf("unexpected domain: %q", manifest.Domain)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatal("expected single and batch support")
	}
	if strings.Join(manifest.InputExtensions, ",") != "url" {
		t.Fatalf("unexpected input extensions: %v", manifest.InputExtensions)
	}
	if strings.Join(manifest.OutputExtensions, ",") != "mp4,mp3" {
		t.Fatalf("unexpected output extensions: %v", manifest.OutputExtensions)
	}
	if strings.Join(manifest.RuntimeDeps, ",") != "yt-dlp,ffmpeg,deno" {
		t.Fatalf("unexpected runtime dependencies: %v", manifest.RuntimeDeps)
	}
	if manifest.ResourceClass != "external" {
		t.Fatalf("unexpected resource class: %q", manifest.ResourceClass)
	}
	if manifest.MaxWorkers != 2 {
		t.Fatalf("unexpected max workers: %d", manifest.MaxWorkers)
	}
	if strings.Join(manifest.Tags, ",") != "web,download,youtube" {
		t.Fatalf("unexpected tags: %v", manifest.Tags)
	}
}

func TestDownloadToolRuntimeState(t *testing.T) {
	tests := []struct {
		name       string
		probeErr   error
		missing    []string
		wantStatus string
		wantReason string
	}{
		{
			name:       "everything installed",
			wantStatus: "enabled",
		},
		{
			name:       "yt-dlp missing",
			probeErr:   &engine.WebError{Code: engine.CodeRuntimeUnavailable, Message: "yt-dlp is not available; install it from the toolchain panel"},
			wantStatus: "degraded",
			wantReason: "toolchain panel",
		},
		{
			name:       "ffmpeg missing",
			missing:    []string{"ffmpeg"},
			wantStatus: "degraded",
			wantReason: "toolchain",
		},
		{
			name:       "deno missing",
			missing:    []string{"deno"},
			wantStatus: "degraded",
			wantReason: "reduced formats",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubWebBinaries(t, tt.missing...)
			tool := NewDownloadToolWithDeps(stubWebProbe{err: tt.probeErr}, nil)

			state := tool.RuntimeState(context.Background())
			if state.Status != tt.wantStatus {
				t.Fatalf("expected status %q, got %+v", tt.wantStatus, state)
			}
			if tt.wantStatus == "enabled" {
				if !state.Healthy || state.Reason != "" {
					t.Fatalf("expected a healthy enabled runtime, got %+v", state)
				}
				return
			}
			if state.Healthy {
				t.Fatalf("expected a degraded runtime, got %+v", state)
			}
			if !strings.Contains(state.Reason, tt.wantReason) {
				t.Fatalf("expected reason to mention %q, got %q", tt.wantReason, state.Reason)
			}
		})
	}
}

func TestDownloadPlanSingle(t *testing.T) {
	outDir := t.TempDir()
	tool := downloadTestTool(t)
	request := downloadRequest("single", outDir, []string{testVideoURL}, map[string]any{
		"items": []any{downloadItemOption(testVideoURL, "  My/ Talk\\ Part\u0007 1  ", "abc123", "mp4")},
	})

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if !plan.Success || len(plan.Outputs) != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	output := plan.Outputs[0]
	wantPath := filepath.Join(outDir, "My Talk Part 1 [abc123].mp4")
	if output.OutputPath != wantPath {
		t.Fatalf("expected output %q, got %q", wantPath, output.OutputPath)
	}
	if output.InputPath != testVideoURL {
		t.Fatalf("expected the url as input path, got %q", output.InputPath)
	}
	if output.OutputKind != webOutputKindFile {
		t.Fatalf("unexpected output kind: %q", output.OutputKind)
	}
	if output.Exists || output.Adjusted {
		t.Fatalf("expected a fresh natural output, got %+v", output)
	}
	if _, statErr := os.Stat(wantPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan must not create the output, got %v", statErr)
	}

	if plan.EffectiveOptions["kind"] != engine.KindVideo {
		t.Fatalf("expected default kind video, got %v", plan.EffectiveOptions["kind"])
	}
	if got := plan.EffectiveOptions["maxHeight"]; got != 0 {
		t.Fatalf("expected default maxHeight 0, got %v", got)
	}
	if _, exists := plan.EffectiveOptions["audioBitrateKbps"]; exists {
		t.Fatal("video plans must not carry an audio bitrate")
	}

	assertEffectiveItem(t, plan, 0, wantPath, engine.KindVideo, 0, 0)
}

func TestDownloadPlanBatchNeverCollides(t *testing.T) {
	outDir := t.TempDir()
	tool := downloadTestTool(t)
	urls := []string{testVideoURL, "https://www.youtube.com/watch?v=xyz789"}
	request := downloadRequest("batch", outDir, urls, map[string]any{
		"items": []any{
			downloadItemOption(urls[0], "Same Title", "abc123", "mp4"),
			downloadItemOption(urls[1], "Same Title", "abc123", "mp4"),
		},
	})

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	first := filepath.Join(outDir, "Same Title [abc123].mp4")
	second := filepath.Join(outDir, "Same Title [abc123]-2.mp4")
	if plan.Outputs[0].OutputPath != first {
		t.Fatalf("expected first output %q, got %q", first, plan.Outputs[0].OutputPath)
	}
	if plan.Outputs[1].OutputPath != second {
		t.Fatalf("expected second output %q, got %q", second, plan.Outputs[1].OutputPath)
	}
	if !plan.Outputs[1].Adjusted {
		t.Fatal("expected the reserved collision to be reported as adjusted")
	}
	if len(plan.Outputs[1].Warnings) != 1 || !strings.Contains(plan.Outputs[1].Warnings[0], "-2.mp4") {
		t.Fatalf("expected a collision warning, got %v", plan.Outputs[1].Warnings)
	}
}

func TestDownloadPlanSuffixesExistingFile(t *testing.T) {
	outDir := t.TempDir()
	existing := filepath.Join(outDir, "Clip [abc123].mp4")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatalf("write existing fixture: %v", err)
	}

	tool := downloadTestTool(t)
	plan, planErr := tool.Plan(context.Background(), downloadRequest("single", outDir, []string{testVideoURL}, map[string]any{
		"items": []any{downloadItemOption(testVideoURL, "Clip", "abc123", "mp4")},
	}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "Clip [abc123]-2.mp4")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if !plan.Outputs[0].Adjusted {
		t.Fatal("expected the collision to be reported as adjusted")
	}
	if len(plan.Outputs[0].Warnings) != 1 || !strings.Contains(plan.Outputs[0].Warnings[0], "-2.mp4") {
		t.Fatalf("expected a collision warning, got %v", plan.Outputs[0].Warnings)
	}
}

func TestDownloadPlanExplicitOutputPath(t *testing.T) {
	outDir := t.TempDir()
	explicit := filepath.Join(outDir, "hand picked.mp3")
	tool := downloadTestTool(t)

	plan, planErr := tool.Plan(context.Background(), downloadRequest("single", outDir, []string{testAudioURL}, map[string]any{
		"kind":       engine.KindAudio,
		"outputPath": explicit,
		"items":      []any{downloadItemOption(testAudioURL, "Any Title", "def456", "m4a")},
	}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if plan.Outputs[0].OutputPath != explicit {
		t.Fatalf("expected the explicit path %q, got %q", explicit, plan.Outputs[0].OutputPath)
	}
	if plan.EffectiveOptions["audioBitrateKbps"] != 0 {
		t.Fatalf("expected the default bitrate, got %v", plan.EffectiveOptions["audioBitrateKbps"])
	}
	assertEffectiveItem(t, plan, 0, explicit, engine.KindAudio, 0, 0)
}

func TestDownloadPlanRejectsExplicitOutputPathInBatch(t *testing.T) {
	outDir := t.TempDir()
	tool := downloadTestTool(t)
	urls := []string{testVideoURL, testAudioURL}

	_, planErr := tool.Plan(context.Background(), downloadRequest("batch", outDir, urls, map[string]any{
		"outputPath": filepath.Join(outDir, "one.mp4"),
		"items": []any{
			downloadItemOption(urls[0], "One", "abc123", "mp4"),
			downloadItemOption(urls[1], "Two", "def456", "mp4"),
		},
	}))
	if planErr == nil || planErr.DetailCode != detailWebOptionsInvalid {
		t.Fatalf("expected WEB_DOWNLOAD_OPTIONS_INVALID, got %+v", planErr)
	}
}

func TestDownloadPlanRequiresItems(t *testing.T) {
	tool := downloadTestTool(t)

	_, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{testVideoURL}, nil))
	if planErr == nil || planErr.DetailCode != detailWebItemsRequired {
		t.Fatalf("expected WEB_DOWNLOAD_ITEMS_REQUIRED, got %+v", planErr)
	}
	if !strings.Contains(planErr.Message, "probe") {
		t.Fatalf("expected the message to tell the caller to probe, got %q", planErr.Message)
	}
	if planErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected a validation family code, got %q", planErr.Code)
	}
}

func TestDownloadPlanRejectsItemsMismatch(t *testing.T) {
	tool := downloadTestTool(t)
	urls := []string{testVideoURL, testAudioURL}

	t.Run("missing entry", func(t *testing.T) {
		_, planErr := tool.Plan(context.Background(), downloadRequest("batch", t.TempDir(), urls, map[string]any{
			"items": []any{downloadItemOption(urls[0], "One", "abc123", "mp4")},
		}))
		if planErr == nil || planErr.DetailCode != detailWebItemsMismatch {
			t.Fatalf("expected WEB_DOWNLOAD_ITEMS_MISMATCH, got %+v", planErr)
		}
	})

	t.Run("url does not match", func(t *testing.T) {
		_, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{testVideoURL}, map[string]any{
			"items": []any{downloadItemOption(testAudioURL, "One", "abc123", "mp4")},
		}))
		if planErr == nil || planErr.DetailCode != detailWebItemsMismatch {
			t.Fatalf("expected WEB_DOWNLOAD_ITEMS_MISMATCH, got %+v", planErr)
		}
	})

	t.Run("entry is not an object", func(t *testing.T) {
		_, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{testVideoURL}, map[string]any{
			"items": []any{"https://www.youtube.com/watch?v=abc123"},
		}))
		if planErr == nil || planErr.DetailCode != detailWebItemsMismatch {
			t.Fatalf("expected WEB_DOWNLOAD_ITEMS_MISMATCH, got %+v", planErr)
		}
	})
}

func TestDownloadPlanRejectsInvalidURL(t *testing.T) {
	tool := downloadTestTool(t)
	badURL := "ftp://example.com/clip"

	_, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{badURL}, map[string]any{
		"items": []any{downloadItemOption(badURL, "Clip", "abc123", "mp4")},
	}))
	if planErr == nil || planErr.DetailCode != engine.CodeInvalidURL {
		t.Fatalf("expected %s, got %+v", engine.CodeInvalidURL, planErr)
	}
}

func TestDownloadPlanAudioDefaults(t *testing.T) {
	outDir := t.TempDir()
	tool := downloadTestTool(t)

	plan, planErr := tool.Plan(context.Background(), downloadRequest("single", outDir, []string{testAudioURL}, map[string]any{
		"kind":             engine.KindAudio,
		"maxHeight":        720,
		"audioBitrateKbps": 192,
		"items":            []any{downloadItemOption(testAudioURL, "Track", "def456", "webm")},
	}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "Track [def456].mp3")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if plan.EffectiveOptions["kind"] != engine.KindAudio {
		t.Fatalf("expected kind audio, got %v", plan.EffectiveOptions["kind"])
	}
	if plan.EffectiveOptions["audioBitrateKbps"] != 192 {
		t.Fatalf("expected bitrate 192, got %v", plan.EffectiveOptions["audioBitrateKbps"])
	}
	if _, exists := plan.EffectiveOptions["maxHeight"]; exists {
		t.Fatal("audio plans must not carry a video maxHeight")
	}
	assertEffectiveItem(t, plan, 0, want, engine.KindAudio, 720, 192)
}

func TestDownloadPlanFailsWhenRuntimeIsDegraded(t *testing.T) {
	stubWebBinaries(t)
	probeErr := &engine.WebError{
		Code:       engine.CodeRuntimeUnavailable,
		DetailCode: "yt_dlp_not_found",
		Message:    "yt-dlp is not available; install it from the toolchain panel",
	}
	tool := NewDownloadToolWithDeps(stubWebProbe{err: probeErr}, nil)

	plan, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{testVideoURL}, map[string]any{
		"items": []any{downloadItemOption(testVideoURL, "Clip", "abc123", "mp4")},
	}))
	if planErr == nil || planErr.DetailCode != engine.CodeRuntimeUnavailable {
		t.Fatalf("expected %s, got %+v", engine.CodeRuntimeUnavailable, planErr)
	}
	if planErr.Code != models.ErrorCodeRuntimeDepMissing {
		t.Fatalf("expected the runtime family, got %q", planErr.Code)
	}
	if plan.Success || len(plan.Outputs) != 0 {
		t.Fatalf("expected a failed empty plan, got %+v", plan)
	}
}

func TestDownloadPlanFailsWhenFFmpegIsMissing(t *testing.T) {
	stubWebBinaries(t, "ffmpeg")
	tool := NewDownloadToolWithDeps(stubWebProbe{}, nil)

	_, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{testVideoURL}, map[string]any{
		"items": []any{downloadItemOption(testVideoURL, "Clip", "abc123", "mp4")},
	}))
	if planErr == nil || planErr.DetailCode != engine.CodeRuntimeUnavailable {
		t.Fatalf("expected %s, got %+v", engine.CodeRuntimeUnavailable, planErr)
	}
	if !strings.Contains(planErr.Message, "toolchain") {
		t.Fatalf("expected actionable guidance, got %q", planErr.Message)
	}
}

func TestDownloadPlanDoesNotRequireDeno(t *testing.T) {
	stubWebBinaries(t, "deno")
	tool := NewDownloadToolWithDeps(stubWebProbe{}, nil)

	plan, planErr := tool.Plan(context.Background(), downloadRequest("single", t.TempDir(), []string{testVideoURL}, map[string]any{
		"items": []any{downloadItemOption(testVideoURL, "Clip", "abc123", "mp4")},
	}))
	if planErr != nil {
		t.Fatalf("a missing Deno only degrades the formats, plan failed: %v", planErr)
	}
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan)
	}
}

func TestDownloadPlanRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
	}{
		{name: "invalid kind", options: map[string]any{"kind": "playlist"}},
		{name: "negative max height", options: map[string]any{"maxHeight": -1}},
		{name: "fractional bitrate", options: map[string]any{"audioBitrateKbps": 192.5}},
		{name: "missing output dir", options: map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := downloadTestTool(t)
			options := map[string]any{"items": []any{downloadItemOption(testVideoURL, "Clip", "abc123", "mp4")}}
			for key, value := range tt.options {
				options[key] = value
			}
			outputDir := t.TempDir()
			if tt.name == "missing output dir" {
				outputDir = ""
			}

			_, planErr := tool.Plan(context.Background(), downloadRequest("single", outputDir, []string{testVideoURL}, options))
			if planErr == nil || planErr.DetailCode != detailWebOptionsInvalid {
				t.Fatalf("expected %s, got %+v", detailWebOptionsInvalid, planErr)
			}
		})
	}
}

func TestSanitizeDownloadTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "separators and controls", title: " A/B\\C\u0007 D ", want: "A B C D"},
		{name: "collapses whitespace", title: "one   two\tthree", want: "one two three"},
		{name: "empty falls back", title: " / \\ ", want: "download"},
		{name: "caps at 120 runes", title: strings.Repeat("á", 130), want: strings.Repeat("á", 120)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDownloadTitle(tt.title); got != tt.want {
				t.Fatalf("sanitizeDownloadTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestMapWebError(t *testing.T) {
	tests := []struct {
		name       string
		webErr     *engine.WebError
		wantNil    bool
		wantCode   string
		wantDetail string
	}{
		{name: "nil is success", webErr: nil, wantNil: true},
		{name: "invalid url", webErr: &engine.WebError{Code: engine.CodeInvalidURL, Message: "bad"}, wantCode: models.ErrorCodeValidationInvalidInput, wantDetail: engine.CodeInvalidURL},
		{name: "format unavailable", webErr: &engine.WebError{Code: engine.CodeFormatUnavailable, Message: "no"}, wantCode: models.ErrorCodeValidationInvalidInput, wantDetail: engine.CodeFormatUnavailable},
		{name: "runtime unavailable", webErr: &engine.WebError{Code: engine.CodeRuntimeUnavailable, Message: "missing"}, wantCode: models.ErrorCodeRuntimeDepMissing, wantDetail: engine.CodeRuntimeUnavailable},
		{name: "js runtime missing", webErr: &engine.WebError{Code: engine.CodeJSRuntimeMissing, Message: "deno"}, wantCode: models.ErrorCodeRuntimeDepMissing, wantDetail: engine.CodeJSRuntimeMissing},
		{name: "unsupported site", webErr: &engine.WebError{Code: engine.CodeUnsupportedSite, Message: "site"}, wantCode: models.ErrorCodeUnsupportedFormat, wantDetail: engine.CodeUnsupportedSite},
		{name: "metadata failed", webErr: &engine.WebError{Code: engine.CodeMetadataFailed, Message: "meta"}, wantCode: models.ErrorCodeExecIOTransient, wantDetail: engine.CodeMetadataFailed},
		{name: "execution failed", webErr: &engine.WebError{Code: engine.CodeExecutionFailed, Message: "run"}, wantCode: models.ErrorCodeExecIOTransient, wantDetail: engine.CodeExecutionFailed},
		{name: "no output", webErr: &engine.WebError{Code: engine.CodeNoOutput, Message: "empty"}, wantCode: models.ErrorCodeExecIOTransient, wantDetail: engine.CodeNoOutput},
		{name: "canceled", webErr: &engine.WebError{Code: engine.CodeCanceled, Message: "stop"}, wantCode: models.ErrorCodeCancelledByUser, wantDetail: engine.CodeCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobErr := mapWebError(tt.webErr)
			if tt.wantNil {
				if jobErr != nil {
					t.Fatalf("expected nil, got %+v", jobErr)
				}
				return
			}
			if jobErr == nil {
				t.Fatal("expected a job error")
			}
			if jobErr.Code != tt.wantCode || jobErr.DetailCode != tt.wantDetail {
				t.Fatalf("got %s/%s, want %s/%s", jobErr.Code, jobErr.DetailCode, tt.wantCode, tt.wantDetail)
			}
		})
	}
}

func assertEffectiveItem(t *testing.T, plan models.PlanJobResponse, index int, outputPath, kind string, maxHeight, bitrate int) {
	t.Helper()

	items, ok := plan.EffectiveOptions["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected effective items as []map[string]any, got %#v", plan.EffectiveOptions["items"])
	}
	if index >= len(items) {
		t.Fatalf("effective items has %d entries, want index %d", len(items), index)
	}

	item := items[index]
	if item["outputPath"] != outputPath {
		t.Fatalf("expected item outputPath %q, got %v", outputPath, item["outputPath"])
	}
	if item["kind"] != kind {
		t.Fatalf("expected item kind %q, got %v", kind, item["kind"])
	}
	if item["maxHeight"] != maxHeight {
		t.Fatalf("expected item maxHeight %d, got %v", maxHeight, item["maxHeight"])
	}
	if item["audioBitrateKbps"] != bitrate {
		t.Fatalf("expected item bitrate %d, got %v", bitrate, item["audioBitrateKbps"])
	}
	if item["url"] == "" {
		t.Fatal("expected the item url")
	}
}

// fakeWebRunner scripts yt-dlp without a process. Each call records its
// arguments and can emit stdout lines before returning the canned result.
type fakeWebRunner struct {
	calls []webRunnerCall
	run   func(call webRunnerCall, emit func(line string)) (execx.Result, error)
}

type webRunnerCall struct {
	path string
	args []string
}

func (r *fakeWebRunner) Run(_ context.Context, path string, args []string, stdout func(line string)) (execx.Result, error) {
	call := webRunnerCall{path: path, args: append([]string(nil), args...)}
	r.calls = append(r.calls, call)

	if r.run == nil {
		return execx.Result{}, nil
	}

	return r.run(call, stdout)
}

func explicitOutputArg(args []string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "-o" {
			return args[index+1]
		}
	}

	return ""
}

func writeDownloadOutput(call webRunnerCall) error {
	outputPath := explicitOutputArg(call.args)
	if outputPath == "" {
		return errors.New("expected an explicit -o target")
	}

	return os.WriteFile(outputPath, []byte("media"), 0o644)
}

func planDownload(t *testing.T, tool *DownloadTool, request models.JobRequest) models.PlanJobResponse {
	t.Helper()

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}

	return plan
}

// downloadFixtures plans a batch of count links (1 = single) inside a temp
// output directory and returns the tool ready for a fake runner.
func downloadFixtures(t *testing.T, count int) (*DownloadTool, models.JobRequest, models.PlanJobResponse) {
	t.Helper()

	outDir := t.TempDir()
	tool := downloadTestTool(t)
	urls := []string{testVideoURL}
	items := []any{downloadItemOption(testVideoURL, "Clip", "abc123", "mp4")}
	mode := "single"
	if count == 2 {
		urls = []string{testVideoURL, testAudioURL}
		items = []any{
			downloadItemOption(urls[0], "One", "abc123", "mp4"),
			downloadItemOption(urls[1], "Two", "def456", "mp4"),
		}
		mode = "batch"
	}
	request := downloadRequest(mode, outDir, urls, map[string]any{"items": items})

	return tool, request, planDownload(t, tool, request)
}

func TestExecutePlanSingleSuccess(t *testing.T) {
	tool, request, plan := downloadFixtures(t, 1)
	plannedPath := plan.Outputs[0].OutputPath

	runner := &fakeWebRunner{run: func(call webRunnerCall, emit func(string)) (execx.Result, error) {
		emit("ff-dl 0 100 0 NA")
		emit("ff-dl 50 100 512 1")
		if err := writeDownloadOutput(call); err != nil {
			return execx.Result{}, err
		}
		emit("ff-dl 100 100 NA 0")
		emit(plannedPath)
		return execx.Result{}, nil
	}}
	tool.runner = runner

	var samples []models.JobProgress
	items, execErr := tool.ExecutePlan(context.Background(), request, plan, func(progress models.JobProgress) {
		samples = append(samples, progress)
	})
	if execErr != nil {
		t.Fatalf("expected a successful download, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(items))
	}

	item := items[0]
	if !item.Success || item.OutputPath != plannedPath || item.InputPath != testVideoURL {
		t.Fatalf("unexpected result item: %+v", item)
	}
	if item.OutputCount != 1 || item.Message != "Download successful" {
		t.Fatalf("unexpected result item metadata: %+v", item)
	}
	if len(runner.calls) != 1 || runner.calls[0].path != "yt-dlp" {
		t.Fatalf("expected one yt-dlp call, got %+v", runner.calls)
	}
	if got := runner.calls[0].args[len(runner.calls[0].args)-1]; got != testVideoURL {
		t.Fatalf("expected the url last, got %q", got)
	}
	if explicitOutputArg(runner.calls[0].args) != plannedPath {
		t.Fatalf("expected the planned path as -o, got %v", runner.calls[0].args)
	}

	if len(samples) == 0 {
		t.Fatal("expected progress samples")
	}
	assertMonotonicProgress(t, samples)
	last := samples[len(samples)-1]
	if last.Current != 100 || last.Total != 100 || last.Stage != webDownloadStage {
		t.Fatalf("unexpected final progress: %+v", last)
	}
}

func TestExecutePlanBatchProgressIsMonotonic(t *testing.T) {
	tool, request, plan := downloadFixtures(t, 2)
	plannedByURL := map[string]string{}
	for _, output := range plan.Outputs {
		plannedByURL[output.InputPath] = output.OutputPath
	}

	runner := &fakeWebRunner{run: func(call webRunnerCall, emit func(string)) (execx.Result, error) {
		url := call.args[len(call.args)-1]
		emit("ff-dl 0 100 0 NA")
		emit("ff-dl 25 100 256 3")
		emit("ff-dl 50 100 512 1")
		if err := writeDownloadOutput(call); err != nil {
			return execx.Result{}, err
		}
		emit("ff-dl 100 100 NA 0")
		emit(plannedByURL[url])
		return execx.Result{}, nil
	}}
	tool.runner = runner

	var samples []models.JobProgress
	items, execErr := tool.ExecutePlan(context.Background(), request, plan, func(progress models.JobProgress) {
		samples = append(samples, progress)
	})
	if execErr != nil {
		t.Fatalf("expected a successful batch, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}

	assertMonotonicProgress(t, samples)
	last := samples[len(samples)-1]
	if last.Current != 100 {
		t.Fatalf("expected overall progress 100, got %+v", last)
	}
}

func TestExecutePlanIsolatesItemFailures(t *testing.T) {
	tool, request, plan := downloadFixtures(t, 2)
	plannedByURL := map[string]string{}
	for _, output := range plan.Outputs {
		plannedByURL[output.InputPath] = output.OutputPath
	}

	runner := &fakeWebRunner{run: func(call webRunnerCall, emit func(string)) (execx.Result, error) {
		url := call.args[len(call.args)-1]
		if url == testVideoURL {
			return execx.Result{ExitCode: 1, Stderr: "ERROR: unable to download video data"}, errors.New("exit status 1")
		}
		if err := writeDownloadOutput(call); err != nil {
			return execx.Result{}, err
		}
		emit(plannedByURL[url])
		return execx.Result{}, nil
	}}
	tool.runner = runner

	items, execErr := tool.ExecutePlan(context.Background(), request, plan, nil)
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}
	if items[0].Success || items[0].Error == nil {
		t.Fatalf("expected the first item to fail, got %+v", items[0])
	}
	if items[0].Error.DetailCode != engine.CodeExecutionFailed {
		t.Fatalf("unexpected failure code: %+v", items[0].Error)
	}
	if !items[1].Success || items[1].Error != nil {
		t.Fatalf("expected the second item to succeed, got %+v", items[1])
	}
	if execErr == nil || execErr.DetailCode != engine.CodeExecutionFailed {
		t.Fatalf("expected the first failure as the job error, got %+v", execErr)
	}
}

func TestExecutePlanCancellation(t *testing.T) {
	tool, request, plan := downloadFixtures(t, 1)

	t.Run("before the first item", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		runner := &fakeWebRunner{}
		tool.runner = runner

		items, execErr := tool.ExecutePlan(ctx, request, plan, nil)
		if len(items) != 0 {
			t.Fatalf("expected no result items, got %d", len(items))
		}
		assertCanceledDownload(t, execErr)
		if len(runner.calls) != 0 {
			t.Fatalf("no command may run after cancellation, got %d", len(runner.calls))
		}
	})

	t.Run("mid download", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := &fakeWebRunner{run: func(webRunnerCall, func(string)) (execx.Result, error) {
			cancel()
			return execx.Result{}, context.Canceled
		}}
		tool.runner = runner

		items, execErr := tool.ExecutePlan(ctx, request, plan, nil)
		if len(items) != 1 {
			t.Fatalf("expected the attempted item to be reported, got %d", len(items))
		}
		if items[0].Success || items[0].Error == nil || items[0].Error.DetailCode != engine.CodeCanceled {
			t.Fatalf("expected a canceled failure item, got %+v", items[0])
		}
		assertCanceledDownload(t, execErr)
	})
}

func TestExecuteSingleAndBatchPaths(t *testing.T) {
	tool, request, _ := downloadFixtures(t, 1)
	tool.runner = &fakeWebRunner{run: func(call webRunnerCall, emit func(string)) (execx.Result, error) {
		if err := writeDownloadOutput(call); err != nil {
			return execx.Result{}, err
		}
		emit(explicitOutputArg(call.args))
		return execx.Result{}, nil
	}}

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected ExecuteSingle to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %+v", item)
	}

	batchTool, batchRequest, _ := downloadFixtures(t, 2)
	batchTool.runner = &fakeWebRunner{run: func(call webRunnerCall, emit func(string)) (execx.Result, error) {
		if err := writeDownloadOutput(call); err != nil {
			return execx.Result{}, err
		}
		emit(explicitOutputArg(call.args))
		return execx.Result{}, nil
	}}

	items, batchErr := batchTool.ExecuteBatch(context.Background(), batchRequest, nil)
	if batchErr != nil {
		t.Fatalf("expected ExecuteBatch to succeed, got %v", batchErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 batch items, got %d", len(items))
	}
}

func assertMonotonicProgress(t *testing.T, samples []models.JobProgress) {
	t.Helper()

	last := -1
	for _, sample := range samples {
		if sample.Total != 100 || sample.Stage != webDownloadStage {
			t.Fatalf("unexpected progress shape: %+v", sample)
		}
		if sample.Current < last {
			t.Fatalf("progress went backwards: %d after %d (%+v)", sample.Current, last, sample)
		}
		if sample.Current < 0 || sample.Current > 100 {
			t.Fatalf("progress out of range: %+v", sample)
		}
		last = sample.Current
	}
}

func assertCanceledDownload(t *testing.T, jobErr *models.JobError) {
	t.Helper()

	if jobErr == nil || jobErr.DetailCode != engine.CodeCanceled {
		t.Fatalf("expected %s, got %+v", engine.CodeCanceled, jobErr)
	}
	if jobErr.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected the cancellation family, got %q", jobErr.Code)
	}
}
