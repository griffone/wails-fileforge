package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeMixFixtures creates the placeholder files ValidateMix only stats.
func writeMixFixtures(t *testing.T) (dir, video, source, output string) {
	t.Helper()

	dir = t.TempDir()
	video = filepath.Join(dir, "camera.mp4")
	source = filepath.Join(dir, "recorder.m4a")
	output = filepath.Join(dir, "camera_synced.mp4")
	for _, path := range []string{video, source} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	return dir, video, source, output
}

func mixArgsOf(prefix []string, graph, outputPath string, bitrate string) []string {
	args := append([]string{}, prefix...)
	args = append(args,
		"-filter_complex", graph,
		"-map", "0:v",
		"-map", "[a]",
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", bitrate,
		"-ar", "48000",
		"-fps_mode", "passthrough",
		"-movflags", "+faststart",
		outputPath,
	)

	return args
}

var mixGlobalArgs = []string{"-hide_banner", "-loglevel", "error", "-nostats", "-nostdin"}

func TestBuildMixArgsReplacePositiveOffsetDelaysSource(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	args, argsErr := BuildMixArgs(MixRequest{
		VideoPath:          video,
		OutputPath:         output,
		Mode:               MixModeReplace,
		SourcePath:         source,
		OffsetSeconds:      2,
		SourceStartSeconds: 0.5,
		OriginalAudio:      MixOriginalRemove,
		AudioBitrateKbps:   0,
		DurationSeconds:    10,
	})
	if argsErr != nil {
		t.Fatalf("build args: %v", argsErr)
	}

	graph := "[1:a]atrim=start=0.5:end=8.5,asetpts=PTS-STARTPTS,aresample=48000,aformat=channel_layouts=stereo,volume=0dB[g0];[g0]adelay=2000|2000[d0];[d0]amix=inputs=1:normalize=0:duration=longest[mixed];[mixed]apad,atrim=0:10,aresample=async=1:first_pts=0[a]"
	want := mixArgsOf(
		append(append([]string{}, mixGlobalArgs...), "-y", "-i", video, "-i", source),
		graph,
		output,
		"256k",
	)

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, want)
	}
}

func TestBuildMixArgsReplaceNegativeOffsetTrimsSourceHead(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	args, argsErr := BuildMixArgs(MixRequest{
		VideoPath:          video,
		OutputPath:         output,
		Mode:               MixModeReplace,
		SourcePath:         source,
		OffsetSeconds:      -40,
		SourceStartSeconds: 0.3,
		OriginalAudio:      MixOriginalRemove,
		AudioBitrateKbps:   192,
		DurationSeconds:    10,
	})
	if argsErr != nil {
		t.Fatalf("build args: %v", argsErr)
	}

	graph := "[1:a]atrim=start=40.3:end=50.3,asetpts=PTS-STARTPTS,aresample=48000,aformat=channel_layouts=stereo,volume=0dB[g0];[g0]amix=inputs=1:normalize=0:duration=longest[mixed];[mixed]apad,atrim=0:10,aresample=async=1:first_pts=0[a]"
	want := mixArgsOf(
		append(append([]string{}, mixGlobalArgs...), "-y", "-i", video, "-i", source),
		graph,
		output,
		"192k",
	)

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, want)
	}
	for _, arg := range args {
		if strings.Contains(arg, "adelay") {
			t.Fatalf("a negative offset must not adelay the source, got %q", arg)
		}
	}
}

func TestBuildMixArgsSectionsShareOneSourceThroughAsplit(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	args, argsErr := BuildMixArgs(MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeSections,
		OriginalAudio:    MixOriginalRemove,
		AudioBitrateKbps: 256,
		DurationSeconds:  10,
		Segments: []MixSegment{
			{VideoStart: 2, VideoEnd: 4, SourceStart: 1, SourcePath: source},
			{VideoStart: 5, VideoEnd: 7, SourceStart: 3, SourcePath: source},
		},
	})
	if argsErr != nil {
		t.Fatalf("build args: %v", argsErr)
	}

	graph := "[1:a]asplit=2[sp1_0][sp1_1];" +
		"[sp1_0]atrim=start=1:end=3,asetpts=PTS-STARTPTS,aresample=48000,aformat=channel_layouts=stereo,volume=0dB[g0];[g0]adelay=2000|2000[d0];" +
		"[sp1_1]atrim=start=3:end=5,asetpts=PTS-STARTPTS,aresample=48000,aformat=channel_layouts=stereo,volume=0dB[g1];[g1]adelay=5000|5000[d1];" +
		"[d0][d1]amix=inputs=2:normalize=0:duration=longest[mixed];[mixed]apad,atrim=0:10,aresample=async=1:first_pts=0[a]"
	want := mixArgsOf(
		append(append([]string{}, mixGlobalArgs...), "-y", "-i", video, "-i", source),
		graph,
		output,
		"256k",
	)

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\n got: %#v\nwant: %#v", args, want)
	}
	graph = args[indexOfMixArg(args, "-filter_complex")+1]
	if strings.Count(graph, "[1:a]") != 1 {
		t.Fatalf("the shared source must be declared as one input, got %q", graph)
	}
}

func TestBuildMixArgsOriginalAudioPolicies(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)
	tests := []struct {
		name     string
		policy   string
		duckGain float64
		wantOrig string
	}{
		{name: "remove", policy: MixOriginalRemove},
		{name: "keep", policy: MixOriginalKeep, wantOrig: "[0:a]aresample=48000,aformat=channel_layouts=stereo,volume=0dB[orig]"},
		{name: "duck", policy: MixOriginalDuck, duckGain: -18, wantOrig: "[0:a]aresample=48000,aformat=channel_layouts=stereo,volume=-18dB[orig]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, argsErr := BuildMixArgs(MixRequest{
				VideoPath:        video,
				OutputPath:       output,
				Mode:             MixModeReplace,
				SourcePath:       source,
				OriginalAudio:    tt.policy,
				OriginalGainDB:   tt.duckGain,
				AudioBitrateKbps: 256,
				DurationSeconds:  3,
			})
			if argsErr != nil {
				t.Fatalf("build args: %v", argsErr)
			}

			graph := args[indexOfMixArg(args, "-filter_complex")+1]
			if tt.wantOrig == "" {
				if strings.Contains(graph, "[orig]") {
					t.Fatalf("remove must not mix the original audio, got %q", graph)
				}
				return
			}
			if !strings.Contains(graph, tt.wantOrig) {
				t.Fatalf("expected %q in graph, got %q", tt.wantOrig, graph)
			}
			if !strings.Contains(graph, "amix=inputs=2:normalize=0:duration=longest[mixed]") {
				t.Fatalf("keep/duck must add the original as an amix input, got %q", graph)
			}
		})
	}
}

func TestBuildMixArgsFades(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	args, argsErr := BuildMixArgs(MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeSections,
		OriginalAudio:    MixOriginalRemove,
		AudioBitrateKbps: 256,
		DurationSeconds:  10,
		Segments: []MixSegment{
			{VideoStart: 0, VideoEnd: 2, SourcePath: source, FadeSeconds: 0.5},
		},
	})
	if argsErr != nil {
		t.Fatalf("build args: %v", argsErr)
	}

	graph := args[indexOfMixArg(args, "-filter_complex")+1]
	want := "atrim=start=0:end=2,asetpts=PTS-STARTPTS,aresample=48000,aformat=channel_layouts=stereo,volume=0dB,afade=t=in:st=0:d=0.5,afade=t=out:st=1.5:d=0.5[g0]"
	if !strings.Contains(graph, want) {
		t.Fatalf("expected %q in graph, got %q", want, graph)
	}
}

func TestMixFadeSecondsClampsToHalfDuration(t *testing.T) {
	segment := MixSegment{FadeSeconds: 5}
	if got := mixFadeSeconds(segment, 2); got != 1 {
		t.Fatalf("fade = %v, want 1 (half of 2 s)", got)
	}
	if got := mixFadeSeconds(MixSegment{FadeSeconds: 0}, 2); got != 0 {
		t.Fatalf("an explicit zero fade must stay disabled, got %v", got)
	}
}

func TestBuildMixArgsBitrateAndValidation(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	base := MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 10,
	}

	t.Run("default bitrate", func(t *testing.T) {
		args, argsErr := BuildMixArgs(base)
		if argsErr != nil {
			t.Fatalf("build args: %v", argsErr)
		}
		if !hasMixArgSequence(args, "-b:a", "256k") {
			t.Fatalf("expected the 256k default, got %v", args)
		}
	})

	t.Run("explicit bitrate", func(t *testing.T) {
		request := base
		request.AudioBitrateKbps = 320
		args, argsErr := BuildMixArgs(request)
		if argsErr != nil {
			t.Fatalf("build args: %v", argsErr)
		}
		if !hasMixArgSequence(args, "-b:a", "320k") {
			t.Fatalf("expected 320k, got %v", args)
		}
	})

	t.Run("out of range", func(t *testing.T) {
		request := base
		request.AudioBitrateKbps = 10
		if _, argsErr := BuildMixArgs(request); argsErr == nil || argsErr.Code != ErrorCodeVideoAudioSyncBitrateInvalid {
			t.Fatalf("expected the bitrate code, got %v", argsErr)
		}
	})
}

func hasMixArgSequence(args []string, want ...string) bool {
	for index := 0; index+len(want) <= len(args); index++ {
		matched := true
		for offset, value := range want {
			if args[index+offset] != value {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func TestValidateMixRejectsInvalidRequests(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	base := MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 10,
	}

	existing := filepath.Join(filepath.Dir(output), "existing.mp4")
	if err := os.WriteFile(existing, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	segments := func(mutate func([]MixSegment) []MixSegment) []MixSegment {
		return mutate([]MixSegment{
			{VideoStart: 1, VideoEnd: 3, SourcePath: source},
		})
	}

	tests := []struct {
		name     string
		mutate   func(*MixRequest)
		wantCode string
	}{
		{name: "missing video path", mutate: func(req *MixRequest) { req.VideoPath = "" }, wantCode: ErrorCodeVideoAudioSyncValidation},
		{name: "missing video file", mutate: func(req *MixRequest) { req.VideoPath = filepath.Join(t.TempDir(), "nope.mp4") }, wantCode: ErrorCodeVideoAudioSyncValidation},
		{name: "missing output", mutate: func(req *MixRequest) { req.OutputPath = "" }, wantCode: ErrorCodeVideoAudioSyncValidation},
		{name: "zero duration", mutate: func(req *MixRequest) { req.DurationSeconds = 0 }, wantCode: ErrorCodeVideoAudioSyncDurationUnavailable},
		{name: "nan duration", mutate: func(req *MixRequest) { req.DurationSeconds = mathNaN() }, wantCode: ErrorCodeVideoAudioSyncDurationUnavailable},
		{name: "invalid mode", mutate: func(req *MixRequest) { req.Mode = "guess" }, wantCode: ErrorCodeVideoAudioSyncModeInvalid},
		{name: "invalid original audio", mutate: func(req *MixRequest) { req.OriginalAudio = "mute" }, wantCode: ErrorCodeVideoAudioSyncOriginalAudioInvalid},
		{name: "gain out of range", mutate: func(req *MixRequest) { req.SourceGainDB = 25 }, wantCode: ErrorCodeVideoAudioSyncGainInvalid},
		{name: "original gain out of range", mutate: func(req *MixRequest) { req.OriginalGainDB = -61 }, wantCode: ErrorCodeVideoAudioSyncGainInvalid},
		{name: "bitrate out of range", mutate: func(req *MixRequest) { req.AudioBitrateKbps = 600 }, wantCode: ErrorCodeVideoAudioSyncBitrateInvalid},
		{name: "missing source", mutate: func(req *MixRequest) { req.SourcePath = "" }, wantCode: ErrorCodeVideoAudioSyncValidation},
		{name: "source not found", mutate: func(req *MixRequest) { req.SourcePath = filepath.Join(t.TempDir(), "nope.m4a") }, wantCode: ErrorCodeVideoAudioSyncSourceNotFound},
		{name: "nan offset", mutate: func(req *MixRequest) { req.OffsetSeconds = mathNaN() }, wantCode: ErrorCodeVideoAudioSyncOffsetInvalid},
		{name: "offset after the end", mutate: func(req *MixRequest) { req.OffsetSeconds = 10 }, wantCode: ErrorCodeVideoAudioSyncOffsetInvalid},
		{name: "negative source start", mutate: func(req *MixRequest) { req.SourceStartSeconds = -1 }, wantCode: ErrorCodeVideoAudioSyncSourceStartInvalid},
		{name: "output collides with the video", mutate: func(req *MixRequest) { req.OutputPath = video }, wantCode: ErrorCodeVideoAudioSyncOutputCollides},
		{name: "output collides with a source", mutate: func(req *MixRequest) { req.OutputPath = source }, wantCode: ErrorCodeVideoAudioSyncOutputCollides},
		{name: "output exists", mutate: func(req *MixRequest) { req.OutputPath = existing }, wantCode: ErrorCodeVideoAudioSyncOutputExists},
		{
			name:     "sections without segments",
			mutate:   func(req *MixRequest) { req.Mode = MixModeSections; req.Segments = nil },
			wantCode: ErrorCodeVideoAudioSyncSegmentsRequired,
		},
		{
			name: "sections above the cap",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = make([]MixSegment, 0, MixMaxSegments+1)
				for index := 0; index <= MixMaxSegments; index++ {
					req.Segments = append(req.Segments, MixSegment{VideoStart: 0, VideoEnd: 0.001, SourcePath: source})
				}
			},
			wantCode: ErrorCodeVideoAudioSyncSegmentLimit,
		},
		{
			name: "negative segment start",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = segments(func(s []MixSegment) []MixSegment { s[0].VideoStart = -1; return s })
			},
			wantCode: ErrorCodeVideoAudioSyncSegmentInvalid,
		},
		{
			name: "segment ends before it starts",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = segments(func(s []MixSegment) []MixSegment { s[0].VideoEnd = 1; return s })
			},
			wantCode: ErrorCodeVideoAudioSyncSegmentInvalid,
		},
		{
			name: "segment ends after the video",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = segments(func(s []MixSegment) []MixSegment { s[0].VideoEnd = 11; return s })
			},
			wantCode: ErrorCodeVideoAudioSyncSegmentInvalid,
		},
		{
			name: "segment source not found",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = segments(func(s []MixSegment) []MixSegment { s[0].SourcePath = filepath.Join(t.TempDir(), "nope.m4a"); return s })
			},
			wantCode: ErrorCodeVideoAudioSyncSourceNotFound,
		},
		{
			name: "segment gain out of range",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = segments(func(s []MixSegment) []MixSegment { s[0].GainDB = -61; return s })
			},
			wantCode: ErrorCodeVideoAudioSyncGainInvalid,
		},
		{
			name: "overlapping segments",
			mutate: func(req *MixRequest) {
				req.Mode = MixModeSections
				req.Segments = []MixSegment{
					{VideoStart: 1, VideoEnd: 5, SourcePath: source},
					{VideoStart: 4, VideoEnd: 6, SourcePath: source},
				}
			},
			wantCode: ErrorCodeVideoAudioSyncSegmentOverlap,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := base
			tt.mutate(&request)

			validationErr := ValidateMix(request)
			if validationErr == nil {
				t.Fatal("expected a validation error")
			}
			if validationErr.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q (%s)", validationErr.Code, tt.wantCode, validationErr.Message)
			}
		})
	}
}

// mathNaN keeps the test table readable without importing math in every case.
func mathNaN() float64 {
	var zero float64
	return zero / zero
}

func TestValidateMixSegmentErrorsNameTheIndex(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	validationErr := ValidateMix(MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeSections,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 10,
		Segments: []MixSegment{
			{VideoStart: 1, VideoEnd: 3, SourcePath: source},
			{VideoStart: 4, VideoEnd: 12, SourcePath: source},
		},
	})
	if validationErr == nil || validationErr.Code != ErrorCodeVideoAudioSyncSegmentInvalid {
		t.Fatalf("expected a segment error, got %v", validationErr)
	}
	if !strings.Contains(validationErr.Message, "segment 2") || !strings.Contains(validationErr.Message, "12.000") {
		t.Fatalf("the message must name the segment and its time, got %q", validationErr.Message)
	}
}

func TestValidateMixOverlapNamesBothSegments(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	validationErr := ValidateMix(MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeSections,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 10,
		Segments: []MixSegment{
			{VideoStart: 1, VideoEnd: 5, SourcePath: source},
			{VideoStart: 4, VideoEnd: 6, SourcePath: source},
		},
	})
	if validationErr == nil || validationErr.Code != ErrorCodeVideoAudioSyncSegmentOverlap {
		t.Fatalf("expected an overlap error, got %v", validationErr)
	}
	for _, want := range []string{"segments 1 and 2", "overlap"} {
		if !strings.Contains(validationErr.Message, want) {
			t.Fatalf("expected %q in %q", want, validationErr.Message)
		}
	}
}

func TestExecuteMixPublishesAtomically(t *testing.T) {
	dir, video, source, output := writeMixFixtures(t)

	runner := &recordingRunner{}
	mixErr := ExecuteMix(context.Background(), stubMergeRuntimeProbe{}, runner, MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 4,
	}, nil)
	if mixErr != nil {
		t.Fatalf("execute mix: %v", mixErr)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("ffmpeg calls = %d, want 1", len(runner.calls))
	}
	tempPath := runner.calls[0][len(runner.calls[0])-1]
	if tempPath == output {
		t.Fatal("ffmpeg must write to a temp sibling")
	}
	if filepath.Dir(tempPath) != dir {
		t.Fatalf("temp output %q must live next to %q", tempPath, output)
	}
	if _, statErr := os.Stat(output); statErr != nil {
		t.Fatalf("expected the published output: %v", statErr)
	}
	assertNoVideoTempOutputs(t, dir)
}

func TestExecuteMixRemovesTheTempFileOnFailure(t *testing.T) {
	dir, video, source, output := writeMixFixtures(t)

	runner := &recordingRunner{run: func(context.Context, string, []string) error {
		return errors.New("ffmpeg exploded")
	}}
	mixErr := ExecuteMix(context.Background(), stubMergeRuntimeProbe{}, runner, MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 4,
	}, nil)
	if mixErr == nil || mixErr.Code != ErrorCodeVideoAudioSyncExecutionFailed {
		t.Fatalf("expected the execution code, got %v", mixErr)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed run must not publish an output: %v", statErr)
	}
	assertNoVideoTempOutputs(t, dir)
}

func TestExecuteMixMapsCancellation(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	runner := &recordingRunner{run: func(context.Context, string, []string) error {
		return context.Canceled
	}}
	mixErr := ExecuteMix(context.Background(), stubMergeRuntimeProbe{}, runner, MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 4,
	}, nil)
	if mixErr == nil || mixErr.Code != ErrorCodeVideoAudioSyncCancelled {
		t.Fatalf("expected the cancelled code, got %v", mixErr)
	}
}

// mixCaptureRunner implements both runner shapes and returns a loudnorm report
// from the measurement pass.
type mixCaptureRunner struct {
	args    [][]string
	reports []string
}

func (r *mixCaptureRunner) Run(_ context.Context, _ string, args []string) error {
	r.args = append(r.args, args)
	return writeRunnerOutput(args)
}

func (r *mixCaptureRunner) RunCapture(_ context.Context, _ string, args []string, _ float64, _ func(float64, string)) (string, string, error) {
	r.args = append(r.args, args)
	if len(r.args) == 1 {
		return "", r.reports[0], nil
	}

	return "", r.reports[0], writeRunnerOutput(args)
}

func TestExecuteMixNormalizeRunsTwoPasses(t *testing.T) {
	dir, video, source, output := writeMixFixtures(t)

	runner := &mixCaptureRunner{reports: []string{
		`[Parsed_loudnorm_0 @ 0x1] ` + "\n" + `{
			"input_i" : "-12.30",
			"input_tp" : "-1.00",
			"input_lra" : "3.20",
			"input_thresh" : "-22.50",
			"output_i" : "-14.00",
			"target_offset" : "0.10"
		}`,
	}}

	mixErr := ExecuteMix(context.Background(), stubMergeRuntimeProbe{}, runner, MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 4,
		Normalize:       true,
	}, nil)
	if mixErr != nil {
		t.Fatalf("normalized mix: %v", mixErr)
	}
	if len(runner.args) != 2 {
		t.Fatalf("normalization calls = %d, want 2 (measure + apply)", len(runner.args))
	}
	if !hasMixArgSequence(runner.args[0], "-loglevel", "info") {
		t.Fatalf("the measurement pass must log at info level: %v", runner.args[0])
	}
	measureGraph := runner.args[0][indexOfMixArg(runner.args[0], "-filter_complex")+1]
	if !strings.Contains(measureGraph, "loudnorm=I=-14.0:TP=-1.5:LRA=11:print_format=json") {
		t.Fatalf("expected the measurement filter, got %q", measureGraph)
	}
	if !hasMixArgSequence(runner.args[0], "-f", "null", "-") {
		t.Fatalf("the measurement pass must render to null: %v", runner.args[0])
	}
	applyGraph := runner.args[1][indexOfMixArg(runner.args[1], "-filter_complex")+1]
	if !strings.Contains(applyGraph, "measured_I=-12.30") || !strings.Contains(applyGraph, "linear=true") {
		t.Fatalf("expected the measured values in the apply graph, got %q", applyGraph)
	}
	if hasMixArgSequence(runner.args[1], "-f", "null") {
		t.Fatalf("the apply pass must encode a real file: %v", runner.args[1])
	}
	if _, statErr := os.Stat(output); statErr != nil {
		t.Fatalf("expected the published output: %v", statErr)
	}
	assertNoVideoTempOutputs(t, dir)
}

func TestExecuteMixNormalizeRequiresCaptureRunner(t *testing.T) {
	_, video, source, output := writeMixFixtures(t)

	mixErr := ExecuteMix(context.Background(), stubMergeRuntimeProbe{}, &recordingRunner{}, MixRequest{
		VideoPath:       video,
		OutputPath:      output,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 4,
		Normalize:       true,
	}, nil)
	if mixErr == nil || mixErr.Code != ErrorCodeVideoAudioSyncLoudnormFailed {
		t.Fatalf("expected the loudnorm code, got %v", mixErr)
	}
}

func TestRenderPreviewReencodesAShortSpan(t *testing.T) {
	dir, video, source, _ := writeMixFixtures(t)
	preview := filepath.Join(dir, "preview.mp4")

	runner := &recordingRunner{}
	previewErr := RenderPreview(context.Background(), stubMergeRuntimeProbe{}, runner, MixRequest{
		VideoPath:       video,
		Mode:            MixModeReplace,
		SourcePath:      source,
		OriginalAudio:   MixOriginalRemove,
		DurationSeconds: 10,
	}, 2, 3, preview)
	if previewErr != nil {
		t.Fatalf("render preview: %v", previewErr)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("ffmpeg calls = %d, want 1", len(runner.calls))
	}

	args := runner.calls[0]
	graph := args[indexOfMixArg(args, "-filter_complex")+1]
	for _, want := range []string{
		"[0:v]trim=start=2:end=5,setpts=PTS-STARTPTS,scale=640:-2[pv]",
		"[mixed]atrim=start=2:end=5,asetpts=PTS-STARTPTS,aresample=async=1:first_pts=0[pa]",
	} {
		if !strings.Contains(graph, want) {
			t.Fatalf("expected %q in the preview graph, got %q", want, graph)
		}
	}
	if !hasMixArgSequence(args, "-map", "[pv]") || !hasMixArgSequence(args, "-map", "[pa]") {
		t.Fatalf("the preview must map both trimmed streams: %v", args)
	}
	for _, want := range [][]string{
		{"-c:v", "libx264"},
		{"-preset", "ultrafast"},
		{"-b:a", "128k"},
	} {
		if !hasMixArgSequence(args, want...) {
			t.Fatalf("expected %v in %v", want, args)
		}
	}
	if _, statErr := os.Stat(preview); statErr != nil {
		t.Fatalf("expected the published preview: %v", statErr)
	}
	assertNoVideoTempOutputs(t, dir)
}

func indexOfMixArg(args []string, want string) int {
	for index, arg := range args {
		if arg == want {
			return index
		}
	}

	return -1
}

func TestMixOutputExtension(t *testing.T) {
	tests := []struct {
		codec string
		want  string
		mp4   bool
	}{
		{codec: "h264", want: "mp4", mp4: true},
		{codec: "hevc", want: "mp4", mp4: true},
		{codec: "av1", want: "mp4", mp4: true},
		{codec: "vp9", want: "mp4", mp4: true},
		{codec: "mpeg4", want: "mkv"},
		{codec: "prores", want: "mkv"},
	}

	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			container, mp4 := MixOutputExtension(tt.codec)
			if container != tt.want || mp4 != tt.mp4 {
				t.Fatalf("MixOutputExtension(%q) = %q/%v, want %q/%v", tt.codec, container, mp4, tt.want, tt.mp4)
			}
		})
	}
}
