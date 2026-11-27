package engine

import (
	"bufio"
	"context"
	"errors"
	"fileforge-desktop/internal/platform/execx"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/platform/fsx"
)

// Burning subtitles re-encodes the video with libass so the SRT cues become
// pixels. The work happens in a private temp directory: the subtitle file is
// copied there with a fixed relative name, the final video is written to a
// hidden temp file beside the destination, and only a successful render is
// renamed into place. Stale or partial outputs are never published.
const (
	BurnStagePrepare = "burn.prepare"
	BurnStageRender  = "burn.render"

	ErrorCodeBurnRuntimeUnavailable = "BURN_RUNTIME_UNAVAILABLE"
	ErrorCodeBurnFailed             = "BURN_FAILED"
	ErrorCodeBurnOutputFailed       = "BURN_OUTPUT_FAILED"
	ErrorCodeBurnCancelled          = "BURN_CANCELLED"
	ErrorCodeBurnValidation         = "VALIDATION_ERROR"

	burnTempDirPattern = "fileforge-burn-*"
	burnSubtitleName   = "subs.srt"

	// Sizes are libass SRT/ASS default PlayRes (384x288) units: libass scales
	// them with the video height, and 18 is roughly 6% of the frame height.
	burnDefaultFontSize = 18
	burnDefaultMarginV  = 20
	burnDefaultOutline  = 2
	burnDefaultShadow   = 1

	burnDefaultFontWindows = "Arial"
	burnDefaultFontDarwin  = "Helvetica"
	burnDefaultFontOther   = "DejaVu Sans"

	// Bounded stderr prevents a noisy ffmpeg run from returning megabytes of
	// logs to the UI. Only the tail is useful for a failure message.
	burnStderrTailLines = 60
	burnStderrTailBytes = 8 * 1024
	burnScanMaxToken    = 1024 * 1024

	burnProgressStart = 5
	burnProgressLimit = 99
)

// BurnStyle describes the libass overrides applied to every cue. FontSize,
// MarginV, Outline, and Shadow use the default PlayRes units and scale with the
// video height.
type BurnStyle struct {
	FontName string
	FontSize int
	MarginV  int
	Outline  int
	Shadow   int
}

// DefaultBurnStyle returns the platform-appropriate readable defaults: Arial on
// Windows, Helvetica on macOS, and DejaVu Sans elsewhere.
func DefaultBurnStyle() BurnStyle {
	fontName := burnDefaultFontOther
	switch runtime.GOOS {
	case "windows":
		fontName = burnDefaultFontWindows
	case "darwin":
		fontName = burnDefaultFontDarwin
	}

	return BurnStyle{
		FontName: fontName,
		FontSize: burnDefaultFontSize,
		MarginV:  burnDefaultMarginV,
		Outline:  burnDefaultOutline,
		Shadow:   burnDefaultShadow,
	}
}

// BurnRequest describes one subtitle burn. AudioCodec is the codec of the
// first audio stream; compatible codecs are copied, everything else is
// re-encoded to AAC so PCM-in-MP4 containers never fail the run.
type BurnRequest struct {
	InputPath    string
	SubtitlePath string
	OutputPath   string
	AudioCodec   string
	Style        BurnStyle
}

// BurnProgressEvent is one coarse burn update. Percent is 0-100.
type BurnProgressEvent struct {
	Stage   string
	Percent int
	Message string
}

// BurnResult reports the published file and the probed source duration. A zero
// duration means ffprobe could not read it.
type BurnResult struct {
	OutputPath            string
	SourceDurationSeconds float64
}

// BurnError is a typed burn failure with a stable code the tool maps to a
// canonical job error.
type BurnError struct {
	Code    string
	Message string
	Cause   error
}

func (e *BurnError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *BurnError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// BurnCommandRunner runs ffmpeg with its working directory set to dir and
// streams stdout lines while the process runs. stderr is returned bounded for
// failure reporting.
type BurnCommandRunner interface {
	RunBurn(ctx context.Context, dir, name string, args []string, onStdout func(string)) (stderr string, err error)
}

// ExecBurnCommandRunner runs real ffmpeg processes. CommandContext kills the
// process when the context is cancelled.
type ExecBurnCommandRunner struct{}

// RunBurn runs one ffmpeg process with two independent pipes. stdout is scanned
// line by line so progress reaches the UI while rendering; stderr is kept in a
// bounded tail buffer. Both scanners finish before the call returns.
func (r *ExecBurnCommandRunner) RunBurn(ctx context.Context, dir, name string, args []string, onStdout func(string)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	command := execx.CommandContext(ctx, name, args...)
	command.Dir = dir

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter

	if startErr := command.Start(); startErr != nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = stdoutReader.Close()
		_ = stderrReader.Close()
		return "", startErr
	}

	var scanners sync.WaitGroup
	scanners.Add(2)

	go func() {
		defer scanners.Done()
		scanner := bufio.NewScanner(stdoutReader)
		scanner.Buffer(make([]byte, 0, 64*1024), burnScanMaxToken)
		for scanner.Scan() {
			if onStdout != nil {
				onStdout(scanner.Text())
			}
		}
		_ = stdoutReader.Close()
	}()

	stderrTail := &burnOutputBuffer{}
	go func() {
		defer scanners.Done()
		scanner := bufio.NewScanner(stderrReader)
		scanner.Buffer(make([]byte, 0, 64*1024), burnScanMaxToken)
		for scanner.Scan() {
			stderrTail.add(scanner.Text())
		}
		_ = stderrReader.Close()
	}()

	waitErr := command.Wait()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	scanners.Wait()

	return stderrTail.text(), waitErr
}

// burnOutputBuffer keeps the last burnStderrTailLines lines within
// burnStderrTailBytes bytes so memory stays bounded during a long render.
type burnOutputBuffer struct {
	lines []string
	size  int
}

func (b *burnOutputBuffer) add(line string) {
	if len(line) > burnStderrTailBytes {
		line = line[len(line)-burnStderrTailBytes:]
	}
	b.lines = append(b.lines, line)
	b.size += len(line) + 1
	for len(b.lines) > 1 && (len(b.lines) > burnStderrTailLines || b.size > burnStderrTailBytes) {
		b.size -= len(b.lines[0]) + 1
		b.lines = b.lines[1:]
	}
}

func (b *burnOutputBuffer) text() string {
	return boundedBurnOutput(strings.Join(b.lines, "\n"))
}

// boundedBurnOutput trims arbitrary command output to the last 60 lines and the
// last 8 KiB, preferring to start on a line boundary.
func boundedBurnOutput(raw string) string {
	trimmed := strings.TrimRight(raw, "\r\n \t")
	if trimmed == "" {
		return ""
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) > burnStderrTailLines {
		lines = lines[len(lines)-burnStderrTailLines:]
	}

	joined := strings.Join(lines, "\n")
	if len(joined) > burnStderrTailBytes {
		joined = joined[len(joined)-burnStderrTailBytes:]
		if index := strings.IndexByte(joined, '\n'); index >= 0 {
			joined = joined[index+1:]
		}
	}

	return strings.TrimSpace(strings.ToValidUTF8(joined, ""))
}

func resolveBurnRunner(runner BurnCommandRunner) BurnCommandRunner {
	if runner == nil {
		return &ExecBurnCommandRunner{}
	}

	return runner
}

func resolveBurnStyle(style BurnStyle) BurnStyle {
	defaults := DefaultBurnStyle()
	resolved := BurnStyle{
		FontName: strings.TrimSpace(style.FontName),
		FontSize: style.FontSize,
		MarginV:  style.MarginV,
		Outline:  style.Outline,
		Shadow:   style.Shadow,
	}

	if resolved.FontName == "" {
		resolved.FontName = defaults.FontName
	}
	if resolved.FontSize == 0 {
		resolved.FontSize = defaults.FontSize
	}
	if resolved.MarginV == 0 {
		resolved.MarginV = defaults.MarginV
	}
	if resolved.Outline == 0 {
		resolved.Outline = defaults.Outline
	}
	if resolved.Shadow == 0 {
		resolved.Shadow = defaults.Shadow
	}

	return resolved
}

// sanitizeBurnFontName removes the filtergraph separators that would break the
// force_style value. Fonts with commas or quotes fall back to the platform
// default instead of producing an invalid filter.
func sanitizeBurnFontName(fontName string) string {
	replacer := strings.NewReplacer(",", "", "'", "")
	sanitized := strings.TrimSpace(replacer.Replace(fontName))
	if sanitized == "" {
		return replacer.Replace(DefaultBurnStyle().FontName)
	}

	return sanitized
}

func burnAudioCodecCanBeCopied(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "aac", "mp3", "ac3", "eac3", "alac", "opus":
		return true
	default:
		return false
	}
}

// BuildBurnArgs builds the ffmpeg argv for one burn. subtitleName must be a
// relative file name inside the runner working directory: the subtitles filter
// is resolved against cmd.Dir, so no absolute path or shell escaping is needed.
func BuildBurnArgs(req BurnRequest, subtitleName string) []string {
	style := resolveBurnStyle(req.Style)
	fontName := sanitizeBurnFontName(style.FontName)
	subtitleName = strings.TrimSpace(subtitleName)
	if subtitleName == "" {
		subtitleName = burnSubtitleName
	}

	filter := fmt.Sprintf(
		"subtitles=%s:force_style='FontName=%s,FontSize=%d,PrimaryColour=&H00FFFFFF,OutlineColour=&H00000000,BorderStyle=1,Outline=%d,Shadow=%d,Alignment=2,MarginV=%d'",
		subtitleName,
		fontName,
		style.FontSize,
		style.Outline,
		style.Shadow,
		style.MarginV,
	)

	args := withFFmpegGlobalArgs([]string{
		"-y",
		"-i", strings.TrimSpace(req.InputPath),
		"-map", "0:v:0",
		"-map", "0:a?",
		"-vf", filter,
		"-c:v", "libx264",
		"-preset", x264PresetHigh,
		"-crf", "20",
		"-pix_fmt", "yuv420p",
	})

	if burnAudioCodecCanBeCopied(req.AudioCodec) {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}

	args = append(args,
		"-map_metadata", "0",
		"-map_chapters", "0",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		strings.TrimSpace(req.OutputPath),
	)

	return args
}

// parseBurnProgress adapts the shared ffmpeg progress parser to the burn
// callback shape. It accepts out_time_us and out_time_ms (both carry
// microseconds), a manually parsed out_time=HH:MM:SS.ffffff, and progress=end.
// ok is false for every other line.
func parseBurnProgress(line string) (microseconds int64, done bool, ok bool) {
	sample, parsed := ParseFFmpegProgress(line)
	if !parsed {
		return 0, false, false
	}

	return sample.Microseconds, sample.Done, true
}

// parseBurnTimestamp parses ffmpeg's HH:MM:SS.ffffff timestamps into
// microseconds through the shared parser.
func parseBurnTimestamp(value string) (int64, bool) {
	return parseFFmpegTimestamp(value)
}

// burnPercent maps the rendered position to the 5-99 range reserved for the
// render stage. It returns the lower bound when the duration or position is
// unusable, so callers only need to clamp the upper edge.
func burnPercent(microseconds int64, durationSeconds float64) int {
	if microseconds <= 0 || durationSeconds <= 0 || math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) {
		return burnProgressStart
	}

	percent := int(math.Round(float64(microseconds) / (durationSeconds * 1_000_000) * 100))
	if percent < burnProgressStart {
		return burnProgressStart
	}
	if percent > burnProgressLimit {
		return burnProgressLimit
	}

	return percent
}

// Burn validates the request, renders the subtitles with ffmpeg, and publishes
// the result with an atomic rename in the destination directory. Temp files,
// including a partially written output, are removed on every failure.
func Burn(ctx context.Context, runner BurnCommandRunner, ffmpegPath string, req BurnRequest, onProgress func(BurnProgressEvent)) (BurnResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runner = resolveBurnRunner(runner)
	ffmpegPath = strings.TrimSpace(ffmpegPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	if validationErr := validateBurnRequest(req); validationErr != nil {
		return BurnResult{}, validationErr
	}

	// ffmpeg runs with its working directory set to the subtitle workspace, so
	// every path handed to it must be absolute before the command starts.
	inputPath := absoluteBurnPath(strings.TrimSpace(req.InputPath))
	subtitlePath := absoluteBurnPath(strings.TrimSpace(req.SubtitlePath))
	outputPath := absoluteBurnPath(strings.TrimSpace(req.OutputPath))

	duration, _ := inspect.ProbeVideoDuration(ctx, inputPath)

	tempDir, err := os.MkdirTemp("", burnTempDirPattern)
	if err != nil {
		return BurnResult{}, &BurnError{Code: ErrorCodeBurnFailed, Message: "Could not create the burn workspace", Cause: err}
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	subtitleContent, err := os.ReadFile(subtitlePath)
	if err != nil {
		return BurnResult{}, &BurnError{Code: ErrorCodeBurnFailed, Message: "Could not read the subtitle file", Cause: err}
	}
	if writeErr := os.WriteFile(filepath.Join(tempDir, burnSubtitleName), subtitleContent, 0o644); writeErr != nil {
		return BurnResult{}, &BurnError{Code: ErrorCodeBurnFailed, Message: "Could not prepare the subtitle file", Cause: writeErr}
	}

	emitBurnProgress(onProgress, BurnStagePrepare, 2, "Preparing subtitles…")

	tempPath, err := tempOutputPath(outputPath)
	if err != nil {
		return BurnResult{}, &BurnError{Code: ErrorCodeBurnOutputFailed, Message: "The subtitled video could not be saved.", Cause: err}
	}

	published := false
	defer func() {
		if !published {
			removeTempOutput(tempPath)
		}
	}()

	renderReq := BurnRequest{
		InputPath:    inputPath,
		SubtitlePath: subtitlePath,
		OutputPath:   tempPath,
		AudioCodec:   req.AudioCodec,
		Style:        resolveBurnStyle(req.Style),
	}
	args := BuildBurnArgs(renderReq, burnSubtitleName)

	emitBurnProgress(onProgress, BurnStageRender, burnProgressStart, "Burning subtitles into the video…")

	durationKnown := duration > 0
	lastPercent := burnProgressStart
	doneAnnounced := false
	announceDone := func() {
		if doneAnnounced {
			return
		}
		doneAnnounced = true
		emitBurnProgress(onProgress, BurnStageRender, 100, "Subtitles burned.")
	}
	onLine := func(line string) {
		microseconds, done, ok := parseBurnProgress(line)
		if !ok {
			return
		}
		if done {
			announceDone()
			return
		}
		if !durationKnown {
			return
		}

		percent := burnPercent(microseconds, duration)
		if percent == lastPercent {
			return
		}
		lastPercent = percent
		emitBurnProgress(onProgress, BurnStageRender, percent, fmt.Sprintf("Burning subtitles… %d%%", percent))
	}

	stderr, runErr := runner.RunBurn(ctx, tempDir, ffmpegPath, args, onLine)
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return BurnResult{}, cancelledBurnError(ctxErr)
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return BurnResult{}, &BurnError{
				Code:    ErrorCodeBurnRuntimeUnavailable,
				Message: "FFmpeg was not found. Install or repair the local toolchain.",
				Cause:   runErr,
			}
		}

		cause := runErr
		if tail := boundedBurnOutput(stderr); tail != "" {
			cause = fmt.Errorf("%w: %s", runErr, tail)
		}

		return BurnResult{}, &BurnError{
			Code:    ErrorCodeBurnFailed,
			Message: "FFmpeg could not burn the subtitles",
			Cause:   cause,
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return BurnResult{}, cancelledBurnError(ctxErr)
	}

	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		return BurnResult{}, &BurnError{Code: ErrorCodeBurnOutputFailed, Message: "The subtitled video could not be saved.", Cause: chmodErr}
	}
	if publishErr := publishOutput(tempPath, outputPath); publishErr != nil {
		return BurnResult{}, &BurnError{Code: ErrorCodeBurnOutputFailed, Message: "The subtitled video could not be saved.", Cause: publishErr}
	}
	published = true

	announceDone()

	return BurnResult{OutputPath: outputPath, SourceDurationSeconds: duration}, nil
}

func validateBurnRequest(req BurnRequest) *BurnError {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: "inputPath is required"}
	}
	if validationErr := validateBurnRegularFile(inputPath, "input"); validationErr != nil {
		return validationErr
	}

	subtitlePath := strings.TrimSpace(req.SubtitlePath)
	if subtitlePath == "" {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: "subtitlePath is required"}
	}
	if extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(subtitlePath)), "."); extension != "srt" {
		return &BurnError{
			Code:    ErrorCodeBurnValidation,
			Message: fmt.Sprintf("subtitle must be an .srt file: %s", filepath.Base(subtitlePath)),
		}
	}
	if validationErr := validateBurnRegularFile(subtitlePath, "subtitle"); validationErr != nil {
		return validationErr
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: "outputPath is required"}
	}
	if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPath) {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: "output path collides with the input path"}
	}

	outputDir := filepath.Dir(outputPath)
	dirInfo, err := os.Stat(outputDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("output directory does not exist: %s", outputDir), Cause: err}
		}
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("output directory is not accessible: %s", outputDir), Cause: err}
	}
	if !dirInfo.IsDir() {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("output path parent is not a directory: %s", outputDir)}
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: statErr}
	}

	return nil
}

func validateBurnRegularFile(path, label string) *BurnError {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("%s does not exist: %s", label, filepath.Base(path)), Cause: err}
		}
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("%s is not accessible: %s", label, filepath.Base(path)), Cause: err}
	}
	if info.IsDir() {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("%s path is a directory: %s", label, filepath.Base(path))}
	}
	if !info.Mode().IsRegular() {
		return &BurnError{Code: ErrorCodeBurnValidation, Message: fmt.Sprintf("%s is not a regular file: %s", label, filepath.Base(path))}
	}

	return nil
}

func emitBurnProgress(onProgress func(BurnProgressEvent), stage string, percent int, message string) {
	if onProgress == nil {
		return
	}

	onProgress(BurnProgressEvent{Stage: stage, Percent: percent, Message: message})
}

func cancelledBurnError(cause error) *BurnError {
	return &BurnError{Code: ErrorCodeBurnCancelled, Message: "Burning subtitles was canceled.", Cause: cause}
}

func absoluteBurnPath(path string) string {
	if path == "" {
		return path
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return absolute
}
