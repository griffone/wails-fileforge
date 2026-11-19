package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/toolchain"
)

// Local Whisper transcription. The pipeline extracts a mono 16 kHz WAV with
// ffmpeg, runs whisper.cpp, and publishes the requested subtitle/text files
// atomically. No network access and no automatic model downloads: every path
// is resolved locally and reported to the user when missing.
const (
	TranscribeFormatSRT = "srt"
	TranscribeFormatVTT = "vtt"
	TranscribeFormatTXT = "txt"

	TranscribeLanguageAuto    = "auto"
	TranscribeLanguageEnglish = "en"
	TranscribeLanguageSpanish = "es"

	// TranscribeStageExtract, TranscribeStageTranscribe and
	// TranscribeStagePublish are the coarse progress stages reported by the
	// pipeline; whisper.cpp progress is not streamed in this slice.
	TranscribeStageExtract    = "extract"
	TranscribeStageTranscribe = "transcribe"
	TranscribeStagePublish    = "publish"

	ErrorCodeTranscribeRuntimeUnavailable = "TRANSCRIBE_RUNTIME_UNAVAILABLE"
	ErrorCodeTranscribeModelMissing       = "TRANSCRIBE_MODEL_MISSING"
	ErrorCodeTranscribeAudioFailed        = "TRANSCRIBE_AUDIO_FAILED"
	ErrorCodeTranscribeFailed             = "TRANSCRIBE_FAILED"
	ErrorCodeTranscribeOutputFailed       = "TRANSCRIBE_OUTPUT_FAILED"
	ErrorCodeTranscribeCancelled          = "TRANSCRIBE_CANCELLED"
	ErrorCodeTranscribeValidation         = "VALIDATION_ERROR"

	transcribeTempDirPattern  = "fileforge-transcribe-*"
	transcribeTempFilePattern = ".fileforge-transcribe-*.tmp"
	transcribeAudioFileName   = "audio.wav"
	transcribeOutputPrefix    = "transcript"
	transcribeMaxThreads      = 8

	// transcribeMaxCapturedOutputBytes bounds the diagnostic output kept in
	// memory. Whisper can be noisy on long inputs, and a runaway process must
	// never exhaust memory to report an error.
	transcribeMaxCapturedOutputBytes = 8 * 1024 * 1024
)

// TranscriptionWarningNoSpeech is added when Whisper recognized no text. Empty
// speech is still a success: the requested files are published so the user has
// valid (empty) subtitles instead of a hard failure.
const TranscriptionWarningNoSpeech = "No speech was recognized."

var (
	transcriptionInputExtensions = []string{"mp4", "mov", "mkv", "webm", "avi", "mp3", "m4a", "wav", "ogg", "flac"}
	transcriptionFormats         = []string{TranscribeFormatSRT, TranscribeFormatVTT, TranscribeFormatTXT}
	transcriptionLanguages       = []string{TranscribeLanguageAuto, TranscribeLanguageEnglish, TranscribeLanguageSpanish}

	// The directory probes are package variables so tests can simulate a
	// machine without the installed binary or model.
	transcribeUserHomeDir   = os.UserHomeDir
	transcribeUserConfigDir = os.UserConfigDir
	transcribeLookPath      = exec.LookPath

	// managedTranscribePaths resolves the installer-managed whisper.cpp runtime
	// and models. It is a package variable so tests can isolate the resolver
	// from the machine and so this package does not depend on installer state.
	managedTranscribePaths = func() (string, []string) {
		dataDir, err := toolchain.DataDir()
		if err != nil || strings.TrimSpace(dataDir) == "" {
			return "", nil
		}

		return toolchain.ManagedWhisperCLIPath(dataDir), toolchain.ManagedModelPaths(dataDir)
	}
)

// transcribeTimestampLinePattern matches SRT and VTT cue timing lines. Only the
// timestamp line is matched, so cue indexes and spoken text are excluded from
// the character count.
var transcribeTimestampLinePattern = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}[.,]\d{3}\s+-->\s+`)

// SupportedTranscriptionInputExtensions lists the video/audio containers the
// pipeline accepts.
func SupportedTranscriptionInputExtensions() []string {
	return append([]string{}, transcriptionInputExtensions...)
}

// SupportedTranscriptionFormats lists the subtitle/text formats in the order
// outputs are planned and reported.
func SupportedTranscriptionFormats() []string {
	return append([]string{}, transcriptionFormats...)
}

// SupportedTranscriptionLanguages lists the language values accepted by the
// pipeline. The installed model is multilingual.
func SupportedTranscriptionLanguages() []string {
	return append([]string{}, transcriptionLanguages...)
}

// TranscribeCommandRunner runs an external command and returns its combined
// output. stdout and stderr are merged so a failed command can report the tool
// message. It is separate from CommandRunner because the transcription
// pipeline needs the captured output, not just the exit status.
type TranscribeCommandRunner interface {
	Output(ctx context.Context, name string, args []string) ([]byte, error)
}

// StreamingTranscribeCommandRunner is implemented by runners that can surface
// whisper.cpp progress while the command runs. The engine falls back to Output
// when a runner only supports the buffered contract.
type StreamingTranscribeCommandRunner interface {
	RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) ([]byte, error)
}

// ExecTranscribeCommandRunner runs real binaries with exec.CommandContext, so
// context cancellation kills ffmpeg or whisper-cli.
type ExecTranscribeCommandRunner struct{}

func (r *ExecTranscribeCommandRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)

	output := newBoundedTranscribeOutput(transcribeMaxCapturedOutputBytes)
	command.Stdout = output
	command.Stderr = output

	if err := command.Run(); err != nil {
		return output.Bytes(), transcribeCommandError(name, output.Bytes(), err)
	}

	return output.Bytes(), nil
}

// RunStreaming runs a command and calls onLine for every stdout/stderr line.
// whisper.cpp only reports progress when its output is consumed while running;
// buffering until exit would leave the UI stuck at the initial percentage. The
// captured copy is bounded so a noisy engine cannot exhaust memory.
func (r *ExecTranscribeCommandRunner) RunStreaming(ctx context.Context, name string, args []string, onLine func(string)) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)

	pipeReader, pipeWriter := io.Pipe()
	command.Stdout = pipeWriter
	command.Stderr = pipeWriter
	if err := command.Start(); err != nil {
		_ = pipeWriter.Close()
		_ = pipeReader.Close()
		return nil, err
	}

	buffer := newBoundedTranscribeOutput(transcribeMaxCapturedOutputBytes)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pipeReader)
		for scanner.Scan() {
			line := scanner.Text()
			buffer.AppendLine(line)
			if onLine != nil {
				onLine(line)
			}
		}
		_ = pipeReader.Close()
	}()

	waitErr := command.Wait()
	_ = pipeWriter.Close()
	<-done
	output := buffer.Bytes()
	if waitErr != nil {
		return output, transcribeCommandError(name, output, waitErr)
	}

	return output, nil
}

// boundedTranscribeOutput keeps at most max bytes of command output while still
// consuming the full stream, so the child never blocks on a full pipe.
type boundedTranscribeOutput struct {
	max  int
	data []byte
}

func newBoundedTranscribeOutput(maxBytes int) *boundedTranscribeOutput {
	if maxBytes <= 0 {
		maxBytes = transcribeMaxCapturedOutputBytes
	}

	return &boundedTranscribeOutput{max: maxBytes}
}

func (b *boundedTranscribeOutput) Write(p []byte) (int, error) {
	written := len(p)
	if remaining := b.max - len(b.data); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}

	return written, nil
}

// AppendLine stores one line plus its separator within the byte budget.
func (b *boundedTranscribeOutput) AppendLine(line string) {
	if len(b.data) >= b.max {
		return
	}

	payload := line + "\n"
	if remaining := b.max - len(b.data); len(payload) > remaining {
		payload = payload[:remaining]
	}
	b.data = append(b.data, payload...)
}

func (b *boundedTranscribeOutput) Bytes() []byte {
	return b.data
}

func transcribeCommandError(name string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s failed: %w", name, err)
	}

	return fmt.Errorf("%s failed: %w: %s", name, err, message)
}

func resolveTranscribeRunner(runner TranscribeCommandRunner) TranscribeCommandRunner {
	if runner == nil {
		return &ExecTranscribeCommandRunner{}
	}

	return runner
}

// TranscribeRequest describes one media file transcription. OutputPaths maps
// the requested format (srt, vtt, txt) to its final path.
type TranscribeRequest struct {
	InputPath   string
	OutputPaths map[string]string
	WhisperPath string
	ModelPath   string
	Language    string
}

// TranscribeResult reports what Whisper produced. Warnings never fail the run.
type TranscribeResult struct {
	TextCharacters int
	Segments       int
	Warnings       []string
	Language       string
}

// TranscribeProgressEvent is one coarse pipeline update. Percent is 0-100.
type TranscribeProgressEvent struct {
	Stage   string
	Percent int
	Message string
}

// TranscriptionError is a typed transcription failure with a stable code the
// tool maps to a canonical job error.
type TranscriptionError struct {
	Code    string
	Message string
	Cause   error
}

func (e *TranscriptionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *TranscriptionError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// TranscriptionCapabilities is the detected local Whisper runtime: the resolved
// binary and model paths, their presence, the model file name/size and the
// supported languages.
type TranscriptionCapabilities struct {
	WhisperPath     string
	WhisperPresent  bool
	ModelPath       string
	ModelPresent    bool
	ModelName       string
	ModelSizeBytes  int64
	Languages       []string
	DefaultLanguage string
}

// Available reports whether the pipeline can run: both the binary and a
// non-empty model must exist.
func (c TranscriptionCapabilities) Available() bool {
	return c.WhisperPresent && c.ModelPresent
}

// WhisperPathCandidates returns the binary search order: the provided path (a
// Settings override), the installer-managed whisper.cpp runtime, the installed
// local-share path, the per-user config path and whisper-cli from PATH.
func WhisperPathCandidates(providedPath string) []string {
	candidates := make([]string, 0, 6)
	if trimmed := strings.TrimSpace(providedPath); trimmed != "" {
		candidates = append(candidates, filepath.Clean(trimmed))
	}

	if managedWhisperPath, _ := managedTranscribePaths(); strings.TrimSpace(managedWhisperPath) != "" {
		candidates = append(candidates, filepath.Clean(managedWhisperPath))
	}

	if home, err := transcribeUserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "share", "fileforge", "whisper", "whisper-cli"))
	}
	if configDir, err := transcribeUserConfigDir(); err == nil && strings.TrimSpace(configDir) != "" {
		candidates = append(candidates, filepath.Join(configDir, "fileforge", "whisper", "whisper-cli"))
	}
	if path, err := transcribeLookPath("whisper-cli"); err == nil && strings.TrimSpace(path) != "" {
		candidates = append(candidates, path)
	} else {
		candidates = append(candidates, "whisper-cli")
	}

	return dedupeTranscribeCandidates(candidates)
}

// ModelPathCandidates returns the model search order: the provided path (a
// Settings override), the installer-managed models (default first), the
// installed local-share model and the per-user config model.
func ModelPathCandidates(providedPath string) []string {
	candidates := make([]string, 0, 6)
	if trimmed := strings.TrimSpace(providedPath); trimmed != "" {
		candidates = append(candidates, filepath.Clean(trimmed))
	}

	if _, managedModels := managedTranscribePaths(); len(managedModels) > 0 {
		for _, modelPath := range managedModels {
			if strings.TrimSpace(modelPath) != "" {
				candidates = append(candidates, filepath.Clean(modelPath))
			}
		}
	}

	if home, err := transcribeUserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, ".local", "share", "fileforge", "models", "ggml-base.bin"))
	}
	if configDir, err := transcribeUserConfigDir(); err == nil && strings.TrimSpace(configDir) != "" {
		candidates = append(candidates, filepath.Join(configDir, "fileforge", "models", "ggml-base.bin"))
	}

	return dedupeTranscribeCandidates(candidates)
}

// ResolveWhisperPath returns the first existing binary from the search order and
// whether it is present. When nothing exists it returns the first candidate so
// guidance can point at the expected location.
func ResolveWhisperPath(providedPath string) (string, bool) {
	candidates := WhisperPathCandidates(providedPath)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}

	return candidates[0], false
}

// ResolveModelPath returns the first existing non-empty model from the search
// order and whether it is present. When nothing exists it returns the first
// candidate so guidance can point at the expected location.
func ResolveModelPath(providedPath string) (string, bool) {
	candidates := ModelPathCandidates(providedPath)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Size() > 0 {
			return candidate, true
		}
	}

	return candidates[0], false
}

// DetectTranscriptionCapabilities resolves the binary and model without running
// anything. Provided paths win when they exist; otherwise the installed
// defaults are used.
func DetectTranscriptionCapabilities(whisperPath, modelPath string) TranscriptionCapabilities {
	resolvedWhisper, whisperPresent := ResolveWhisperPath(whisperPath)
	resolvedModel, modelPresent := ResolveModelPath(modelPath)

	capabilities := TranscriptionCapabilities{
		WhisperPath:     resolvedWhisper,
		WhisperPresent:  whisperPresent,
		ModelPath:       resolvedModel,
		ModelPresent:    modelPresent,
		Languages:       SupportedTranscriptionLanguages(),
		DefaultLanguage: TranscribeLanguageAuto,
	}

	if modelPresent {
		if info, err := os.Stat(resolvedModel); err == nil && !info.IsDir() {
			capabilities.ModelName = filepath.Base(resolvedModel)
			capabilities.ModelSizeBytes = info.Size()
		}
	}

	return capabilities
}

// NormalizeTranscriptionLanguage lowercases the language and defaults an empty
// value to auto. Unsupported values are a validation error.
func NormalizeTranscriptionLanguage(rawLanguage string) (string, *TranscriptionError) {
	language := strings.ToLower(strings.TrimSpace(rawLanguage))
	if language == "" {
		return TranscribeLanguageAuto, nil
	}

	for _, supported := range transcriptionLanguages {
		if language == supported {
			return language, nil
		}
	}

	return "", &TranscriptionError{
		Code:    ErrorCodeTranscribeValidation,
		Message: "language must be auto, en, or es",
	}
}

// ValidateTranscribeInput checks that inputPath is an existing regular file
// with a supported video/audio extension.
func ValidateTranscribeInput(inputPath string) *TranscriptionError {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: "inputPath is required"}
	}

	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(inputPath)), ".")
	if !containsString(transcriptionInputExtensions, extension) {
		return &TranscriptionError{
			Code:    ErrorCodeTranscribeValidation,
			Message: fmt.Sprintf("input file type is not supported: %s", filepath.Base(inputPath)),
		}
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("input does not exist: %s", filepath.Base(inputPath)), Cause: err}
		}

		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("input is not accessible: %s", filepath.Base(inputPath)), Cause: err}
	}
	if info.IsDir() {
		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath))}
	}

	return nil
}

// ValidateTranscribeDestination checks that dir exists, is a directory and
// accepts writes, so an auto output can be promised safely.
func ValidateTranscribeDestination(dir string) *TranscriptionError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: "output directory is required"}
	}

	return validateTranscribeOutputDir(filepath.Clean(dir))
}

// ValidateTranscribeRequest checks the input, the language and every requested
// output path without running any command. Runtime presence is checked by
// Transcribe itself because it needs the resolved binary/model paths.
func ValidateTranscribeRequest(req TranscribeRequest) *TranscriptionError {
	_, validationErr := normalizeTranscribeRequest(req)
	return validationErr
}

// normalizeTranscribeRequest validates the request and returns the canonical
// form used by the pipeline: trimmed input, lowercase format keys, defaulted
// language and ordered output paths.
func normalizeTranscribeRequest(req TranscribeRequest) (TranscribeRequest, *TranscriptionError) {
	normalized := TranscribeRequest{
		InputPath:   strings.TrimSpace(req.InputPath),
		OutputPaths: make(map[string]string, len(req.OutputPaths)),
		WhisperPath: strings.TrimSpace(req.WhisperPath),
		ModelPath:   strings.TrimSpace(req.ModelPath),
	}

	if validationErr := ValidateTranscribeInput(normalized.InputPath); validationErr != nil {
		return TranscribeRequest{}, validationErr
	}

	language, languageErr := NormalizeTranscriptionLanguage(req.Language)
	if languageErr != nil {
		return TranscribeRequest{}, languageErr
	}
	normalized.Language = language

	if len(req.OutputPaths) == 0 {
		return TranscribeRequest{}, &TranscriptionError{
			Code:    ErrorCodeTranscribeValidation,
			Message: "at least one output format (srt, vtt, txt) is required",
		}
	}

	checkedDirs := make(map[string]struct{}, 2)
	for rawFormat, rawPath := range req.OutputPaths {
		format := strings.ToLower(strings.TrimSpace(rawFormat))
		if !containsString(transcriptionFormats, format) {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: fmt.Sprintf("output format must be srt, vtt, or txt: %s", rawFormat),
			}
		}

		outputPath := filepath.Clean(strings.TrimSpace(rawPath))
		if strings.TrimSpace(rawPath) == "" {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: fmt.Sprintf("output path for %s is required", format),
			}
		}
		if _, duplicate := normalized.OutputPaths[format]; duplicate {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: fmt.Sprintf("output format %s was requested twice", format),
			}
		}
		if outputExtension := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), "."); outputExtension != format {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: fmt.Sprintf("output extension .%s does not match format %s", outputExtension, format),
			}
		}
		if fsx.NormalizePathKey(outputPath) == fsx.NormalizePathKey(normalized.InputPath) {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: "output path collides with the input path",
			}
		}

		if _, statErr := os.Stat(outputPath); statErr == nil {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: fmt.Sprintf("output already exists: %s", outputPath),
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return TranscribeRequest{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeValidation,
				Message: fmt.Sprintf("output path is not accessible: %s", outputPath),
				Cause:   statErr,
			}
		}

		dir := filepath.Dir(outputPath)
		if _, checked := checkedDirs[dir]; !checked {
			if dirErr := validateTranscribeOutputDir(dir); dirErr != nil {
				return TranscribeRequest{}, dirErr
			}
			checkedDirs[dir] = struct{}{}
		}

		normalized.OutputPaths[format] = outputPath
	}

	return normalized, nil
}

func validateTranscribeOutputDir(dir string) *TranscriptionError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}

		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}
	if !info.IsDir() {
		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	probe, probeErr := os.CreateTemp(dir, transcribeTempFilePattern)
	if probeErr != nil {
		return &TranscriptionError{Code: ErrorCodeTranscribeValidation, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: probeErr}
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	return nil
}

// Transcribe runs the local pipeline for one media file: extract audio with
// ffmpeg, transcribe with whisper.cpp, then publish the requested outputs. Temp
// files and any final files written by this run are removed on failure or
// cancellation.
func Transcribe(ctx context.Context, runner TranscribeCommandRunner, req TranscribeRequest, onProgress func(TranscribeProgressEvent)) (TranscribeResult, *TranscriptionError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return TranscribeResult{}, cancelledTranscriptionError("transcription canceled", ctx.Err())
	}

	normalized, validationErr := normalizeTranscribeRequest(req)
	if validationErr != nil {
		return TranscribeResult{}, validationErr
	}

	capabilities := DetectTranscriptionCapabilities(normalized.WhisperPath, normalized.ModelPath)
	if !capabilities.WhisperPresent {
		return TranscribeResult{}, &TranscriptionError{
			Code:    ErrorCodeTranscribeRuntimeUnavailable,
			Message: TranscriptionWhisperMissingMessage(capabilities),
		}
	}
	if !capabilities.ModelPresent {
		return TranscribeResult{}, &TranscriptionError{
			Code:    ErrorCodeTranscribeModelMissing,
			Message: TranscriptionModelMissingMessage(capabilities),
		}
	}

	runner = resolveTranscribeRunner(runner)

	tempDir, err := os.MkdirTemp("", transcribeTempDirPattern)
	if err != nil {
		return TranscribeResult{}, &TranscriptionError{Code: ErrorCodeTranscribeAudioFailed, Message: "could not create the transcription workspace", Cause: err}
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	audioPath := filepath.Join(tempDir, transcribeAudioFileName)
	emitTranscribeProgress(onProgress, TranscribeStageExtract, 10, "Extracting audio…")
	if _, runErr := runner.Output(ctx, "ffmpeg", withFFmpegGlobalArgs([]string{
		"-y",
		"-i", normalized.InputPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "pcm_s16le",
		audioPath,
	})); runErr != nil {
		if ctx.Err() != nil {
			return TranscribeResult{}, cancelledTranscriptionError("transcription canceled", ctx.Err())
		}

		return TranscribeResult{}, &TranscriptionError{
			Code:    ErrorCodeTranscribeAudioFailed,
			Message: "failed to extract audio from the input media",
			Cause:   runErr,
		}
	}
	if ctx.Err() != nil {
		return TranscribeResult{}, cancelledTranscriptionError("transcription canceled", ctx.Err())
	}
	if _, statErr := os.Stat(audioPath); statErr != nil {
		return TranscribeResult{}, &TranscriptionError{
			Code:    ErrorCodeTranscribeAudioFailed,
			Message: "the extracted audio file is missing",
			Cause:   statErr,
		}
	}

	formats := orderedTranscriptionFormats(normalized.OutputPaths)
	tempPrefix := filepath.Join(tempDir, transcribeOutputPrefix)
	whisperArgs := []string{
		"-m", capabilities.ModelPath,
		"-f", audioPath,
		"-l", normalized.Language,
		"-t", strconv.Itoa(transcribeThreads()),
	}
	for _, format := range formats {
		switch format {
		case TranscribeFormatSRT:
			whisperArgs = append(whisperArgs, "-osrt")
		case TranscribeFormatVTT:
			whisperArgs = append(whisperArgs, "-ovtt")
		case TranscribeFormatTXT:
			whisperArgs = append(whisperArgs, "-otxt")
		}
	}
	whisperArgs = append(whisperArgs, "-pp", "-of", tempPrefix)

	emitTranscribeProgress(onProgress, TranscribeStageTranscribe, 20, "Transcribing audio with Whisper…")
	whisperProgress := func(line string) {
		percent, ok := parseWhisperProgress(line)
		if !ok || onProgress == nil {
			return
		}
		overall := 20 + int(math.Round(float64(percent)*0.7))
		if overall > 89 {
			overall = 89
		}
		onProgress(TranscribeProgressEvent{
			Stage:   TranscribeStageTranscribe,
			Percent: overall,
			Message: fmt.Sprintf("Transcribing audio… %d%%", percent),
		})
	}
	var whisperErr error
	if streamer, ok := runner.(StreamingTranscribeCommandRunner); ok {
		_, whisperErr = streamer.RunStreaming(ctx, capabilities.WhisperPath, whisperArgs, whisperProgress)
	} else {
		_, whisperErr = runner.Output(ctx, capabilities.WhisperPath, whisperArgs)
	}
	if whisperErr != nil {
		if ctx.Err() != nil {
			return TranscribeResult{}, cancelledTranscriptionError("transcription canceled", ctx.Err())
		}

		return TranscribeResult{}, &TranscriptionError{
			Code:    ErrorCodeTranscribeFailed,
			Message: "Whisper could not transcribe the audio",
			Cause:   whisperErr,
		}
	}
	if ctx.Err() != nil {
		return TranscribeResult{}, cancelledTranscriptionError("transcription canceled", ctx.Err())
	}

	contents := make(map[string]string, len(formats))
	for _, format := range formats {
		content, readErr := os.ReadFile(tempPrefix + "." + format)
		if readErr != nil {
			if !errors.Is(readErr, os.ErrNotExist) {
				return TranscribeResult{}, &TranscriptionError{
					Code:    ErrorCodeTranscribeOutputFailed,
					Message: fmt.Sprintf("failed to read the generated %s output", format),
					Cause:   readErr,
				}
			}
			contents[format] = emptyTranscriptionContent(format)
			continue
		}

		contents[format] = string(content)
	}

	result := TranscribeResult{Language: normalized.Language}
	textFormat, textContent := transcriptionTextSource(contents, formats)
	if textFormat == TranscribeFormatTXT {
		result.TextCharacters = countTranscriptionCharacters(textContent)
	} else if textFormat != "" {
		result.TextCharacters = countTranscriptionCharacters(subtitleSpeechText(textContent))
	}
	result.Segments = transcriptionSegmentCount(textFormat, textContent)
	if result.TextCharacters == 0 {
		result.Warnings = append(result.Warnings, TranscriptionWarningNoSpeech)
	}

	if ctx.Err() != nil {
		return TranscribeResult{}, cancelledTranscriptionError("transcription canceled", ctx.Err())
	}

	emitTranscribeProgress(onProgress, TranscribeStagePublish, 90, "Saving the transcription…")
	published := make([]string, 0, len(formats))
	for _, format := range formats {
		outputPath := normalized.OutputPaths[format]
		if publishErr := publishTranscriptionFile(outputPath, contents[format]); publishErr != nil {
			for _, publishedPath := range published {
				_ = os.Remove(publishedPath)
			}

			return TranscribeResult{}, &TranscriptionError{
				Code:    ErrorCodeTranscribeOutputFailed,
				Message: fmt.Sprintf("failed to write the %s output", format),
				Cause:   publishErr,
			}
		}

		published = append(published, outputPath)
	}

	return result, nil
}

var whisperProgressPattern = regexp.MustCompile(`progress\s*=\s*(\d+)%`)

// parseWhisperProgress extracts the percentage from whisper.cpp's
// `whisper_print_progress_callback: progress = 48%` output.
func parseWhisperProgress(line string) (int, bool) {
	matches := whisperProgressPattern.FindStringSubmatch(line)
	if len(matches) != 2 {
		return 0, false
	}
	value, err := strconv.Atoi(matches[1])
	if err != nil || value < 0 || value > 100 {
		return 0, false
	}

	return value, true
}

// TranscriptionWhisperMissingMessage explains where whisper-cli was expected
// and how to fix it.
func TranscriptionWhisperMissingMessage(capabilities TranscriptionCapabilities) string {
	return fmt.Sprintf(
		"whisper-cli was not found. Install whisper.cpp or choose the binary in Settings (expected at %s).",
		capabilities.WhisperPath,
	)
}

// TranscriptionModelMissingMessage explains where the model was expected and
// how to fix it. The app never downloads models automatically.
func TranscriptionModelMissingMessage(capabilities TranscriptionCapabilities) string {
	return fmt.Sprintf(
		"no Whisper model was found. Download a ggml model or choose it in Settings (expected at %s).",
		capabilities.ModelPath,
	)
}

func orderedTranscriptionFormats(outputPaths map[string]string) []string {
	formats := make([]string, 0, len(outputPaths))
	for _, format := range transcriptionFormats {
		if _, requested := outputPaths[format]; requested {
			formats = append(formats, format)
		}
	}

	return formats
}

func emptyTranscriptionContent(format string) string {
	if format == TranscribeFormatVTT {
		return "WEBVTT\n\n"
	}

	return ""
}

// transcriptionTextSource picks the transcript used for the result counts: TXT
// first, then SRT, then VTT. VTT is the last resort so a VTT-only request still
// reports meaningful counts.
func transcriptionTextSource(contents map[string]string, formats []string) (string, string) {
	for _, preferred := range []string{TranscribeFormatTXT, TranscribeFormatSRT, TranscribeFormatVTT} {
		for _, format := range formats {
			if format == preferred {
				return format, contents[format]
			}
		}
	}

	return "", ""
}

// subtitleSpeechText drops WEBVTT headers, cue indexes and timing lines so the
// character count reflects the recognized speech.
func subtitleSpeechText(content string) string {
	var builder strings.Builder
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.EqualFold(trimmed, "WEBVTT") {
			continue
		}
		if transcribeTimestampLinePattern.MatchString(trimmed) {
			continue
		}
		if isDigitsOnly(trimmed) {
			continue
		}

		builder.WriteString(trimmed)
	}

	return builder.String()
}

func transcriptionSegmentCount(textFormat, content string) int {
	switch textFormat {
	case TranscribeFormatSRT, TranscribeFormatVTT:
		segments := 0
		for _, line := range strings.Split(content, "\n") {
			if transcribeTimestampLinePattern.MatchString(strings.TrimSpace(line)) {
				segments++
			}
		}

		return segments
	case TranscribeFormatTXT:
		segments := 0
		for _, line := range strings.Split(content, "\n") {
			if strings.TrimSpace(line) != "" {
				segments++
			}
		}

		return segments
	default:
		return 0
	}
}

// countTranscriptionCharacters counts non-whitespace characters, matching the
// OCR result summary.
func countTranscriptionCharacters(text string) int {
	count := 0
	for _, char := range text {
		if !unicode.IsSpace(char) {
			count++
		}
	}

	return count
}

// publishTranscriptionFile writes content to outputPath atomically: a temp file
// in the destination folder, then a rename. An existing destination is never
// overwritten.
func publishTranscriptionFile(outputPath, content string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), transcribeTempFilePattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, writeErr := tempFile.WriteString(content); writeErr != nil {
		_ = tempFile.Close()
		return writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return closeErr
	}

	// CreateTemp uses 0600; publish a readable file like the other tools.
	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		return chmodErr
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return fmt.Errorf("output already exists: %s", outputPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	return os.Rename(tempPath, outputPath)
}

func transcribeThreads() int {
	threads := runtime.NumCPU()
	if threads > transcribeMaxThreads {
		threads = transcribeMaxThreads
	}
	if threads < 1 {
		threads = 1
	}

	return threads
}

func emitTranscribeProgress(onProgress func(TranscribeProgressEvent), stage string, percent int, message string) {
	if onProgress == nil {
		return
	}

	onProgress(TranscribeProgressEvent{Stage: stage, Percent: percent, Message: message})
}

func cancelledTranscriptionError(message string, cause error) *TranscriptionError {
	return &TranscriptionError{Code: ErrorCodeTranscribeCancelled, Message: message, Cause: cause}
}

func dedupeTranscribeCandidates(candidates []string) []string {
	unique := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}

		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	if len(unique) == 0 {
		unique = append(unique, "whisper-cli")
	}

	return unique
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}
