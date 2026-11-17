package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testTranscribeSRTContent = "1\n00:00:00,000 --> 00:00:02,000\nHello world\n\n2\n00:00:02,000 --> 00:00:04,000\nSecond line\n"
	testTranscribeVTTContent = "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello world\n\n00:00:02.000 --> 00:00:04.000\nSecond line\n"
	testTranscribeTXTContent = "Hello world\nSecond line\n"
)

// stubTranscribePaths isolates the binary/model search from the real machine.
// A nil lookPath simulates whisper-cli not being on PATH.
func stubTranscribePaths(t *testing.T, homeDir, configDir string, lookPath func(string) (string, error)) {
	t.Helper()

	originalHome := transcribeUserHomeDir
	originalConfig := transcribeUserConfigDir
	originalLookPath := transcribeLookPath

	transcribeUserHomeDir = func() (string, error) { return homeDir, nil }
	transcribeUserConfigDir = func() (string, error) { return configDir, nil }
	if lookPath == nil {
		transcribeLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	} else {
		transcribeLookPath = lookPath
	}

	t.Cleanup(func() {
		transcribeUserHomeDir = originalHome
		transcribeUserConfigDir = originalConfig
		transcribeLookPath = originalLookPath
	})
}

func writeTranscribeTestFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func TestResolveWhisperPathPrefersProvidedOverride(t *testing.T) {
	provided := filepath.Join(t.TempDir(), "custom-whisper")
	writeTranscribeTestFile(t, provided, "binary", 0o755)
	stubTranscribePaths(t, t.TempDir(), t.TempDir(), nil)

	got, present := ResolveWhisperPath(provided)
	if !present {
		t.Fatal("expected the provided binary to be present")
	}
	if got != provided {
		t.Fatalf("expected %q, got %q", provided, got)
	}
}

func TestResolveWhisperPathUsesInstalledLocalShareDefault(t *testing.T) {
	home := t.TempDir()
	installed := filepath.Join(home, ".local", "share", "fileforge", "whisper", "whisper-cli")
	writeTranscribeTestFile(t, installed, "binary", 0o755)
	stubTranscribePaths(t, home, t.TempDir(), nil)

	got, present := ResolveWhisperPath("")
	if !present {
		t.Fatal("expected the installed default binary to be present")
	}
	if got != installed {
		t.Fatalf("expected %q, got %q", installed, got)
	}
}

func TestResolveWhisperPathFallsBackToConfigDir(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	installed := filepath.Join(configDir, "fileforge", "whisper", "whisper-cli")
	writeTranscribeTestFile(t, installed, "binary", 0o755)
	stubTranscribePaths(t, home, configDir, nil)

	got, present := ResolveWhisperPath("")
	if !present {
		t.Fatal("expected the config-dir binary to be present")
	}
	if got != installed {
		t.Fatalf("expected %q, got %q", installed, got)
	}
}

func TestResolveWhisperPathFallsBackToPath(t *testing.T) {
	home := t.TempDir()
	configDir := t.TempDir()
	pathBinary := filepath.Join(t.TempDir(), "whisper-cli")
	writeTranscribeTestFile(t, pathBinary, "binary", 0o755)
	stubTranscribePaths(t, home, configDir, func(name string) (string, error) {
		if name != "whisper-cli" {
			return "", fmt.Errorf("unexpected lookup: %s", name)
		}
		return pathBinary, nil
	})

	got, present := ResolveWhisperPath("")
	if !present {
		t.Fatal("expected the PATH binary to be present")
	}
	if got != pathBinary {
		t.Fatalf("expected %q, got %q", pathBinary, got)
	}
}

func TestResolveWhisperPathMissingReportsExpectedPath(t *testing.T) {
	home := t.TempDir()
	stubTranscribePaths(t, home, t.TempDir(), nil)

	got, present := ResolveWhisperPath("")
	if present {
		t.Fatal("expected a missing binary")
	}

	want := filepath.Join(home, ".local", "share", "fileforge", "whisper", "whisper-cli")
	if got != want {
		t.Fatalf("expected the expected path %q, got %q", want, got)
	}
}

func TestResolveModelPathUsesInstalledDefaultAndRejectsEmptyFiles(t *testing.T) {
	home := t.TempDir()
	modelPath := filepath.Join(home, ".local", "share", "fileforge", "models", "ggml-base.bin")
	writeTranscribeTestFile(t, modelPath, "ggml", 0o644)
	stubTranscribePaths(t, home, t.TempDir(), nil)

	got, present := ResolveModelPath("")
	if !present || got != modelPath {
		t.Fatalf("expected %q present, got %q present=%t", modelPath, got, present)
	}

	emptyModel := filepath.Join(t.TempDir(), "ggml-empty.bin")
	writeTranscribeTestFile(t, emptyModel, "", 0o644)
	if err := os.Remove(modelPath); err != nil {
		t.Fatalf("remove the default model: %v", err)
	}

	got, present = ResolveModelPath(emptyModel)
	if present {
		t.Fatal("expected a zero-byte model to be rejected")
	}
	if got != emptyModel {
		t.Fatalf("expected the provided empty model path %q, got %q", emptyModel, got)
	}
}

func TestResolveModelPathFallsBackToConfigDir(t *testing.T) {
	configDir := t.TempDir()
	modelPath := filepath.Join(configDir, "fileforge", "models", "ggml-base.bin")
	writeTranscribeTestFile(t, modelPath, "ggml", 0o644)
	stubTranscribePaths(t, t.TempDir(), configDir, nil)

	got, present := ResolveModelPath("")
	if !present || got != modelPath {
		t.Fatalf("expected %q present, got %q present=%t", modelPath, got, present)
	}
}

func TestResolveModelPathMissingReportsExpectedPath(t *testing.T) {
	home := t.TempDir()
	stubTranscribePaths(t, home, t.TempDir(), nil)

	got, present := ResolveModelPath("")
	if present {
		t.Fatal("expected a missing model")
	}

	want := filepath.Join(home, ".local", "share", "fileforge", "models", "ggml-base.bin")
	if got != want {
		t.Fatalf("expected the expected path %q, got %q", want, got)
	}
}

func TestDetectTranscriptionCapabilitiesReportsRuntime(t *testing.T) {
	home := t.TempDir()
	binaryPath := filepath.Join(home, ".local", "share", "fileforge", "whisper", "whisper-cli")
	modelPath := filepath.Join(home, ".local", "share", "fileforge", "models", "ggml-base.bin")
	writeTranscribeTestFile(t, binaryPath, "binary", 0o755)
	writeTranscribeTestFile(t, modelPath, "12345", 0o644)
	stubTranscribePaths(t, home, t.TempDir(), nil)

	capabilities := DetectTranscriptionCapabilities("", "")
	if !capabilities.Available() {
		t.Fatalf("expected an available runtime, got %+v", capabilities)
	}
	if capabilities.WhisperPath != binaryPath {
		t.Fatalf("expected whisper path %q, got %q", binaryPath, capabilities.WhisperPath)
	}
	if capabilities.ModelPath != modelPath {
		t.Fatalf("expected model path %q, got %q", modelPath, capabilities.ModelPath)
	}
	if capabilities.ModelName != "ggml-base.bin" {
		t.Fatalf("expected ggml-base.bin, got %q", capabilities.ModelName)
	}
	if capabilities.ModelSizeBytes != 5 {
		t.Fatalf("expected 5 model bytes, got %d", capabilities.ModelSizeBytes)
	}
	if strings.Join(capabilities.Languages, ",") != "auto,en,es" {
		t.Fatalf("unexpected languages: %v", capabilities.Languages)
	}
	if capabilities.DefaultLanguage != TranscribeLanguageAuto {
		t.Fatalf("expected auto as the default language, got %q", capabilities.DefaultLanguage)
	}
}

func TestDetectTranscriptionCapabilitiesMissingRuntime(t *testing.T) {
	stubTranscribePaths(t, t.TempDir(), t.TempDir(), nil)

	capabilities := DetectTranscriptionCapabilities("", "")
	if capabilities.Available() {
		t.Fatal("expected an unavailable runtime")
	}
	if capabilities.WhisperPresent || capabilities.ModelPresent {
		t.Fatalf("expected both dependencies to be missing, got %+v", capabilities)
	}
}

// fakeTranscribeRunner replaces ffmpeg and whisper-cli. The default behaviour
// writes a fake WAV and the SRT/VTT/TXT files so the pipeline can publish them.
type fakeTranscribeRunner struct {
	mu      sync.Mutex
	calls   []transcribeRunnerCall
	ffmpeg  func(ctx context.Context, args []string) ([]byte, error)
	whisper func(ctx context.Context, name string, args []string) ([]byte, error)
}

type transcribeRunnerCall struct {
	Name string
	Args []string
}

func newFakeTranscribeRunner() *fakeTranscribeRunner {
	return &fakeTranscribeRunner{}
}

func (r *fakeTranscribeRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, transcribeRunnerCall{Name: name, Args: append([]string{}, args...)})
	ffmpeg := r.ffmpeg
	whisper := r.whisper
	r.mu.Unlock()

	if name == "ffmpeg" {
		if ffmpeg != nil {
			return ffmpeg(ctx, args)
		}

		return r.writeFakeAudio(args)
	}

	if whisper != nil {
		return whisper(ctx, name, args)
	}

	return r.writeFakeWhisperOutputs(args)
}

func (r *fakeTranscribeRunner) writeFakeAudio(args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("ffmpeg called without arguments")
	}

	audioPath := args[len(args)-1]
	if err := os.WriteFile(audioPath, []byte("RIFF-fake-wav"), 0o644); err != nil {
		return nil, err
	}

	return nil, nil
}

func (r *fakeTranscribeRunner) writeFakeWhisperOutputs(args []string) ([]byte, error) {
	prefix := transcribeArgValue(args, "-of")
	if prefix == "" {
		return nil, errors.New("whisper called without -of")
	}

	contents := map[string]string{
		TranscribeFormatSRT: testTranscribeSRTContent,
		TranscribeFormatVTT: testTranscribeVTTContent,
		TranscribeFormatTXT: testTranscribeTXTContent,
	}
	for _, flag := range args {
		format := ""
		switch flag {
		case "-osrt":
			format = TranscribeFormatSRT
		case "-ovtt":
			format = TranscribeFormatVTT
		case "-otxt":
			format = TranscribeFormatTXT
		default:
			continue
		}

		if err := os.WriteFile(prefix+"."+format, []byte(contents[format]), 0o644); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func (r *fakeTranscribeRunner) snapshotCalls() []transcribeRunnerCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]transcribeRunnerCall{}, r.calls...)
}

func (r *fakeTranscribeRunner) firstCall(name string) (transcribeRunnerCall, bool) {
	for _, call := range r.snapshotCalls() {
		if call.Name == name {
			return call, true
		}
	}

	return transcribeRunnerCall{}, false
}

func transcribeArgValue(args []string, flag string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag {
			return args[index+1]
		}
	}

	return ""
}

// newTranscribeEngineFixture builds a request with an existing binary, model
// and input, plus one output path per requested format.
func newTranscribeEngineFixture(t *testing.T, formats ...string) TranscribeRequest {
	t.Helper()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "whisper-cli")
	modelPath := filepath.Join(dir, "ggml-base.bin")
	writeTranscribeTestFile(t, binaryPath, "binary", 0o755)
	writeTranscribeTestFile(t, modelPath, "model", 0o644)

	inputDir := filepath.Join(dir, "media")
	inputPath := filepath.Join(inputDir, "clip.mp4")
	writeTranscribeTestFile(t, inputPath, "media", 0o644)

	outputPaths := make(map[string]string, len(formats))
	for _, format := range formats {
		outputPaths[format] = filepath.Join(dir, "out."+format)
	}

	return TranscribeRequest{
		InputPath:   inputPath,
		OutputPaths: outputPaths,
		WhisperPath: binaryPath,
		ModelPath:   modelPath,
		Language:    "es",
	}
}

func TestTranscribePublishesRequestedFormats(t *testing.T) {
	runner := newFakeTranscribeRunner()
	request := newTranscribeEngineFixture(t, TranscribeFormatSRT, TranscribeFormatTXT)

	var stages []string
	result, transcribeErr := Transcribe(context.Background(), runner, request, func(event TranscribeProgressEvent) {
		stages = append(stages, event.Stage)
	})
	if transcribeErr != nil {
		t.Fatalf("unexpected transcription error: %v", transcribeErr)
	}

	if result.TextCharacters != 20 {
		t.Fatalf("expected 20 characters, got %d", result.TextCharacters)
	}
	if result.Segments != 2 {
		t.Fatalf("expected 2 segments, got %d", result.Segments)
	}
	if result.Language != "es" {
		t.Fatalf("expected es, got %q", result.Language)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}

	srtContent, err := os.ReadFile(request.OutputPaths[TranscribeFormatSRT])
	if err != nil {
		t.Fatalf("read srt output: %v", err)
	}
	if string(srtContent) != testTranscribeSRTContent {
		t.Fatalf("unexpected srt content: %q", string(srtContent))
	}
	txtContent, err := os.ReadFile(request.OutputPaths[TranscribeFormatTXT])
	if err != nil {
		t.Fatalf("read txt output: %v", err)
	}
	if string(txtContent) != testTranscribeTXTContent {
		t.Fatalf("unexpected txt content: %q", string(txtContent))
	}
	if _, err := os.Stat(request.OutputPaths[TranscribeFormatVTT]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected no VTT output when it was not requested")
	}

	whisperCall, ok := runner.firstCall(request.WhisperPath)
	if !ok {
		t.Fatal("expected a whisper invocation")
	}
	if got := transcribeArgValue(whisperCall.Args, "-l"); got != "es" {
		t.Fatalf("expected -l es, got %q", got)
	}
	if got := transcribeArgValue(whisperCall.Args, "-m"); got != request.ModelPath {
		t.Fatalf("expected model %q, got %q", request.ModelPath, got)
	}
	if hasString(whisperCall.Args, "-ovtt") {
		t.Fatal("expected no -ovtt flag when VTT was not requested")
	}
	if hasString(whisperCall.Args, "-osrt") == false || hasString(whisperCall.Args, "-otxt") == false {
		t.Fatalf("expected srt and txt flags, got %v", whisperCall.Args)
	}

	threadsText := transcribeArgValue(whisperCall.Args, "-t")
	if threadsText == "" {
		t.Fatal("expected a thread count")
	}
	var threads int
	if _, err := fmt.Sscanf(threadsText, "%d", &threads); err != nil {
		t.Fatalf("parse threads %q: %v", threadsText, err)
	}
	if threads < 1 || threads > transcribeMaxThreads {
		t.Fatalf("expected 1..%d threads, got %d", transcribeMaxThreads, threads)
	}

	if strings.Join(stages, ",") != "extract,transcribe,publish" {
		t.Fatalf("unexpected progress stages: %v", stages)
	}

	ffmpegCall, ok := runner.firstCall("ffmpeg")
	if !ok {
		t.Fatal("expected an ffmpeg invocation")
	}
	tempDir := filepath.Dir(ffmpegCall.Args[len(ffmpegCall.Args)-1])
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the temp workspace %q to be removed, got %v", tempDir, err)
	}
}

func TestTranscribeCountsFromSRTWhenTXTIsNotRequested(t *testing.T) {
	runner := newFakeTranscribeRunner()
	request := newTranscribeEngineFixture(t, TranscribeFormatSRT)

	result, transcribeErr := Transcribe(context.Background(), runner, request, nil)
	if transcribeErr != nil {
		t.Fatalf("unexpected transcription error: %v", transcribeErr)
	}

	if result.TextCharacters != 20 {
		t.Fatalf("expected 20 characters from SRT speech, got %d", result.TextCharacters)
	}
	if result.Segments != 2 {
		t.Fatalf("expected 2 segments, got %d", result.Segments)
	}
}

func TestTranscribeVTTOnlyWarnsWhenWhisperWritesNothing(t *testing.T) {
	runner := newFakeTranscribeRunner()
	runner.whisper = func(context.Context, string, []string) ([]byte, error) {
		return nil, nil
	}
	request := newTranscribeEngineFixture(t, TranscribeFormatVTT)

	result, transcribeErr := Transcribe(context.Background(), runner, request, nil)
	if transcribeErr != nil {
		t.Fatalf("unexpected transcription error: %v", transcribeErr)
	}

	if result.TextCharacters != 0 || result.Segments != 0 {
		t.Fatalf("expected empty counts, got %d characters, %d segments", result.TextCharacters, result.Segments)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != TranscriptionWarningNoSpeech {
		t.Fatalf("expected the no-speech warning, got %v", result.Warnings)
	}

	content, err := os.ReadFile(request.OutputPaths[TranscribeFormatVTT])
	if err != nil {
		t.Fatalf("read vtt output: %v", err)
	}
	if string(content) != "WEBVTT\n\n" {
		t.Fatalf("unexpected empty VTT content: %q", string(content))
	}
}

func TestTranscribeMapsAudioFailure(t *testing.T) {
	runner := newFakeTranscribeRunner()
	runner.ffmpeg = func(context.Context, []string) ([]byte, error) {
		return nil, errors.New("ffmpeg exploded")
	}
	request := newTranscribeEngineFixture(t, TranscribeFormatSRT)

	_, transcribeErr := Transcribe(context.Background(), runner, request, nil)
	if transcribeErr == nil || transcribeErr.Code != ErrorCodeTranscribeAudioFailed {
		t.Fatalf("expected %s, got %v", ErrorCodeTranscribeAudioFailed, transcribeErr)
	}
	if _, err := os.Stat(request.OutputPaths[TranscribeFormatSRT]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected no final output after an audio failure")
	}
	if _, ok := runner.firstCall("ffmpeg"); !ok {
		t.Fatal("expected ffmpeg to run")
	}
}

func TestTranscribeMapsWhisperFailureAndCleansTemp(t *testing.T) {
	runner := newFakeTranscribeRunner()
	runner.whisper = func(context.Context, string, []string) ([]byte, error) {
		return []byte("native crash"), errors.New("whisper exploded")
	}
	request := newTranscribeEngineFixture(t, TranscribeFormatSRT, TranscribeFormatVTT)

	_, transcribeErr := Transcribe(context.Background(), runner, request, nil)
	if transcribeErr == nil || transcribeErr.Code != ErrorCodeTranscribeFailed {
		t.Fatalf("expected %s, got %v", ErrorCodeTranscribeFailed, transcribeErr)
	}

	for _, format := range []string{TranscribeFormatSRT, TranscribeFormatVTT} {
		if _, err := os.Stat(request.OutputPaths[format]); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected no final %s output after a whisper failure", format)
		}
	}

	ffmpegCall, ok := runner.firstCall("ffmpeg")
	if !ok {
		t.Fatal("expected ffmpeg to run")
	}
	tempDir := filepath.Dir(ffmpegCall.Args[len(ffmpegCall.Args)-1])
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the temp workspace %q to be removed, got %v", tempDir, err)
	}
}

func TestTranscribeCancelKillsWhisperAndCleansUp(t *testing.T) {
	runner := newFakeTranscribeRunner()
	started := make(chan struct{})
	runner.whisper = func(ctx context.Context, _ string, _ []string) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	request := newTranscribeEngineFixture(t, TranscribeFormatSRT)

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		result TranscribeResult
		err    *TranscriptionError
	}
	done := make(chan outcome, 1)
	go func() {
		result, transcribeErr := Transcribe(ctx, runner, request, nil)
		done <- outcome{result: result, err: transcribeErr}
	}()

	<-started
	cancel()

	select {
	case got := <-done:
		if got.err == nil || got.err.Code != ErrorCodeTranscribeCancelled {
			t.Fatalf("expected %s, got %v", ErrorCodeTranscribeCancelled, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the cancelled transcription")
	}

	if _, err := os.Stat(request.OutputPaths[TranscribeFormatSRT]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected no final output after cancellation")
	}

	ffmpegCall, ok := runner.firstCall("ffmpeg")
	if !ok {
		t.Fatal("expected ffmpeg to run")
	}
	tempDir := filepath.Dir(ffmpegCall.Args[len(ffmpegCall.Args)-1])
	if _, err := os.Stat(tempDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the temp workspace %q to be removed, got %v", tempDir, err)
	}
}

func TestTranscribeRejectsExistingOutputBeforeRunning(t *testing.T) {
	runner := newFakeTranscribeRunner()
	request := newTranscribeEngineFixture(t, TranscribeFormatSRT)
	writeTranscribeTestFile(t, request.OutputPaths[TranscribeFormatSRT], "existing", 0o644)

	_, transcribeErr := Transcribe(context.Background(), runner, request, nil)
	if transcribeErr == nil || transcribeErr.Code != ErrorCodeTranscribeValidation {
		t.Fatalf("expected %s, got %v", ErrorCodeTranscribeValidation, transcribeErr)
	}
	if len(runner.snapshotCalls()) != 0 {
		t.Fatalf("expected no commands for a validation failure, got %v", runner.snapshotCalls())
	}
}

func TestValidateTranscribeRequestRejectsInvalidRequests(t *testing.T) {
	dir := t.TempDir()
	validInput := filepath.Join(dir, "clip.mp4")
	writeTranscribeTestFile(t, validInput, "media", 0o644)

	baseRequest := func() TranscribeRequest {
		return TranscribeRequest{
			InputPath:   validInput,
			OutputPaths: map[string]string{TranscribeFormatSRT: filepath.Join(dir, "out.srt")},
			Language:    "en",
		}
	}

	cases := []struct {
		name string
		edit func(request *TranscribeRequest)
		want string
	}{
		{
			name: "missing input",
			edit: func(request *TranscribeRequest) { request.InputPath = "" },
			want: "inputPath is required",
		},
		{
			name: "unsupported input extension",
			edit: func(request *TranscribeRequest) { request.InputPath = filepath.Join(dir, "notes.pdf") },
			want: "input file type is not supported",
		},
		{
			name: "missing input file",
			edit: func(request *TranscribeRequest) { request.InputPath = filepath.Join(dir, "missing.mp4") },
			want: "input does not exist",
		},
		{
			name: "invalid language",
			edit: func(request *TranscribeRequest) { request.Language = "fr" },
			want: "language must be auto, en, or es",
		},
		{
			name: "no outputs",
			edit: func(request *TranscribeRequest) { request.OutputPaths = nil },
			want: "at least one output format",
		},
		{
			name: "unknown output format",
			edit: func(request *TranscribeRequest) {
				request.OutputPaths = map[string]string{"json": filepath.Join(dir, "out.json")}
			},
			want: "output format must be srt, vtt, or txt",
		},
		{
			name: "mismatched output extension",
			edit: func(request *TranscribeRequest) {
				request.OutputPaths = map[string]string{TranscribeFormatSRT: filepath.Join(dir, "out.txt")}
			},
			want: "does not match format",
		},
		{
			name: "missing output directory",
			edit: func(request *TranscribeRequest) {
				request.OutputPaths = map[string]string{TranscribeFormatSRT: filepath.Join(dir, "nope", "out.srt")}
			},
			want: "output directory does not exist",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := baseRequest()
			testCase.edit(&request)

			validationErr := ValidateTranscribeRequest(request)
			if validationErr == nil {
				t.Fatal("expected a validation error")
			}
			if validationErr.Code != ErrorCodeTranscribeValidation {
				t.Fatalf("expected %s, got %s", ErrorCodeTranscribeValidation, validationErr.Code)
			}
			if !strings.Contains(validationErr.Error(), testCase.want) {
				t.Fatalf("expected message containing %q, got %q", testCase.want, validationErr.Error())
			}
		})
	}
}

func TestValidateTranscribeRequestDefaultsLanguageToAuto(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp3")
	writeTranscribeTestFile(t, input, "media", 0o644)

	validationErr := ValidateTranscribeRequest(TranscribeRequest{
		InputPath:   input,
		OutputPaths: map[string]string{TranscribeFormatTXT: filepath.Join(dir, "out.txt")},
	})
	if validationErr != nil {
		t.Fatalf("expected a valid request, got %v", validationErr)
	}
}

// TestTranscribeRealWhisperJFKSample is an optional integration test. It only
// runs when the installed whisper binary, model and the whisper.cpp JFK sample
// exist; it never downloads anything.
func TestTranscribeRealWhisperJFKSample(t *testing.T) {
	whisperPath, whisperPresent := ResolveWhisperPath("")
	modelPath, modelPresent := ResolveModelPath("")
	if !whisperPresent || !modelPresent {
		t.Skip("whisper-cli or the Whisper model is not installed")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot resolve the home directory: %v", err)
	}
	samplePath := filepath.Join(home, ".local", "share", "fileforge", "src", "whisper.cpp", "samples", "jfk.wav")
	if _, statErr := os.Stat(samplePath); statErr != nil {
		t.Skipf("JFK sample missing: %v", statErr)
	}
	if _, lookupErr := exec.LookPath("ffmpeg"); lookupErr != nil {
		t.Skip("ffmpeg is not installed")
	}

	dir := t.TempDir()
	request := TranscribeRequest{
		InputPath: samplePath,
		OutputPaths: map[string]string{
			TranscribeFormatSRT: filepath.Join(dir, "jfk.srt"),
			TranscribeFormatVTT: filepath.Join(dir, "jfk.vtt"),
			TranscribeFormatTXT: filepath.Join(dir, "jfk.txt"),
		},
		WhisperPath: whisperPath,
		ModelPath:   modelPath,
		Language:    TranscribeLanguageEnglish,
	}

	result, transcribeErr := Transcribe(context.Background(), nil, request, nil)
	if transcribeErr != nil {
		t.Fatalf("real transcription failed: %v", transcribeErr)
	}
	if result.TextCharacters == 0 || result.Segments == 0 {
		t.Fatalf("expected recognized speech, got %d characters, %d segments", result.TextCharacters, result.Segments)
	}

	txtContent, err := os.ReadFile(request.OutputPaths[TranscribeFormatTXT])
	if err != nil {
		t.Fatalf("read txt output: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(txtContent)), "country") {
		t.Fatalf("expected the JFK transcript to mention country, got %q", string(txtContent))
	}

	for _, format := range []string{TranscribeFormatSRT, TranscribeFormatVTT} {
		info, statErr := os.Stat(request.OutputPaths[format])
		if statErr != nil {
			t.Fatalf("expected a %s output: %v", format, statErr)
		}
		if info.Size() == 0 {
			t.Fatalf("expected a non-empty %s output", format)
		}
	}
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}
