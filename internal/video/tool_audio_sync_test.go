package video

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

// audioSyncFixture is one temp directory with a video, a source and a stubbed
// ffprobe that answers for both.
type audioSyncFixture struct {
	dir        string
	video      string
	source     string
	videoMeta  inspect.VideoMetadata
	sourceMeta inspect.VideoMetadata
	tool       *AudioSyncTool
}

func newAudioSyncFixture(t *testing.T) *audioSyncFixture {
	t.Helper()

	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")
	source := filepath.Join(dir, "recorder.m4a")
	writeVideoFixture(t, video)
	writeVideoFixture(t, source)

	fixture := &audioSyncFixture{
		dir:        dir,
		video:      video,
		source:     source,
		videoMeta:  inspect.VideoMetadata{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac", FrameRate: 30},
		sourceMeta: inspect.VideoMetadata{DurationSeconds: 12, AudioCodec: "aac"},
	}

	original := probeVideoMetadata
	t.Cleanup(func() {
		probeVideoMetadata = original
	})
	probeVideoMetadata = func(_ context.Context, path string) (inspect.VideoMetadata, bool) {
		switch strings.TrimSpace(path) {
		case video:
			return fixture.videoMeta, true
		case source:
			return fixture.sourceMeta, true
		default:
			return inspect.VideoMetadata{}, false
		}
	}

	fixture.tool = NewAudioSyncToolWithDeps(stubRuntimeProbe{}, nil)

	return fixture
}

func (fixture *audioSyncFixture) replaceOptions() map[string]any {
	return map[string]any{
		"sourcePath":    fixture.source,
		"offsetSeconds": 2.0,
	}
}

func TestAudioSyncPlanReplaceAutoOutputAndEffectiveOptions(t *testing.T) {
	fixture := newAudioSyncFixture(t)

	options := fixture.replaceOptions()
	options["normalize"] = true
	options["originalAudio"] = "keep"
	options["sourceGainDb"] = -3.5
	options["audioBitrateKbps"] = 192

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, options))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a successful plan")
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("planned outputs = %d, want 1", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	if want := filepath.Join(fixture.dir, "camera_synced.mp4"); output.OutputPath != want {
		t.Fatalf("output = %q, want %q", output.OutputPath, want)
	}
	if output.OutputKind != "file" {
		t.Fatalf("output kind = %q, want file", output.OutputKind)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("expected no warnings for an h264 source, got %v", plan.Warnings)
	}

	wantOptions := map[string]any{
		"mode":               "replace",
		"sourcePath":         fixture.source,
		"offsetSeconds":      2.0,
		"sourceStartSeconds": 0.0,
		"originalAudio":      "keep",
		"originalGainDb":     -18.0,
		"sourceGainDb":       -3.5,
		"audioBitrateKbps":   192,
		"normalize":          true,
		"durationSeconds":    10.0,
		"frameRate":          30.0,
		"container":          "mp4",
	}
	for key, want := range wantOptions {
		if got := plan.EffectiveOptions[key]; got != want {
			t.Fatalf("effectiveOptions[%q] = %#v, want %#v", key, got, want)
		}
	}
}

func TestAudioSyncPlanMkvFallbackWarns(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	fixture.videoMeta.VideoCodec = "mpeg4"

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, fixture.replaceOptions()))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}
	if want := filepath.Join(fixture.dir, "camera_synced.mkv"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("output = %q, want %q", plan.Outputs[0].OutputPath, want)
	}
	if got := plan.EffectiveOptions["container"]; got != "mkv" {
		t.Fatalf("container = %#v, want mkv", got)
	}
	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "mpeg4") || !strings.Contains(plan.Warnings[0], "mkv") {
		t.Fatalf("expected an mkv fallback warning naming the codec, got %v", plan.Warnings)
	}
}

func TestAudioSyncPlanSuffixesExistingAutoOutput(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	writeVideoFixture(t, filepath.Join(fixture.dir, "camera_synced.mp4"))

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, fixture.replaceOptions()))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(fixture.dir, "camera_synced-2.mp4"); output.OutputPath != want {
		t.Fatalf("output = %q, want the collision-free %q", output.OutputPath, want)
	}
	if !output.Adjusted {
		t.Fatal("expected the output to be marked adjusted")
	}
	if len(output.Warnings) != 1 {
		t.Fatalf("expected one adjusted-name warning, got %v", output.Warnings)
	}
}

func TestAudioSyncPlanReservesTheSourcePath(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	reservedSource := filepath.Join(fixture.dir, "camera_synced.mp4")
	writeVideoFixture(t, reservedSource)
	probeVideoMetadata = func(_ context.Context, path string) (inspect.VideoMetadata, bool) {
		switch strings.TrimSpace(path) {
		case fixture.video:
			return fixture.videoMeta, true
		case reservedSource:
			return fixture.sourceMeta, true
		default:
			return inspect.VideoMetadata{}, false
		}
	}

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, map[string]any{
		"sourcePath": reservedSource,
	}))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}
	if want := filepath.Join(fixture.dir, "camera_synced-2.mp4"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("output = %q, want the source path reserved so %q is used", plan.Outputs[0].OutputPath, want)
	}
}

func TestAudioSyncPlanExplicitOutput(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	explicit := filepath.Join(fixture.dir, "export", "custom.mp4")
	if err := os.MkdirAll(filepath.Dir(explicit), 0o755); err != nil {
		t.Fatalf("mkdir export: %v", err)
	}

	options := fixture.replaceOptions()
	options["outputPath"] = explicit
	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, options))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}
	if plan.Outputs[0].OutputPath != explicit {
		t.Fatalf("output = %q, want the explicit %q", plan.Outputs[0].OutputPath, explicit)
	}
}

func TestAudioSyncPlanSectionsAreSortedWithDefaults(t *testing.T) {
	fixture := newAudioSyncFixture(t)

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, map[string]any{
		"mode": "sections",
		"segments": []any{
			map[string]any{"videoStart": 5.0, "videoEnd": 8.0, "sourcePath": fixture.source, "sourceStart": 2.0, "gainDb": -6.0},
			map[string]any{"videoStart": 1.0, "videoEnd": 3.0, "sourcePath": fixture.source},
		},
	}))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}

	segments, ok := plan.EffectiveOptions["segments"].([]map[string]any)
	if !ok {
		t.Fatalf("effective segments = %#v, want []map[string]any", plan.EffectiveOptions["segments"])
	}
	if len(segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(segments))
	}
	if segments[0]["videoStart"] != 1.0 || segments[1]["videoStart"] != 5.0 {
		t.Fatalf("segments are not sorted by videoStart: %+v", segments)
	}
	if segments[0]["fadeMs"] != 15.0 {
		t.Fatalf("default fade = %v ms, want 15", segments[0]["fadeMs"])
	}
	if segments[0]["gainDb"] != 0.0 || segments[0]["sourceStart"] != 0.0 {
		t.Fatalf("unexpected segment defaults: %+v", segments[0])
	}
	if segments[1]["gainDb"] != -6.0 {
		t.Fatalf("segment gain = %v, want -6", segments[1]["gainDb"])
	}
	if got := plan.EffectiveOptions["sourcePath"]; got != nil {
		t.Fatalf("sections must not carry a replace sourcePath, got %#v", got)
	}
}

func TestAudioSyncPlanValidationCodes(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	existing := filepath.Join(fixture.dir, "camera_synced.mp4")
	writeVideoFixture(t, existing)

	silentSource := filepath.Join(fixture.dir, "silent.m4a")
	writeVideoFixture(t, silentSource)
	originalProbe := probeVideoMetadata

	newTool := func(t *testing.T) *AudioSyncTool {
		t.Helper()
		return NewAudioSyncToolWithDeps(stubRuntimeProbe{}, nil)
	}

	tests := []struct {
		name    string
		mutate  func(*models.JobRequest)
		prepare func()
		want    string
	}{
		{
			name:   "mode must be single",
			mutate: func(req *models.JobRequest) { req.Mode = "batch" },
			want:   engine.ErrorCodeVideoAudioSyncValidation,
		},
		{
			name:   "exactly one input",
			mutate: func(req *models.JobRequest) { req.InputPaths = nil },
			want:   engine.ErrorCodeVideoAudioSyncValidation,
		},
		{
			name:   "missing video file",
			mutate: func(req *models.JobRequest) { req.InputPaths = []string{filepath.Join(fixture.dir, "missing.mp4")} },
			want:   engine.ErrorCodeVideoAudioSyncValidation,
		},
		{
			name: "metadata unavailable",
			mutate: func(req *models.JobRequest) {
				probeVideoMetadata = func(context.Context, string) (inspect.VideoMetadata, bool) { return inspect.VideoMetadata{}, false }
			},
			want: engine.ErrorCodeVideoAudioSyncDurationUnavailable,
		},
		{
			name: "no video stream",
			mutate: func(req *models.JobRequest) {
				fixture.videoMeta.VideoCodec = ""
			},
			want: engine.ErrorCodeVideoAudioSyncInputNoVideo,
		},
		{
			name: "no duration",
			mutate: func(req *models.JobRequest) {
				fixture.videoMeta.DurationSeconds = 0
			},
			want: engine.ErrorCodeVideoAudioSyncDurationUnavailable,
		},
		{
			name:   "invalid mode option",
			mutate: func(req *models.JobRequest) { req.Options["mode"] = "guess" },
			want:   engine.ErrorCodeVideoAudioSyncModeInvalid,
		},
		{
			name:   "missing source path",
			mutate: func(req *models.JobRequest) { delete(req.Options, "sourcePath") },
			want:   engine.ErrorCodeVideoAudioSyncValidation,
		},
		{
			name:   "source does not exist",
			mutate: func(req *models.JobRequest) { req.Options["sourcePath"] = filepath.Join(fixture.dir, "missing.m4a") },
			want:   engine.ErrorCodeVideoAudioSyncSourceNotFound,
		},
		{
			name: "source has no audio",
			mutate: func(req *models.JobRequest) {
				req.Options["sourcePath"] = silentSource
			},
			prepare: func() {
				fixture.sourceMeta.AudioCodec = ""
			},
			want: engine.ErrorCodeVideoAudioSyncSourceNoAudio,
		},
		{
			name:   "invalid original audio",
			mutate: func(req *models.JobRequest) { req.Options["originalAudio"] = "mute" },
			want:   engine.ErrorCodeVideoAudioSyncOriginalAudioInvalid,
		},
		{
			name:   "keep without an original stream",
			mutate: func(req *models.JobRequest) { req.Options["originalAudio"] = "keep" },
			prepare: func() {
				fixture.videoMeta.AudioCodec = ""
			},
			want: engine.ErrorCodeVideoAudioSyncOriginalAudioMissing,
		},
		{
			name:   "gain out of range",
			mutate: func(req *models.JobRequest) { req.Options["sourceGainDb"] = 30.0 },
			want:   engine.ErrorCodeVideoAudioSyncGainInvalid,
		},
		{
			name:   "bitrate out of range",
			mutate: func(req *models.JobRequest) { req.Options["audioBitrateKbps"] = 10.0 },
			want:   engine.ErrorCodeVideoAudioSyncBitrateInvalid,
		},
		{
			name:   "offset not finite",
			mutate: func(req *models.JobRequest) { req.Options["offsetSeconds"] = nanValue() },
			want:   engine.ErrorCodeVideoAudioSyncOffsetInvalid,
		},
		{
			name:   "offset after the video",
			mutate: func(req *models.JobRequest) { req.Options["offsetSeconds"] = 10.0 },
			want:   engine.ErrorCodeVideoAudioSyncOffsetInvalid,
		},
		{
			name:   "negative source start",
			mutate: func(req *models.JobRequest) { req.Options["sourceStartSeconds"] = -1.0 },
			want:   engine.ErrorCodeVideoAudioSyncSourceStartInvalid,
		},
		{
			name: "sections need segments",
			mutate: func(req *models.JobRequest) {
				req.Options = map[string]any{"mode": "sections"}
			},
			want: engine.ErrorCodeVideoAudioSyncSegmentsRequired,
		},
		{
			name: "segments above the cap",
			mutate: func(req *models.JobRequest) {
				segments := make([]any, 0, engine.MixMaxSegments+1)
				for index := 0; index <= engine.MixMaxSegments; index++ {
					segments = append(segments, map[string]any{"videoStart": 0.0, "videoEnd": 0.001, "sourcePath": fixture.source})
				}
				req.Options = map[string]any{"mode": "sections", "segments": segments}
			},
			want: engine.ErrorCodeVideoAudioSyncSegmentLimit,
		},
		{
			name: "segment requires videoStart",
			mutate: func(req *models.JobRequest) {
				req.Options = map[string]any{"mode": "sections", "segments": []any{
					map[string]any{"videoEnd": 2.0, "sourcePath": fixture.source},
				}}
			},
			want: engine.ErrorCodeVideoAudioSyncSegmentInvalid,
		},
		{
			name: "negative segment fade",
			mutate: func(req *models.JobRequest) {
				req.Options = map[string]any{"mode": "sections", "segments": []any{
					map[string]any{"videoStart": 1.0, "videoEnd": 2.0, "sourcePath": fixture.source, "fadeMs": -5.0},
				}}
			},
			want: engine.ErrorCodeVideoAudioSyncSegmentInvalid,
		},
		{
			name: "segment ends after the video",
			mutate: func(req *models.JobRequest) {
				req.Options = map[string]any{"mode": "sections", "segments": []any{
					map[string]any{"videoStart": 1.0, "videoEnd": 11.0, "sourcePath": fixture.source},
				}}
			},
			want: engine.ErrorCodeVideoAudioSyncSegmentInvalid,
		},
		{
			name: "segments overlap",
			mutate: func(req *models.JobRequest) {
				req.Options = map[string]any{"mode": "sections", "segments": []any{
					map[string]any{"videoStart": 1.0, "videoEnd": 5.0, "sourcePath": fixture.source},
					map[string]any{"videoStart": 4.0, "videoEnd": 6.0, "sourcePath": fixture.source},
				}}
			},
			want: engine.ErrorCodeVideoAudioSyncSegmentOverlap,
		},
		{
			name: "segment source has no audio",
			mutate: func(req *models.JobRequest) {
				req.Options = map[string]any{"mode": "sections", "segments": []any{
					map[string]any{"videoStart": 1.0, "videoEnd": 2.0, "sourcePath": silentSource},
				}}
			},
			prepare: func() {
				fixture.sourceMeta.AudioCodec = ""
			},
			want: engine.ErrorCodeVideoAudioSyncSourceNoAudio,
		},
		{
			name:   "explicit output exists",
			mutate: func(req *models.JobRequest) { req.Options["outputPath"] = existing },
			want:   engine.ErrorCodeVideoAudioSyncOutputExists,
		},
		{
			name:   "normalize must be a boolean",
			mutate: func(req *models.JobRequest) { req.Options["normalize"] = "maybe" },
			want:   engine.ErrorCodeVideoAudioSyncValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Restore the fixture probe before each case so prepare and mutate
			// start from the same baseline.
			probeVideoMetadata = originalProbe
			fixture.videoMeta = inspect.VideoMetadata{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac", FrameRate: 30}
			fixture.sourceMeta = inspect.VideoMetadata{DurationSeconds: 12, AudioCodec: "aac"}
			if tt.prepare != nil {
				tt.prepare()
			}

			request := videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, fixture.replaceOptions())
			tt.mutate(&request)

			_, planErr := newTool(t).Plan(context.Background(), request)
			if planErr == nil {
				t.Fatal("expected a plan error")
			}
			if planErr.Code != tt.want {
				t.Fatalf("code = %q, want %q (%s)", planErr.Code, tt.want, planErr.Message)
			}
		})
	}
}

// nanValue keeps the table readable without importing math.
func nanValue() float64 {
	var zero float64
	return zero / zero
}

func TestAudioSyncExecutePlanAcceptsJSONRoundTrippedSegments(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	fixture.tool = NewAudioSyncToolWithDeps(stubRuntimeProbe{}, &stubCommandRunner{})

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, map[string]any{
		"mode": "sections",
		"segments": []any{
			map[string]any{"videoStart": 1.0, "videoEnd": 3.0, "sourcePath": fixture.source},
		},
	}))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}

	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	var decoded models.PlanJobResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}

	items, execErr := fixture.tool.ExecutePlan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, nil), decoded, nil)
	if execErr != nil {
		t.Fatalf("execute round-tripped plan: %v", execErr)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("unexpected result: %+v", items)
	}
}

func TestAudioSyncExecutePlanReportsProgressAndMessage(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	runner := &stubCommandRunner{}
	fixture.tool = NewAudioSyncToolWithDeps(stubRuntimeProbe{}, runner)

	plan, planErr := fixture.tool.Plan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, fixture.replaceOptions()))
	if planErr != nil {
		t.Fatalf("plan: %v", planErr)
	}

	var progress []models.JobProgress
	items, execErr := fixture.tool.ExecutePlan(context.Background(), videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, nil), plan, func(jobProgress models.JobProgress) {
		progress = append(progress, jobProgress)
	})
	if execErr != nil {
		t.Fatalf("execute: %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("result items = %d, want 1", len(items))
	}
	if !items[0].Success || items[0].Message != "Audio sync successful" {
		t.Fatalf("unexpected result item: %+v", items[0])
	}
	if items[0].OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("result output = %q, want the planned %q", items[0].OutputPath, plan.Outputs[0].OutputPath)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); statErr != nil {
		t.Fatalf("expected the output file: %v", statErr)
	}

	if len(progress) == 0 {
		t.Fatal("expected progress updates")
	}
	for _, update := range progress {
		if update.Stage != "video.audio_sync" {
			t.Fatalf("progress stage = %q, want video.audio_sync", update.Stage)
		}
		if update.Total != 100 {
			t.Fatalf("progress total = %d, want 100", update.Total)
		}
	}
	if progress[len(progress)-1].Current != 100 {
		t.Fatalf("final progress = %+v, want 100", progress[len(progress)-1])
	}

	if len(runner.calls) != 1 {
		t.Fatalf("ffmpeg calls = %d, want 1", len(runner.calls))
	}
	if !hasArgSequence(runner.calls[0], "-c:v", "copy") {
		t.Fatalf("the video stream must be copied: %v", runner.calls[0])
	}
}

func TestAudioSyncExecutePlanRespectsCancellation(t *testing.T) {
	fixture := newAudioSyncFixture(t)
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{{
		InputPath:  fixture.video,
		OutputPath: filepath.Join(fixture.dir, "camera_synced.mp4"),
	}}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &stubCommandRunner{}
	fixture.tool = NewAudioSyncToolWithDeps(stubRuntimeProbe{}, runner)
	_, execErr := fixture.tool.ExecutePlan(ctx, videoToolRequest(ToolIDVideoAudioSync, "single", []string{fixture.video}, nil), plan, nil)
	if execErr == nil || execErr.Code != engine.ErrorCodeVideoAudioSyncCancelled {
		t.Fatalf("expected the cancelled code, got %v", execErr)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("a cancelled run must not start ffmpeg: %v", runner.calls)
	}
}
