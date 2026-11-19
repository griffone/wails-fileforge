package engine

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBurnSRTContent = "1\n00:00:00,200 --> 00:00:01,200\nHola mundo\n\n2\n00:00:01,300 --> 00:00:01,900\nSecond cue\n"

// burnRunnerCall records one fake invocation so tests can assert the argv and
// working directory contract.
type burnRunnerCall struct {
	dir  string
	name string
	args []string
}

// fakeBurnRunner stands in for ffmpeg. It can stream stdout lines, write the
// temp output file parsed from the last argument, and fail with stderr text.
// Subtitle bytes are captured while the private workspace still exists because
// Burn removes it before returning.
type fakeBurnRunner struct {
	stdout       []string
	stderr       string
	err          error
	createOutput bool
	outputBytes  []byte
	onRun        func(ctx context.Context, call burnRunnerCall)

	calls           []burnRunnerCall
	subtitleBytes   []byte
	subtitleMode    os.FileMode
	subtitleReadErr error
}

func (r *fakeBurnRunner) RunBurn(ctx context.Context, dir, name string, args []string, onStdout func(string)) (string, error) {
	call := burnRunnerCall{dir: dir, name: name, args: append([]string{}, args...)}
	r.calls = append(r.calls, call)

	subtitlePath := filepath.Join(dir, burnSubtitleName)
	r.subtitleBytes, r.subtitleReadErr = os.ReadFile(subtitlePath)
	if info, statErr := os.Stat(subtitlePath); statErr == nil {
		r.subtitleMode = info.Mode().Perm()
	}

	if r.onRun != nil {
		r.onRun(ctx, call)
	}
	for _, line := range r.stdout {
		if onStdout != nil {
			onStdout(line)
		}
	}
	if r.createOutput && len(args) > 0 {
		if writeErr := os.WriteFile(args[len(args)-1], r.outputBytes, 0o600); writeErr != nil {
			return r.stderr, writeErr
		}
	}
	if r.err != nil {
		return r.stderr, r.err
	}

	return r.stderr, nil
}

func (r *fakeBurnRunner) lastCall(t *testing.T) burnRunnerCall {
	t.Helper()

	if len(r.calls) == 0 {
		t.Fatal("expected the burn runner to be called")
	}

	return r.calls[len(r.calls)-1]
}

func writeBurnTestFile(t *testing.T, path string, content []byte) {
	t.Helper()

	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// burnTestRequest builds a valid-looking request around a fake input file.
// ProbeVideoDuration cannot read it, so the engine treats the duration as
// unknown and only the start and done events are emitted.
func burnTestRequest(t *testing.T) BurnRequest {
	t.Helper()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.mp4")
	subtitlePath := filepath.Join(dir, "captions.srt")
	writeBurnTestFile(t, inputPath, []byte("fake input"))
	writeBurnTestFile(t, subtitlePath, []byte(testBurnSRTContent))

	return BurnRequest{
		InputPath:    inputPath,
		SubtitlePath: subtitlePath,
		OutputPath:   filepath.Join(dir, "output.mp4"),
		AudioCodec:   "aac",
	}
}

func burnArgIndex(args []string, target string) int {
	for index, arg := range args {
		if arg == target {
			return index
		}
	}

	return -1
}

func assertNoBurnTempOutputs(t *testing.T, dir string) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, ".fileforge-burn-*"))
	if err != nil {
		t.Fatalf("glob temp outputs: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary burn outputs were left behind: %v", matches)
	}
}

func TestBuildBurnArgsUsesDefaultStyleAndCopiesCompatibleAudio(t *testing.T) {
	req := BurnRequest{
		InputPath:  "input.mp4",
		OutputPath: "output.mp4",
		AudioCodec: "aac",
	}

	args := BuildBurnArgs(req, burnSubtitleName)

	defaults := DefaultBurnStyle()
	wantFilter := fmt.Sprintf(
		"subtitles=subs.srt:force_style='FontName=%s,FontSize=18,PrimaryColour=&H00FFFFFF,OutlineColour=&H00000000,BorderStyle=1,Outline=2,Shadow=1,Alignment=2,MarginV=20'",
		defaults.FontName,
	)
	if got := argValueAfter(args, "-vf"); got != wantFilter {
		t.Fatalf("unexpected filter:\n got: %s\nwant: %s", got, wantFilter)
	}
	if got := argValueAfter(args, "-i"); got != "input.mp4" {
		t.Fatalf("expected the relative input, got %q", got)
	}
	if args[len(args)-1] != "output.mp4" {
		t.Fatalf("expected the output as the last argument, got %q", args[len(args)-1])
	}

	sequences := [][]string{
		{"-y"},
		{"-hide_banner"},
		{"-loglevel", "warning"},
		{"-i", "input.mp4"},
		{"-map", "0:v:0"},
		{"-map", "0:a?"},
		{"-c:v", "libx264"},
		{"-preset", "veryfast"},
		{"-crf", "20"},
		{"-pix_fmt", "yuv420p"},
		{"-c:a", "copy"},
		{"-map_metadata", "0"},
		{"-map_chapters", "0"},
		{"-movflags", "+faststart"},
		{"-progress", "pipe:1"},
		{"-nostats"},
		{"output.mp4"},
	}
	for _, sequence := range sequences {
		if !containsArgSequence(args, sequence...) {
			t.Errorf("missing argument sequence %q in %q", sequence, args)
		}
	}

	pixFmtIndex := burnArgIndex(args, "-pix_fmt")
	audioIndex := burnArgIndex(args, "-c:a")
	metadataIndex := burnArgIndex(args, "-map_metadata")
	if pixFmtIndex < 0 || audioIndex < 0 || metadataIndex < 0 {
		t.Fatalf("expected the encode, audio and metadata flags in %q", args)
	}
	if pixFmtIndex > audioIndex || audioIndex > metadataIndex {
		t.Fatalf("audio args must sit between the encode and metadata args, got index %d/%d/%d", pixFmtIndex, audioIndex, metadataIndex)
	}
}

func TestBuildBurnArgsChoosesAudioCodecFromCompatibility(t *testing.T) {
	cases := []struct {
		name     string
		codec    string
		wantCopy bool
	}{
		{name: "aac is copied", codec: "aac", wantCopy: true},
		{name: "mixed case mp3 is copied", codec: " MP3 ", wantCopy: true},
		{name: "opus is copied", codec: "opus", wantCopy: true},
		{name: "ac3 is copied", codec: "ac3", wantCopy: true},
		{name: "pcm is re-encoded to aac", codec: "pcm_s16le"},
		{name: "empty codec is re-encoded to aac", codec: ""},
		{name: "unknown codec is re-encoded to aac", codec: "vorbis"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := BuildBurnArgs(BurnRequest{InputPath: "input.mp4", OutputPath: "output.mp4", AudioCodec: tc.codec}, burnSubtitleName)

			if got := containsArgSequence(args, "-c:a", "copy"); got != tc.wantCopy {
				t.Fatalf("expected audio copy=%v for codec %q, args: %q", tc.wantCopy, tc.codec, args)
			}
			if !tc.wantCopy && !containsArgSequence(args, "-c:a", "aac", "-b:a", "192k") {
				t.Fatalf("expected an aac fallback for codec %q, args: %q", tc.codec, args)
			}
		})
	}
}

func TestBuildBurnArgsSanitizesFontNameAndKeepsExplicitStyle(t *testing.T) {
	args := BuildBurnArgs(BurnRequest{
		InputPath:  "input.mp4",
		OutputPath: "output.mp4",
		Style: BurnStyle{
			FontName: "Bad,Font'Name",
			FontSize: 24,
			MarginV:  30,
			Outline:  3,
			Shadow:   2,
		},
	}, burnSubtitleName)

	wantFilter := "subtitles=subs.srt:force_style='FontName=BadFontName,FontSize=24,PrimaryColour=&H00FFFFFF,OutlineColour=&H00000000,BorderStyle=1,Outline=3,Shadow=2,Alignment=2,MarginV=30'"
	if got := argValueAfter(args, "-vf"); got != wantFilter {
		t.Fatalf("unexpected filter:\n got: %s\nwant: %s", got, wantFilter)
	}
}

func TestBurnKeepsSubtitleInPrivateWorkingDirectory(t *testing.T) {
	req := burnTestRequest(t)
	runner := &fakeBurnRunner{createOutput: true, outputBytes: []byte("burned")}

	if _, err := Burn(context.Background(), runner, "ffmpeg", req, nil); err != nil {
		t.Fatalf("burn failed: %v", err)
	}

	call := runner.lastCall(t)
	if call.name != "ffmpeg" {
		t.Fatalf("expected ffmpeg to run, got %q", call.name)
	}
	if call.dir == filepath.Dir(req.OutputPath) {
		t.Fatalf("the runner must not work in the output directory: %s", call.dir)
	}
	if !strings.Contains(filepath.Base(call.dir), "fileforge-burn-") {
		t.Fatalf("expected a private burn workspace, got %q", call.dir)
	}
	if !strings.HasPrefix(call.dir, filepath.Clean(os.TempDir())) {
		t.Fatalf("expected the workspace under the system temp directory, got %q", call.dir)
	}
	if runner.subtitleReadErr != nil {
		t.Fatalf("expected subs.srt inside the workspace: %v", runner.subtitleReadErr)
	}
	if string(runner.subtitleBytes) != testBurnSRTContent {
		t.Fatalf("unexpected subtitle bytes in the workspace: %q", runner.subtitleBytes)
	}
	if runner.subtitleMode != 0o644 {
		t.Fatalf("expected the workspace subtitle to be 0644, got %o", runner.subtitleMode)
	}
	if _, statErr := os.Stat(call.dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the workspace to be removed after the run, stat err: %v", statErr)
	}

	filter := argValueAfter(call.args, "-vf")
	if !strings.Contains(filter, "subtitles=subs.srt:") {
		t.Fatalf("expected the relative subtitle name in the filter, got %q", filter)
	}
	if strings.Contains(filter, call.dir) || strings.Contains(filter, filepath.Dir(req.OutputPath)) {
		t.Fatalf("the filter must not contain an absolute path: %q", filter)
	}
	if strings.ContainsAny(filter, `/\`) {
		t.Fatalf("the filter must not contain path separators or escapes: %q", filter)
	}
	for _, arg := range call.args {
		if arg == req.SubtitlePath {
			t.Fatalf("the absolute subtitle path leaked into the argv: %q", call.args)
		}
	}
}

func TestParseBurnProgress(t *testing.T) {
	cases := []struct {
		name         string
		line         string
		microseconds int64
		done         bool
		ok           bool
	}{
		{name: "microseconds field", line: "out_time_us=1500000", microseconds: 1500000, ok: true},
		{name: "milliseconds field carries microseconds", line: "out_time_ms=2000000", microseconds: 2000000, ok: true},
		{name: "zero position", line: "out_time_us=0", microseconds: 0, ok: true},
		{name: "padded value", line: "out_time_us=  250000", microseconds: 250000, ok: true},
		{name: "clock field", line: "out_time=00:00:01.500000", microseconds: 1500000, ok: true},
		{name: "clock without fraction", line: "out_time=00:00:02", microseconds: 2000000, ok: true},
		{name: "short fraction", line: "out_time=00:00:02.5", microseconds: 2500000, ok: true},
		{name: "hours may exceed a day", line: "out_time=100:02:03.000004", microseconds: 360123000004, ok: true},
		{name: "end marker", line: "progress=end", done: true, ok: true},
		{name: "continue marker", line: "progress=continue"},
		{name: "unrelated frame line", line: "frame=25"},
		{name: "bitrate line with spaces", line: "bitrate= 148.9kbits/s"},
		{name: "missing separator", line: "out_time_us"},
		{name: "non numeric value", line: "out_time_us=abc"},
		{name: "negative value", line: "out_time_us=-1"},
		{name: "unavailable clock", line: "out_time=N/A"},
		{name: "invalid minutes", line: "out_time=00:61:00.000000"},
		{name: "invalid seconds", line: "out_time=00:00:61.000000"},
		{name: "too many fraction digits", line: "out_time=00:00:01.1234567"},
		{name: "empty line", line: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			microseconds, done, ok := parseBurnProgress(tc.line)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v for %q", ok, tc.ok, tc.line)
			}
			if microseconds != tc.microseconds {
				t.Fatalf("microseconds = %d, want %d for %q", microseconds, tc.microseconds, tc.line)
			}
			if done != tc.done {
				t.Fatalf("done = %v, want %v for %q", done, tc.done, tc.line)
			}
		})
	}
}

func TestBurnPercentClampsRenderRange(t *testing.T) {
	cases := []struct {
		name         string
		microseconds int64
		duration     float64
		want         int
	}{
		{name: "start of render", microseconds: 0, duration: 2, want: 5},
		{name: "negative position", microseconds: -1, duration: 2, want: 5},
		{name: "quarter", microseconds: 500000, duration: 2, want: 25},
		{name: "half", microseconds: 1000000, duration: 2, want: 50},
		{name: "almost done", microseconds: 1960000, duration: 2, want: 98},
		{name: "duration reached", microseconds: 2000000, duration: 2, want: 99},
		{name: "beyond duration", microseconds: 5000000, duration: 2, want: 99},
		{name: "unknown duration", microseconds: 500000, duration: 0, want: 5},
		{name: "negative duration", microseconds: 500000, duration: -2, want: 5},
		{name: "fractional duration", microseconds: 250000, duration: 0.5, want: 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := burnPercent(tc.microseconds, tc.duration); got != tc.want {
				t.Fatalf("burnPercent(%d, %v) = %d, want %d", tc.microseconds, tc.duration, got, tc.want)
			}
		})
	}
}

func TestBurnEmitsStartAndDoneWhenDurationIsUnknown(t *testing.T) {
	req := burnTestRequest(t)
	runner := &fakeBurnRunner{
		stdout: []string{
			"out_time_us=1000000",
			"out_time=00:00:02.000000",
			"progress=continue",
			"progress=end",
		},
		createOutput: true,
	}

	var events []BurnProgressEvent
	if _, err := Burn(context.Background(), runner, "ffmpeg", req, func(event BurnProgressEvent) {
		events = append(events, event)
	}); err != nil {
		t.Fatalf("burn failed: %v", err)
	}

	want := []BurnProgressEvent{
		{Stage: BurnStagePrepare, Percent: 2, Message: "Preparing subtitles…"},
		{Stage: BurnStageRender, Percent: 5, Message: "Burning subtitles into the video…"},
		{Stage: BurnStageRender, Percent: 100, Message: "Subtitles burned."},
	}
	if len(events) != len(want) {
		t.Fatalf("unexpected events: got %+v, want %+v", events, want)
	}
	for index, wantEvent := range want {
		if events[index] != wantEvent {
			t.Fatalf("event %d = %+v, want %+v", index, events[index], wantEvent)
		}
	}
}

func TestBurnMapsFailureAndCleansUp(t *testing.T) {
	req := burnTestRequest(t)
	var stderr strings.Builder
	for line := 1; line <= 100; line++ {
		fmt.Fprintf(&stderr, "ffmpeg diagnostic line %03d\n", line)
	}
	runner := &fakeBurnRunner{stderr: stderr.String(), err: errors.New("exit status 1")}

	_, err := Burn(context.Background(), runner, "ffmpeg", req, nil)

	var burnErr *BurnError
	if !errors.As(err, &burnErr) {
		t.Fatalf("expected a *BurnError, got %v", err)
	}
	if burnErr.Code != ErrorCodeBurnFailed {
		t.Fatalf("expected code %s, got %s", ErrorCodeBurnFailed, burnErr.Code)
	}
	if burnErr.Message != "FFmpeg could not burn the subtitles" {
		t.Fatalf("unexpected message: %q", burnErr.Message)
	}

	message := err.Error()
	if !strings.Contains(message, "diagnostic line 100") {
		t.Fatalf("expected the stderr tail in the error, got %q", message)
	}
	if strings.Contains(message, "diagnostic line 001") {
		t.Fatalf("the error must not carry the full unbounded log, got %q", message)
	}
	if len(message) > burnStderrTailBytes+256 {
		t.Fatalf("the error is not bounded: %d bytes", len(message))
	}

	if _, statErr := os.Stat(req.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed burn must not publish an output, stat err: %v", statErr)
	}
	call := runner.lastCall(t)
	if _, statErr := os.Stat(call.dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed burn must remove its workspace, stat err: %v", statErr)
	}
	assertNoBurnTempOutputs(t, filepath.Dir(req.OutputPath))
}

func TestBurnMapsCancellationAndCleansUp(t *testing.T) {
	req := burnTestRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &fakeBurnRunner{
		onRun: func(_ context.Context, _ burnRunnerCall) {
			cancel()
		},
		err: context.Canceled,
	}

	_, err := Burn(ctx, runner, "ffmpeg", req, nil)

	var burnErr *BurnError
	if !errors.As(err, &burnErr) {
		t.Fatalf("expected a *BurnError, got %v", err)
	}
	if burnErr.Code != ErrorCodeBurnCancelled {
		t.Fatalf("expected code %s, got %s", ErrorCodeBurnCancelled, burnErr.Code)
	}
	if burnErr.Message != "Burning subtitles was canceled." {
		t.Fatalf("unexpected message: %q", burnErr.Message)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation cause to be preserved, got %v", err)
	}

	if _, statErr := os.Stat(req.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a cancelled burn must not publish an output, stat err: %v", statErr)
	}
	call := runner.lastCall(t)
	if _, statErr := os.Stat(call.dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a cancelled burn must remove its workspace, stat err: %v", statErr)
	}
	assertNoBurnTempOutputs(t, filepath.Dir(req.OutputPath))
}

func TestBurnNormalizesRelativePathsForTheWorkspaceRunner(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeBurnTestFile(t, "input.mp4", []byte("fake input"))
	writeBurnTestFile(t, "captions.srt", []byte(testBurnSRTContent))

	runner := &fakeBurnRunner{
		stdout:       []string{"progress=end"},
		createOutput: true,
		outputBytes:  []byte("rendered video bytes"),
	}

	result, err := Burn(context.Background(), runner, "ffmpeg", BurnRequest{
		InputPath:    "input.mp4",
		SubtitlePath: "captions.srt",
		OutputPath:   "output.mp4",
		AudioCodec:   "aac",
	}, nil)
	if err != nil {
		t.Fatalf("burn failed: %v", err)
	}

	call := runner.lastCall(t)
	if call.dir == dir {
		t.Fatal("the runner must not use the caller working directory")
	}
	if inputIndex := burnArgIndex(call.args, "-i"); inputIndex < 0 || call.args[inputIndex+1] != filepath.Join(dir, "input.mp4") {
		t.Fatalf("input path was not normalized to an absolute path: %v", call.args)
	}
	tempOutput := call.args[len(call.args)-1]
	if !filepath.IsAbs(tempOutput) {
		t.Fatalf("temp output is not absolute: %q", tempOutput)
	}
	if filepath.Dir(tempOutput) != dir {
		t.Fatalf("temp output dir = %q, want %q", filepath.Dir(tempOutput), dir)
	}
	wantOutput := filepath.Join(dir, "output.mp4")
	if result.OutputPath != wantOutput {
		t.Fatalf("result output = %q, want %q", result.OutputPath, wantOutput)
	}
	if _, statErr := os.Stat(wantOutput); statErr != nil {
		t.Fatalf("expected the published output: %v", statErr)
	}
}

func TestBurnPublishesAtomicallyAndPreservesContent(t *testing.T) {
	req := burnTestRequest(t)
	content := []byte("rendered video bytes")
	runner := &fakeBurnRunner{
		stdout:       []string{"progress=end"},
		createOutput: true,
		outputBytes:  content,
	}

	result, err := Burn(context.Background(), runner, "ffmpeg", req, nil)
	if err != nil {
		t.Fatalf("burn failed: %v", err)
	}
	if result.OutputPath != req.OutputPath {
		t.Fatalf("result output = %q, want %q", result.OutputPath, req.OutputPath)
	}
	if result.SourceDurationSeconds != 0 {
		t.Fatalf("expected an unknown duration for the fake input, got %v", result.SourceDurationSeconds)
	}

	data, readErr := os.ReadFile(req.OutputPath)
	if readErr != nil {
		t.Fatalf("read published output: %v", readErr)
	}
	if string(data) != string(content) {
		t.Fatalf("published content = %q, want %q", data, content)
	}
	info, statErr := os.Stat(req.OutputPath)
	if statErr != nil {
		t.Fatalf("stat published output: %v", statErr)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("expected a 0644 published output, got %o", info.Mode().Perm())
	}

	call := runner.lastCall(t)
	tempOutputPath := call.args[len(call.args)-1]
	if tempOutputPath == req.OutputPath {
		t.Fatal("ffmpeg must write to a temp file, never directly to the final path")
	}
	if filepath.Dir(tempOutputPath) != filepath.Dir(req.OutputPath) {
		t.Fatalf("the temp output must live in the destination directory: %q", tempOutputPath)
	}
	if !strings.HasPrefix(filepath.Base(tempOutputPath), ".fileforge-burn-") {
		t.Fatalf("unexpected temp output name: %q", tempOutputPath)
	}
	if _, statErr := os.Stat(tempOutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the temp output must be renamed away, stat err: %v", statErr)
	}
	assertNoBurnTempOutputs(t, filepath.Dir(req.OutputPath))
}

type burnRequestMutator func(t *testing.T, req *BurnRequest)

func TestBurnRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name        string
		mutate      burnRequestMutator
		wantMessage string
	}{
		{
			name:        "missing input path",
			mutate:      func(_ *testing.T, req *BurnRequest) { req.InputPath = "  " },
			wantMessage: "inputPath is required",
		},
		{
			name: "missing input file",
			mutate: func(t *testing.T, req *BurnRequest) {
				req.InputPath = filepath.Join(t.TempDir(), "missing.mp4")
			},
			wantMessage: "input does not exist",
		},
		{
			name: "input is a directory",
			mutate: func(t *testing.T, req *BurnRequest) {
				req.InputPath = t.TempDir()
			},
			wantMessage: "input path is a directory",
		},
		{
			name:        "missing subtitle path",
			mutate:      func(_ *testing.T, req *BurnRequest) { req.SubtitlePath = "" },
			wantMessage: "subtitlePath is required",
		},
		{
			name: "subtitle is not srt",
			mutate: func(t *testing.T, req *BurnRequest) {
				path := filepath.Join(t.TempDir(), "captions.vtt")
				writeBurnTestFile(t, path, []byte("WEBVTT\n"))
				req.SubtitlePath = path
			},
			wantMessage: "must be an .srt file",
		},
		{
			name: "missing subtitle file",
			mutate: func(t *testing.T, req *BurnRequest) {
				req.SubtitlePath = filepath.Join(t.TempDir(), "missing.srt")
			},
			wantMessage: "subtitle does not exist",
		},
		{
			name:        "missing output path",
			mutate:      func(_ *testing.T, req *BurnRequest) { req.OutputPath = "" },
			wantMessage: "outputPath is required",
		},
		{
			name: "missing output directory",
			mutate: func(t *testing.T, req *BurnRequest) {
				req.OutputPath = filepath.Join(t.TempDir(), "missing", "output.mp4")
			},
			wantMessage: "output directory does not exist",
		},
		{
			name: "existing output",
			mutate: func(t *testing.T, req *BurnRequest) {
				writeBurnTestFile(t, req.OutputPath, []byte("existing"))
			},
			wantMessage: "output already exists",
		},
		{
			name:        "output collides with input",
			mutate:      func(_ *testing.T, req *BurnRequest) { req.OutputPath = req.InputPath },
			wantMessage: "collides with the input path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := burnTestRequest(t)
			tc.mutate(t, &req)

			runner := &fakeBurnRunner{}
			result, err := Burn(context.Background(), runner, "ffmpeg", req, nil)

			if result != (BurnResult{}) {
				t.Fatalf("validation failure returned a result: %+v", result)
			}
			var burnErr *BurnError
			if !errors.As(err, &burnErr) {
				t.Fatalf("expected a *BurnError, got %v", err)
			}
			if burnErr.Code != ErrorCodeBurnValidation {
				t.Fatalf("expected code %s, got %s", ErrorCodeBurnValidation, burnErr.Code)
			}
			if !strings.Contains(burnErr.Message, tc.wantMessage) {
				t.Fatalf("message %q does not mention %q", burnErr.Message, tc.wantMessage)
			}
			if len(runner.calls) != 0 {
				t.Fatalf("validation must not run ffmpeg, calls: %+v", runner.calls)
			}
		})
	}
}

func TestBurnMapsMissingFFmpegRuntime(t *testing.T) {
	req := burnTestRequest(t)

	_, err := Burn(context.Background(), &ExecBurnCommandRunner{}, "fileforge-missing-ffmpeg-binary", req, nil)

	var burnErr *BurnError
	if !errors.As(err, &burnErr) {
		t.Fatalf("expected a *BurnError, got %v", err)
	}
	if burnErr.Code != ErrorCodeBurnRuntimeUnavailable {
		t.Fatalf("expected code %s, got %s", ErrorCodeBurnRuntimeUnavailable, burnErr.Code)
	}
	if burnErr.Message != "FFmpeg was not found. Install or repair the local toolchain." {
		t.Fatalf("unexpected message: %q", burnErr.Message)
	}
	if _, statErr := os.Stat(req.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a missing runtime must not publish an output, stat err: %v", statErr)
	}
	assertNoBurnTempOutputs(t, filepath.Dir(req.OutputPath))
}

func TestBoundedBurnOutputKeepsOnlyTheTail(t *testing.T) {
	var raw strings.Builder
	for line := 1; line <= 100; line++ {
		fmt.Fprintf(&raw, "line %03d\n", line)
	}

	tail := boundedBurnOutput(raw.String())
	if !strings.Contains(tail, "line 100") {
		t.Fatalf("the tail must keep the last line, got %q", tail)
	}
	if !strings.Contains(tail, "line 041") {
		t.Fatalf("the tail must keep the last 60 lines, got %q", tail)
	}
	if strings.Contains(tail, "line 040") || strings.Contains(tail, "line 001") {
		t.Fatalf("the tail must drop the oldest lines, got %q", tail)
	}
	if len(tail) > burnStderrTailBytes {
		t.Fatalf("the tail is not bounded: %d bytes", len(tail))
	}

	huge := strings.Repeat("x", burnStderrTailBytes*4)
	clipped := boundedBurnOutput(huge)
	if len(clipped) > burnStderrTailBytes {
		t.Fatalf("a single huge line is not bounded: %d bytes", len(clipped))
	}
	if !strings.HasSuffix(huge, clipped) {
		t.Fatal("the bounded output must be the tail of the original line")
	}
}

func TestExecBurnCommandRunnerStreamsStdout(t *testing.T) {
	ffmpegPath, lookupErr := exec.LookPath("ffmpeg")
	if lookupErr != nil {
		t.Skipf("ffmpeg is not available: %v", lookupErr)
	}

	var lines []string
	stderr, err := (&ExecBurnCommandRunner{}).RunBurn(
		context.Background(),
		t.TempDir(),
		ffmpegPath,
		[]string{"-hide_banner", "-version"},
		func(line string) {
			lines = append(lines, line)
		},
	)
	if err != nil {
		t.Fatalf("ffmpeg -version failed: %v (stderr: %s)", err, stderr)
	}
	if len(lines) == 0 {
		t.Fatalf("expected streamed stdout lines, got none (stderr: %s)", stderr)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "ffmpeg version") {
		t.Fatalf("unexpected streamed output: %q", lines)
	}
}

func TestExecBurnCommandRunnerStopsOnCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the real ffmpeg cancellation test in short mode")
	}
	ffmpegPath, lookupErr := exec.LookPath("ffmpeg")
	if lookupErr != nil {
		t.Skipf("ffmpeg is not available: %v", lookupErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{}, 1)
	errCh := make(chan error, 1)
	go func() {
		_, runErr := (&ExecBurnCommandRunner{}).RunBurn(ctx, t.TempDir(), ffmpegPath, []string{
			"-hide_banner",
			"-loglevel", "error",
			"-re",
			"-f", "lavfi",
			"-i", "testsrc=size=320x180:rate=25:duration=30",
			"-progress", "pipe:1",
			"-nostats",
			"-f", "null", os.DevNull,
		}, func(string) {
			select {
			case started <- struct{}{}:
			default:
			}
		})
		errCh <- runErr
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("ffmpeg never streamed progress before cancellation")
	}

	canceledAt := time.Now()
	cancel()

	select {
	case runErr := <-errCh:
		if runErr == nil {
			t.Fatal("expected the cancelled command to fail")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunBurn did not return after context cancellation")
	}

	if ctx.Err() != context.Canceled {
		t.Fatalf("expected a cancelled context, got %v", ctx.Err())
	}
	if elapsed := time.Since(canceledAt); elapsed > 10*time.Second {
		t.Fatalf("RunBurn took %s to return after cancellation", elapsed)
	} else {
		t.Logf("cancelled real ffmpeg run returned in %s", elapsed)
	}
}

func TestBurnRendersSubtitlePixelsIntoTheVideo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the real ffmpeg burn test in short mode")
	}
	ffmpegPath, lookupErr := exec.LookPath("ffmpeg")
	if lookupErr != nil {
		t.Skipf("ffmpeg is not available: %v", lookupErr)
	}
	if _, probeErr := exec.LookPath("ffprobe"); probeErr != nil {
		t.Skipf("ffprobe is not available: %v", probeErr)
	}

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.mp4")
	runBurnTestCommand(t, ffmpegPath,
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
		inputPath,
	)

	subtitlePath := filepath.Join(dir, "subtitles.srt")
	writeBurnTestFile(t, subtitlePath, []byte(testBurnSRTContent))
	outputPath := filepath.Join(dir, "burned.mp4")

	preCueSource := filepath.Join(dir, "source-0.05.png")
	inCueSource := filepath.Join(dir, "source-0.7.png")
	extractBurnFrame(t, ffmpegPath, inputPath, preCueSource, 0.05)
	extractBurnFrame(t, ffmpegPath, inputPath, inCueSource, 0.7)

	var events []BurnProgressEvent
	result, burnErr := Burn(
		context.Background(),
		&ExecBurnCommandRunner{},
		ffmpegPath,
		BurnRequest{
			InputPath:    inputPath,
			SubtitlePath: subtitlePath,
			OutputPath:   outputPath,
			AudioCodec:   "aac",
		},
		func(event BurnProgressEvent) {
			events = append(events, event)
		},
	)
	if burnErr != nil {
		t.Fatalf("burn failed: %v", burnErr)
	}

	if math.Abs(result.SourceDurationSeconds-2.0) > 0.25 {
		t.Fatalf("source duration = %.3f, want 2.0 +/- 0.25", result.SourceDurationSeconds)
	}
	if result.OutputPath != outputPath {
		t.Fatalf("result output = %q, want %q", result.OutputPath, outputPath)
	}
	if len(events) < 3 {
		t.Fatalf("expected prepare, render and done events, got %+v", events)
	}
	if events[0].Stage != BurnStagePrepare || events[0].Percent != 2 {
		t.Fatalf("unexpected first event: %+v", events[0])
	}
	if last := events[len(events)-1]; last.Stage != BurnStageRender || last.Percent != 100 {
		t.Fatalf("unexpected last event: %+v", last)
	}
	sawRenderProgress := false
	for _, event := range events {
		if event.Stage == BurnStageRender && event.Percent > 5 && event.Percent < 100 {
			sawRenderProgress = true
		}
	}
	if !sawRenderProgress {
		t.Fatalf("expected render progress between 5 and 99, got %+v", events)
	}

	preCueOutput := filepath.Join(dir, "output-0.05.png")
	inCueOutput := filepath.Join(dir, "output-0.7.png")
	extractBurnFrame(t, ffmpegPath, outputPath, preCueOutput, 0.05)
	extractBurnFrame(t, ffmpegPath, outputPath, inCueOutput, 0.7)

	preCueDiff := burnBottomBandDiff(t, preCueSource, preCueOutput)
	inCueDiff := burnBottomBandDiff(t, inCueSource, inCueOutput)
	t.Logf("bottom-band mean absolute difference: before cue = %.3f, inside cue = %.3f", preCueDiff, inCueDiff)

	if preCueDiff >= 1.0 {
		t.Fatalf("re-encode noise before the cue is too high: %.3f", preCueDiff)
	}
	if inCueDiff <= 2.0 {
		t.Fatalf("subtitle pixels did not change the frame enough: %.3f", inCueDiff)
	}
	if inCueDiff <= 3*preCueDiff {
		t.Fatalf("the in-cue difference (%.3f) must clearly exceed the pre-cue difference (%.3f)", inCueDiff, preCueDiff)
	}
}

func runBurnTestCommand(t *testing.T, name string, args ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func extractBurnFrame(t *testing.T, ffmpegPath, inputPath, outputPath string, atSeconds float64) {
	t.Helper()

	runBurnTestCommand(t, ffmpegPath,
		"-y", "-hide_banner", "-loglevel", "error",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", inputPath,
		"-frames:v", "1",
		outputPath,
	)
}

func decodeBurnGrayPNG(t *testing.T, path string) *image.Gray {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open frame %s: %v", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode frame %s: %v", path, err)
	}

	gray := image.NewGray(decoded.Bounds())
	draw.Draw(gray, gray.Bounds(), decoded, decoded.Bounds().Min, draw.Src)

	return gray
}

// burnBottomBandDiff measures how much the bottom 35% of the frame changed.
// Subtitles sit there, so a rendered cue raises the difference sharply.
func burnBottomBandDiff(t *testing.T, sourcePath, outputPath string) float64 {
	t.Helper()

	source := decodeBurnGrayPNG(t, sourcePath)
	output := decodeBurnGrayPNG(t, outputPath)
	if source.Bounds() != output.Bounds() {
		t.Fatalf("frame sizes differ: %v vs %v", source.Bounds(), output.Bounds())
	}

	bounds := source.Bounds()
	startY := bounds.Min.Y + int(math.Round(float64(bounds.Dy())*0.65))
	var sum int64
	var count int64
	for y := startY; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			diff := int64(source.GrayAt(x, y).Y) - int64(output.GrayAt(x, y).Y)
			if diff < 0 {
				diff = -diff
			}
			sum += diff
			count++
		}
	}
	if count == 0 {
		t.Fatal("the measured frame band is empty")
	}

	return float64(sum) / float64(count)
}
