package image

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"fileforge-desktop/internal/toolchain"
)

// Background removal runs rembg 2.0.87 inside the dedicated managed virtual
// environment and a pinned ONNX model. One interpreter handles the whole batch
// so the model loads once; the embedded bridge reports JSON lines and the engine
// validates and publishes every PNG atomically.
const (
	BackgroundRemoveCodeRuntimeUnavailable = "IMAGE_BACKGROUND_RUNTIME_UNAVAILABLE"
	BackgroundRemoveCodeModelUnavailable   = "IMAGE_BACKGROUND_MODEL_UNAVAILABLE"
	BackgroundRemoveCodeInvalidInput       = "IMAGE_BACKGROUND_INPUT_INVALID"
	BackgroundRemoveCodeInputNotFound      = "IMAGE_BACKGROUND_INPUT_NOT_FOUND"
	BackgroundRemoveCodeInputUnsupported   = "IMAGE_BACKGROUND_INPUT_UNSUPPORTED"
	BackgroundRemoveCodeInvalidOption      = "IMAGE_BACKGROUND_OPTION_INVALID"
	BackgroundRemoveCodeOutputInvalid      = "IMAGE_BACKGROUND_OUTPUT_INVALID"
	BackgroundRemoveCodeOutputExists       = "IMAGE_BACKGROUND_OUTPUT_ALREADY_EXISTS"
	BackgroundRemoveCodeProcessFailed      = "IMAGE_BACKGROUND_PROCESS_FAILED"
	BackgroundRemoveCodeVerifyFailed       = "IMAGE_BACKGROUND_VERIFY_FAILED"
	BackgroundRemoveCodePublishFailed      = "IMAGE_BACKGROUND_PUBLISH_FAILED"
	BackgroundRemoveCodeBatchFailed        = "IMAGE_BACKGROUND_BATCH_FAILED"
	BackgroundRemoveCodeCancelled          = "IMAGE_BACKGROUND_CANCELLED"
)

const (
	rembgTempDirPattern    = "fileforge-rembg-*"
	rembgTempFilePattern   = ".fileforge-rembg-*.tmp"
	rembgWriteProbePattern = ".fileforge-rembg-writecheck-*.tmp"
	rembgBridgeFileName    = "rembg_bridge.py"

	// rembgMinimumModelBytes rejects empty or truncated ONNX files before an
	// interpreter starts. The toolchain installer verifies the exact pinned
	// size and MD5; the engine only needs a plausibility floor so a partial
	// download never reaches onnxruntime (and never triggers pooch's network
	// fallback).
	rembgMinimumModelBytes int64 = 64 << 20

	// rembgStderrMaxLines and rembgStderrMaxBytes bound the stderr tail copied
	// into an error, so a chatty interpreter never floods the response.
	rembgStderrMaxLines  = 60
	rembgStderrMaxBytes  = 8 * 1024
	rembgScannerMaxBytes = 1024 * 1024
	rembgMaxCauseRunes   = 300

	// rembgChunkMaxChars and rembgChunkMaxItems bound one bridge argv. Windows
	// caps a command line at 32,767 characters; 10,000 input-path characters
	// leaves room for the interpreter, bridge, flags and the model path.
	rembgChunkMaxChars = 10_000
	rembgChunkMaxItems = 100

	pngColorTypeRGB  = 2
	pngColorTypeRGBA = 6
)

// errRembgNoTransparentPixels is the actionable failure for a cutout that kept
// every pixel opaque: the requested transparent output is impossible.
var errRembgNoTransparentPixels = errors.New("No transparent pixels were found; try a background color.")

// rembgRenameFile and rembgRemoveFile are package seams so tests can simulate
// publication and rollback failures. Production always uses the os functions.
var (
	rembgRenameFile = os.Rename
	rembgRemoveFile = os.Remove
)

// rembgBridgeSource is the Python bridge written to the workspace on every run.
//
//go:embed rembg_bridge.py
var rembgBridgeSource []byte

// BackgroundRemoveOptions describes one background removal pass. BackgroundColor
// is empty for a transparent RGBA PNG or a #RRGGBB color to composite instead.
type BackgroundRemoveOptions struct {
	Model           string
	BackgroundColor string
}

// BackgroundRemoveItem is one input/output pair. Outputs are planned PNG paths.
type BackgroundRemoveItem struct {
	InputPath  string
	OutputPath string
}

// BackgroundRemoveOutcome reports one published PNG and its pixel size.
type BackgroundRemoveOutcome struct {
	OutputPath string
	Width      int
	Height     int
}

// BackgroundRemoveItemFailure is one per-item failure reported by the bridge or
// by publication. Message is user-facing; Cause keeps the raw error.
type BackgroundRemoveItemFailure struct {
	Index      int
	InputPath  string
	OutputPath string
	Code       string
	Message    string
	Cause      error
}

// BackgroundRemoveError is a typed failure with a stable IMAGE_BACKGROUND_*
// code. A batch with some failed items also carries Failures.
type BackgroundRemoveError struct {
	Code     string
	Message  string
	Details  map[string]any
	Cause    error
	Failures []BackgroundRemoveItemFailure
}

func (e *BackgroundRemoveError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *BackgroundRemoveError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// RembgCommandRunner runs the bridge and streams its stdout lines. The engine
// owns parsing, validation and publication; the runner only executes Python.
//
// Contract: onLine is invoked serially from a single goroutine, never
// concurrently. RunRembg must return only after the last callback completed and
// the process is fully drained, because the engine publishes outputs and mutates
// per-item state from the callback.
type RembgCommandRunner interface {
	RunRembg(ctx context.Context, dir, pythonPath string, args []string, env []string, onLine func(string)) error
}

// RembgCommandError keeps the bounded stderr tail next to the process error.
type RembgCommandError struct {
	Err    error
	Stderr string
}

func (e *RembgCommandError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Stderr
	}
	if strings.TrimSpace(e.Stderr) == "" {
		return e.Err.Error()
	}

	return fmt.Sprintf("%v: %s", e.Err, e.Stderr)
}

func (e *RembgCommandError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// ExecRembgCommandRunner runs real interpreters with exec.CommandContext, so
// context cancellation kills the process and the model stops loading.
type ExecRembgCommandRunner struct{}

func (r *ExecRembgCommandRunner) RunRembg(ctx context.Context, dir, pythonPath string, args []string, env []string, onLine func(string)) error {
	command := exec.CommandContext(ctx, pythonPath, args...)
	command.Dir = dir
	if len(env) > 0 {
		command.Env = env
	}

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter

	if startErr := command.Start(); startErr != nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = stdoutReader.Close()
		_ = stderrReader.Close()

		return &RembgCommandError{Err: startErr}
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdoutReader)
		scanner.Buffer(make([]byte, 0, 64*1024), rembgScannerMaxBytes)
		for scanner.Scan() {
			if onLine != nil {
				onLine(scanner.Text())
			}
		}
		_ = stdoutReader.Close()
	}()

	tail := &rembgStderrTail{maxLines: rembgStderrMaxLines, maxBytes: rembgStderrMaxBytes}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderrReader)
		scanner.Buffer(make([]byte, 0, 64*1024), rembgScannerMaxBytes)
		for scanner.Scan() {
			tail.add(scanner.Text())
		}
		_ = stderrReader.Close()
	}()

	waitErr := command.Wait()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	<-stdoutDone
	<-stderrDone

	if waitErr != nil {
		return &RembgCommandError{Err: waitErr, Stderr: tail.String()}
	}

	return nil
}

// rembgStderrTail keeps the last stderr lines and caps the rendered text.
type rembgStderrTail struct {
	lines    []string
	maxLines int
	maxBytes int
}

func (t *rembgStderrTail) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
}

func (t *rembgStderrTail) String() string {
	text := strings.TrimSpace(strings.Join(t.lines, "\n"))
	if len(text) <= t.maxBytes {
		return text
	}

	start := len(text) - t.maxBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}

	return strings.TrimSpace(text[start:])
}

// rembgBridgeEvent is one JSON line emitted by rembg_bridge.py.
type rembgBridgeEvent struct {
	Event   string `json:"event"`
	Index   int    `json:"index"`
	Total   int    `json:"total"`
	Output  string `json:"output"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Message string `json:"message"`
}

// rembgItemState tracks what the bridge reported for one planned item.
type rembgItemState struct {
	reported bool
	outcome  BackgroundRemoveOutcome
	failure  *BackgroundRemoveItemFailure
}

// RemoveBackgrounds validates the managed runtime and the batch, splits the
// batch into bridge-sized chunks and publishes each validated PNG atomically.
//
// A chunk is bounded by a cumulative input-path character budget and an item
// count, so one bridge argv stays below the Windows command-line limit. Each
// chunk starts its own interpreter (the model is reloaded per chunk); bridge
// event indices are mapped back to global item positions and progress stays
// global and monotonic.
//
// Publication policy: an item is published only after its PNG exists, decodes,
// matches the reported size and matches the requested color model (RGBA with at
// least one transparent pixel, or RGB when a background color was requested). A
// destination is never overwritten. Any per-item failure, process failure or
// cancellation removes every output published by this call across all chunks, so
// a failed or cancelled job leaves no partial results (publish-or-nothing per
// job). workingDir is the parent folder of the temporary workspace; an empty
// value uses the OS temp directory.
func RemoveBackgrounds(
	ctx context.Context,
	runner RembgCommandRunner,
	pythonPath, modelPath, workingDir string,
	items []BackgroundRemoveItem,
	opts BackgroundRemoveOptions,
	onProgress func(current, total int),
) ([]BackgroundRemoveOutcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, newBackgroundRemoveCancelled(ctx.Err())
	}
	if runner == nil {
		runner = &ExecRembgCommandRunner{}
	}

	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = toolchain.DefaultRembgModel
	}

	background, hasBackground, colorOK := normalizeBackgroundColor(opts.BackgroundColor)
	if !colorOK {
		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeInvalidOption,
			Message: "backgroundColor must use the #RRGGBB format",
			Details: map[string]any{"backgroundColor": opts.BackgroundColor},
		}
	}

	resolvedPython, pythonErr := validateRembgPythonPath(pythonPath)
	if pythonErr != nil {
		return nil, pythonErr
	}
	_, home, modelErr := validateRembgModelPath(modelPath)
	if modelErr != nil {
		return nil, modelErr
	}

	if len(items) == 0 {
		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeInvalidInput,
			Message: "at least one input image is required",
		}
	}
	if preflightErr := preflightBackgroundRemoveItems(items); preflightErr != nil {
		return nil, preflightErr
	}

	workspace, workspaceErr := os.MkdirTemp(strings.TrimSpace(workingDir), rembgTempDirPattern)
	if workspaceErr != nil {
		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeProcessFailed,
			Message: "could not create the background removal workspace",
			Cause:   workspaceErr,
		}
	}
	defer func() {
		_ = os.RemoveAll(workspace)
	}()
	if absolute, absErr := filepath.Abs(workspace); absErr == nil {
		workspace = absolute
	}

	bridgePath := filepath.Join(workspace, rembgBridgeFileName)
	if writeErr := os.WriteFile(bridgePath, rembgBridgeSource, 0o644); writeErr != nil {
		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeProcessFailed,
			Message: "could not write the background removal bridge",
			Cause:   writeErr,
		}
	}

	env := rembgChildEnv(resolvedPython, home)

	states := make([]rembgItemState, len(items))
	published := make([]string, 0, len(items))
	chunks := rembgItemChunks(items)

	var processDetails map[string]any
	lastProgress := 0
	failed := false

	for chunkIndex, chunk := range chunks {
		if failed {
			break
		}

		chunkOutDir := filepath.Join(workspace, fmt.Sprintf("out-%d", chunkIndex))
		if mkdirErr := os.Mkdir(chunkOutDir, 0o755); mkdirErr != nil {
			chunkErr := &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeProcessFailed,
				Message: "could not create the background removal workspace output folder",
				Cause:   mkdirErr,
			}

			return nil, rembgErrorWithCleanup(chunkErr, published)
		}

		chunkInputs := make([]string, 0, chunk.end-chunk.start)
		for index := chunk.start; index < chunk.end; index++ {
			chunkInputs = append(chunkInputs, absoluteImagePath(strings.TrimSpace(items[index].InputPath)))
		}

		args := []string{bridgePath, "--model", model, "--out-dir", chunkOutDir}
		if hasBackground {
			args = append(args, "--background", background)
		}
		args = append(args, chunkInputs...)

		chunkItemCount := chunk.end - chunk.start
		onLine := func(rawLine string) {
			line := strings.TrimSpace(rawLine)
			if line == "" || !strings.HasPrefix(line, "{") {
				return
			}

			var event rembgBridgeEvent
			if unmarshalErr := json.Unmarshal([]byte(line), &event); unmarshalErr != nil {
				return
			}
			if event.Index < 0 || event.Index >= chunkItemCount {
				return
			}

			globalIndex := chunk.start + event.Index
			state := &states[globalIndex]
			item := items[globalIndex]

			switch event.Event {
			case "progress":
				current := globalIndex + 1
				if current <= lastProgress {
					return
				}
				lastProgress = current
				if onProgress != nil {
					onProgress(current, len(items))
				}
			case "done":
				if state.reported {
					return
				}
				state.reported = true

				width, height, verifyErr := verifyRembgBridgeOutput(event, chunkOutDir, hasBackground)
				if verifyErr != nil {
					state.failure = &BackgroundRemoveItemFailure{
						Index:      globalIndex,
						InputPath:  item.InputPath,
						OutputPath: item.OutputPath,
						Code:       BackgroundRemoveCodeVerifyFailed,
						Message:    rembgVerifyFailureMessage(item, verifyErr),
						Cause:      verifyErr,
					}
					return
				}

				if publishErr := publishBackgroundRemovePNG(event.Output, item.OutputPath); publishErr != nil {
					state.failure = &BackgroundRemoveItemFailure{
						Index:      globalIndex,
						InputPath:  item.InputPath,
						OutputPath: item.OutputPath,
						Code:       publishErr.Code,
						Message:    publishErr.Message,
						Cause:      publishErr.Cause,
					}
					return
				}

				published = append(published, item.OutputPath)
				state.outcome = BackgroundRemoveOutcome{
					OutputPath: item.OutputPath,
					Width:      width,
					Height:     height,
				}
			case "error":
				if state.reported {
					return
				}
				state.reported = true
				state.failure = &BackgroundRemoveItemFailure{
					Index:      globalIndex,
					InputPath:  item.InputPath,
					OutputPath: item.OutputPath,
					Code:       BackgroundRemoveCodeProcessFailed,
					Message:    fmt.Sprintf("Could not remove the background from %s.", filepath.Base(item.InputPath)),
					Cause:      errors.New(rembgBoundMessage(event.Message, rembgMaxCauseRunes)),
				}
			}
		}

		runErr := runner.RunRembg(ctx, workspace, resolvedPython, args, env, onLine)

		if cancelledBy, cancelled := rembgCancellationCause(ctx, runErr); cancelled {
			cancelledErr := newBackgroundRemoveCancelled(cancelledBy)

			return nil, rembgErrorWithCleanup(cancelledErr, published)
		}

		if runErr != nil && errors.Is(runErr, exec.ErrNotFound) {
			runtimeErr := &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeRuntimeUnavailable,
				Message: fmt.Sprintf("the background removal Python interpreter could not run: %s", resolvedPython),
				Details: map[string]any{"pythonPath": resolvedPython},
				Cause:   runErr,
			}

			return nil, rembgErrorWithCleanup(runtimeErr, published)
		}

		var processCause error
		if runErr != nil {
			processCause = runErr
			details := map[string]any{"pythonPath": resolvedPython}
			var commandErr *RembgCommandError
			if errors.As(runErr, &commandErr) && strings.TrimSpace(commandErr.Stderr) != "" {
				details["stderr"] = commandErr.Stderr
			}
			processDetails = details
		}

		chunkFailed := false
		for index := chunk.start; index < chunk.end; index++ {
			state := &states[index]
			switch {
			case state.failure != nil:
				chunkFailed = true
			case state.outcome.OutputPath != "":
				// Validated and published.
			default:
				state.failure = &BackgroundRemoveItemFailure{
					Index:      index,
					InputPath:  items[index].InputPath,
					OutputPath: items[index].OutputPath,
					Code:       BackgroundRemoveCodeProcessFailed,
					Message:    fmt.Sprintf("The background removal engine produced no result for %s.", filepath.Base(items[index].InputPath)),
					Cause:      processCause,
				}
				chunkFailed = true
			}
		}
		if chunkFailed {
			failed = true
		}
	}

	if failed {
		cleanupErrors := removePublishedBackgroundOutputs(published)
		details := rembgCloneDetails(processDetails)
		if len(cleanupErrors) > 0 {
			details["cleanupErrors"] = cleanupErrors
		}

		return nil, rembgItemFailuresError(rembgRollbackFailures(items, states), len(items), details)
	}

	outcomes := make([]BackgroundRemoveOutcome, 0, len(items))
	for _, state := range states {
		if state.outcome.OutputPath != "" {
			outcomes = append(outcomes, state.outcome)
		}
	}

	return outcomes, nil
}

// rembgItemChunk is one contiguous global item range handled by one bridge
// process.
type rembgItemChunk struct {
	start int
	end   int
}

// rembgItemChunks splits items into argv-sized chunks. A single path longer than
// the budget still gets its own chunk, so validation always has a bounded chunk.
func rembgItemChunks(items []BackgroundRemoveItem) []rembgItemChunk {
	chunks := make([]rembgItemChunk, 0, len(items)/rembgChunkMaxItems+1)
	for start := 0; start < len(items); {
		end := start
		chars := 0
		for end < len(items) && end-start < rembgChunkMaxItems {
			length := len(strings.TrimSpace(items[end].InputPath))
			if end > start && chars+length > rembgChunkMaxChars {
				break
			}
			chars += length
			end++
		}
		if end == start {
			end = start + 1
		}

		chunks = append(chunks, rembgItemChunk{start: start, end: end})
		start = end
	}

	return chunks
}

// rembgRollbackFailures reports every item after a job-wide rollback: the
// original per-item failure, or a clear not-kept/not-processed result for items
// whose output was removed or that never ran.
func rembgRollbackFailures(items []BackgroundRemoveItem, states []rembgItemState) []BackgroundRemoveItemFailure {
	failures := make([]BackgroundRemoveItemFailure, 0, len(items))
	for index, item := range items {
		state := states[index]
		if state.failure != nil {
			failures = append(failures, *state.failure)
			continue
		}

		message := "Not processed because the batch failed."
		if state.outcome.OutputPath != "" {
			message = "Not kept because the batch failed."
		}
		failures = append(failures, BackgroundRemoveItemFailure{
			Index:      index,
			InputPath:  item.InputPath,
			OutputPath: item.OutputPath,
			Code:       BackgroundRemoveCodeBatchFailed,
			Message:    message,
		})
	}

	return failures
}

// ValidateBackgroundRemoveInput checks that inputPath is an existing regular
// image with a supported extension. It never decodes pixels.
func ValidateBackgroundRemoveInput(inputPath string) *BackgroundRemoveError {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeInvalidInput,
			Message: "inputPath is required",
		}
	}

	info, statErr := os.Stat(trimmed)
	if statErr != nil {
		message := fmt.Sprintf("%s cannot be read.", filepath.Base(trimmed))
		if errors.Is(statErr, os.ErrNotExist) {
			message = fmt.Sprintf("%s was not found.", filepath.Base(trimmed))
		}

		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeInputNotFound,
			Message: message,
			Details: map[string]any{"inputPath": trimmed},
			Cause:   statErr,
		}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeInputUnsupported,
			Message: fmt.Sprintf("%s is not a regular image file.", filepath.Base(trimmed)),
			Details: map[string]any{"inputPath": trimmed},
		}
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(trimmed), "."))
	if !rembgSupportsInputExtension(extension) {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeInputUnsupported,
			Message: fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(trimmed), strings.Join(imageInputExtensions, ", ")),
			Details: map[string]any{"inputPath": trimmed, "extension": extension},
		}
	}

	return nil
}

// preflightBackgroundRemoveItems validates every item before Python starts: a
// batch process is only spawned when all destinations are safe to publish.
func preflightBackgroundRemoveItems(items []BackgroundRemoveItem) *BackgroundRemoveError {
	inputKeys := make(map[string]int, len(items))
	for index, item := range items {
		inputPath := strings.TrimSpace(item.InputPath)
		if inputErr := ValidateBackgroundRemoveInput(inputPath); inputErr != nil {
			return rembgWithIndex(inputErr, index)
		}

		inputKeys[rembgPathKey(inputPath)] = index
	}

	outputKeys := make(map[string]int, len(items))
	checkedDirs := make(map[string]struct{}, len(items))
	for index, item := range items {
		outputPath := strings.TrimSpace(item.OutputPath)
		if outputPath == "" {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputInvalid,
				Message: "outputPath is required",
				Details: map[string]any{"index": index, "inputPath": item.InputPath},
			}
		}
		if !strings.EqualFold(filepath.Ext(outputPath), ".png") {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputInvalid,
				Message: "outputPath must use .png extension",
				Details: map[string]any{"index": index, "outputPath": outputPath},
			}
		}

		key := rembgPathKey(outputPath)
		if inputIndex, collides := inputKeys[key]; collides {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputInvalid,
				Message: fmt.Sprintf("output path collides with input %s.", filepath.Base(items[inputIndex].InputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath, "inputIndex": inputIndex},
			}
		}
		if otherIndex, duplicate := outputKeys[key]; duplicate {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputInvalid,
				Message: fmt.Sprintf("output path is planned twice: %s.", filepath.Base(outputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath, "duplicateIndex": otherIndex},
			}
		}
		outputKeys[key] = index

		if _, statErr := os.Stat(outputPath); statErr == nil {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputExists,
				Message: fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(outputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath},
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputInvalid,
				Message: fmt.Sprintf("%s cannot be accessed.", filepath.Base(outputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath},
				Cause:   statErr,
			}
		}

		dir := filepath.Dir(outputPath)
		if _, checked := checkedDirs[dir]; checked {
			continue
		}
		checkedDirs[dir] = struct{}{}
		if dirErr := validateRembgOutputDir(dir); dirErr != nil {
			dirErr.Details = map[string]any{"index": index, "outputDir": dir}
			return dirErr
		}
	}

	return nil
}

func validateRembgPythonPath(pythonPath string) (string, *BackgroundRemoveError) {
	trimmed := strings.TrimSpace(pythonPath)
	if trimmed == "" {
		return "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: "the background removal Python interpreter is not configured",
		}
	}

	resolved := absoluteImagePath(trimmed)
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: fmt.Sprintf("the background removal Python interpreter was not found: %s", resolved),
			Details: map[string]any{"pythonPath": resolved},
			Cause:   statErr,
		}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: fmt.Sprintf("the background removal Python interpreter is not a file: %s", resolved),
			Details: map[string]any{"pythonPath": resolved},
		}
	}

	return resolved, nil
}

// validateRembgModelPath validates the pinned ONNX file and derives the managed
// RemBG home from the documented layout <home>/models/<model>/<model>.onnx.
func validateRembgModelPath(modelPath string) (string, string, *BackgroundRemoveError) {
	trimmed := strings.TrimSpace(modelPath)
	if trimmed == "" {
		return "", "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: "the managed background removal model is not configured",
		}
	}

	resolved := absoluteImagePath(trimmed)
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return "", "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: fmt.Sprintf("the managed background removal model was not found: %s", resolved),
			Details: map[string]any{"modelPath": resolved},
			Cause:   statErr,
		}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: fmt.Sprintf("the managed background removal model is not a file: %s", resolved),
			Details: map[string]any{"modelPath": resolved},
		}
	}
	if info.Size() < rembgMinimumModelBytes {
		return "", "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: fmt.Sprintf("the managed background removal model is incomplete: %s", resolved),
			Details: map[string]any{"modelPath": resolved, "sizeBytes": info.Size()},
		}
	}

	home, homeErr := rembgHomeFromModelPath(resolved)
	if homeErr != nil {
		return "", "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: homeErr.Error(),
			Details: map[string]any{"modelPath": resolved},
			Cause:   homeErr,
		}
	}
	homeInfo, homeStatErr := os.Stat(home)
	if homeStatErr != nil || !homeInfo.IsDir() {
		return "", "", &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: fmt.Sprintf("the managed background removal home was not found: %s", home),
			Details: map[string]any{"modelPath": resolved, "rembgHome": home},
			Cause:   homeStatErr,
		}
	}

	return resolved, home, nil
}

func rembgHomeFromModelPath(modelPath string) (string, error) {
	modelDir := filepath.Dir(modelPath)
	modelsDir := filepath.Dir(modelDir)
	if !strings.EqualFold(filepath.Base(modelsDir), "models") {
		return "", fmt.Errorf("model path must use the layout <REMBG_HOME>/models/<model>/<model>.onnx: %s", modelPath)
	}

	return filepath.Dir(modelsDir), nil
}

func validateRembgOutputDir(dir string) *BackgroundRemoveError {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		trimmed = "."
	}

	info, statErr := os.Stat(trimmed)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputInvalid,
				Message: fmt.Sprintf("The output folder does not exist: %s", trimmed),
				Cause:   statErr,
			}
		}

		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeOutputInvalid,
			Message: fmt.Sprintf("The output folder cannot be used: %s", trimmed),
			Cause:   statErr,
		}
	}
	if !info.IsDir() {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeOutputInvalid,
			Message: fmt.Sprintf("%s is not a folder.", trimmed),
		}
	}

	probe, probeErr := os.CreateTemp(trimmed, rembgWriteProbePattern)
	if probeErr != nil {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeOutputInvalid,
			Message: fmt.Sprintf("The output folder is not writable: %s", trimmed),
			Cause:   probeErr,
		}
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	return nil
}

// verifyRembgBridgeOutput validates one bridge output before publication.
// Transparent mode requires truecolor-with-alpha (color type 6) and at least one
// transparent pixel; background mode requires truecolor RGB (color type 2), so a
// grayscale or translucent composite is never published as the requested color.
func verifyRembgBridgeOutput(event rembgBridgeEvent, outDir string, hasBackground bool) (int, int, error) {
	outputPath := strings.TrimSpace(event.Output)
	if outputPath == "" {
		return 0, 0, errors.New("the bridge reported an empty output path")
	}

	resolved := absoluteImagePath(outputPath)
	if rembgPathKey(filepath.Dir(resolved)) != rembgPathKey(outDir) {
		return 0, 0, fmt.Errorf("the bridge wrote outside the workspace output folder: %s", outputPath)
	}
	if !strings.EqualFold(filepath.Ext(resolved), ".png") {
		return 0, 0, fmt.Errorf("the bridge output is not a PNG: %s", outputPath)
	}

	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return 0, 0, statErr
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, 0, fmt.Errorf("the bridge output is empty: %s", outputPath)
	}
	if event.Width <= 0 || event.Height <= 0 {
		return 0, 0, fmt.Errorf("the bridge reported invalid dimensions: %dx%d", event.Width, event.Height)
	}

	colorType, colorErr := pngColorTypeOf(resolved)
	if colorErr != nil {
		return 0, 0, colorErr
	}

	file, openErr := os.Open(resolved)
	if openErr != nil {
		return 0, 0, openErr
	}
	defer file.Close()

	decoded, decodeErr := png.Decode(file)
	if decodeErr != nil {
		return 0, 0, fmt.Errorf("decode the bridge PNG: %w", decodeErr)
	}

	bounds := decoded.Bounds()
	if bounds.Dx() != event.Width || bounds.Dy() != event.Height {
		return 0, 0, fmt.Errorf("the bridge reported %dx%d but the PNG is %dx%d", event.Width, event.Height, bounds.Dx(), bounds.Dy())
	}

	if hasBackground {
		if colorType != pngColorTypeRGB {
			return 0, 0, fmt.Errorf("the bridge PNG is not RGB (color type %d)", colorType)
		}
	} else {
		if colorType != pngColorTypeRGBA {
			return 0, 0, fmt.Errorf("the bridge PNG is not RGBA (color type %d)", colorType)
		}
		if !rembgPNGHasTransparentPixel(decoded) {
			return 0, 0, errRembgNoTransparentPixels
		}
	}

	return bounds.Dx(), bounds.Dy(), nil
}

// rembgVerifyFailureMessage keeps actionable cutout guidance user-facing while
// other verification failures stay generic; the raw cause is kept in Details.
func rembgVerifyFailureMessage(item BackgroundRemoveItem, verifyErr error) string {
	if errors.Is(verifyErr, errRembgNoTransparentPixels) {
		return errRembgNoTransparentPixels.Error()
	}

	return fmt.Sprintf("The background removal engine produced an invalid image for %s.", filepath.Base(item.InputPath))
}

// pngColorTypeOf reads the IHDR color type without decoding pixels.
func pngColorTypeOf(path string) (byte, error) {
	file, openErr := os.Open(path)
	if openErr != nil {
		return 0, openErr
	}
	defer file.Close()

	header := make([]byte, 26)
	if _, readErr := io.ReadFull(file, header); readErr != nil {
		return 0, fmt.Errorf("read the PNG header: %w", readErr)
	}
	if !bytes.Equal(header[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return 0, errors.New("the bridge output is not a PNG")
	}
	if string(header[12:16]) != "IHDR" {
		return 0, errors.New("the PNG is missing its IHDR chunk")
	}

	return header[25], nil
}

func rembgPNGHasTransparentPixel(decoded image.Image) bool {
	switch typed := decoded.(type) {
	case *image.NRGBA:
		for index := 3; index < len(typed.Pix); index += 4 {
			if typed.Pix[index] == 0 {
				return true
			}
		}
		return false
	case *image.RGBA:
		for index := 3; index < len(typed.Pix); index += 4 {
			if typed.Pix[index] == 0 {
				return true
			}
		}
		return false
	case *image.NRGBA64:
		for index := 6; index+1 < len(typed.Pix); index += 8 {
			if typed.Pix[index] == 0 && typed.Pix[index+1] == 0 {
				return true
			}
		}
		return false
	case *image.RGBA64:
		for index := 6; index+1 < len(typed.Pix); index += 8 {
			if typed.Pix[index] == 0 && typed.Pix[index+1] == 0 {
				return true
			}
		}
		return false
	default:
		bounds := decoded.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				_, _, _, alpha := decoded.At(x, y).RGBA()
				if alpha == 0 {
					return true
				}
			}
		}
		return false
	}
}

// publishBackgroundRemovePNG writes the validated workspace PNG next to its
// destination, refuses an existing file and renames atomically.
func publishBackgroundRemovePNG(sourcePath, destinationPath string) *BackgroundRemoveError {
	payload, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not read the generated PNG for %s.", filepath.Base(destinationPath)),
			Cause:   readErr,
		}
	}

	tempFile, tempErr := os.CreateTemp(filepath.Dir(destinationPath), rembgTempFilePattern)
	if tempErr != nil {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   tempErr,
		}
	}
	tempPath := tempFile.Name()
	discardTemp := true
	defer func() {
		if discardTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, writeErr := tempFile.Write(payload); writeErr != nil {
		_ = tempFile.Close()
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   writeErr,
		}
	}
	if chmodErr := tempFile.Chmod(0o644); chmodErr != nil {
		_ = tempFile.Close()
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   chmodErr,
		}
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   closeErr,
		}
	}

	// Reserve the destination atomically: O_EXCL fails when a file appeared
	// after preflight, and the later rename replaces this placeholder with the
	// complete payload in one step.
	placeholder, placeholderErr := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if placeholderErr != nil {
		if errors.Is(placeholderErr, os.ErrExist) {
			return &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeOutputExists,
				Message: fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(destinationPath)),
			}
		}

		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   placeholderErr,
		}
	}
	if closeErr := placeholder.Close(); closeErr != nil {
		_ = rembgRemoveFile(destinationPath)

		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   closeErr,
		}
	}

	if renameErr := rembgRenameFile(tempPath, destinationPath); renameErr != nil {
		_ = rembgRemoveFile(destinationPath)

		return &BackgroundRemoveError{
			Code:    BackgroundRemoveCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   renameErr,
		}
	}
	discardTemp = false

	return nil
}

func removePublishedBackgroundOutputs(paths []string) []string {
	cleanupErrors := make([]string, 0)
	for _, path := range paths {
		if err := rembgRemoveFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", path, err))
		}
	}

	return cleanupErrors
}

// rembgErrorWithCleanup rolls back every output published by this call and adds
// the rollback failures to a whole-call error.
func rembgErrorWithCleanup(err *BackgroundRemoveError, published []string) *BackgroundRemoveError {
	cleanupErrors := removePublishedBackgroundOutputs(published)
	if len(cleanupErrors) == 0 {
		return err
	}

	if err.Details == nil {
		err.Details = make(map[string]any, 1)
	}
	err.Details["cleanupErrors"] = cleanupErrors

	return err
}

func rembgCloneDetails(details map[string]any) map[string]any {
	cloned := make(map[string]any, len(details)+1)
	for key, value := range details {
		cloned[key] = value
	}

	return cloned
}

// rembgChildEnv builds the child environment from os.Environ(), replacing every
// inherited REMBG_HOME/U2NET_HOME/PATH entry so the managed pack always wins and
// dropping Python's ambient import hooks.
func rembgChildEnv(pythonPath, home string) []string {
	isManaged := func(key string) bool {
		switch strings.ToUpper(key) {
		case "REMBG_HOME", "U2NET_HOME", "PYTHONNOUSERSITE", "OMP_NUM_THREADS", "PATH",
			"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP":
			return true
		default:
			return false
		}
	}

	env := make([]string, 0, len(os.Environ())+5)
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if !found || isManaged(key) {
			continue
		}
		env = append(env, entry)
	}

	pathValue := filepath.Dir(pythonPath)
	if inherited := os.Getenv("PATH"); inherited != "" {
		pathValue += string(os.PathListSeparator) + inherited
	}

	return append(env,
		"REMBG_HOME="+home,
		"U2NET_HOME="+home,
		"PYTHONNOUSERSITE=1",
		"OMP_NUM_THREADS="+strconv.Itoa(rembgThreadCount()),
		"PATH="+pathValue,
	)
}

// rembgThreadCount keeps onnxruntime from claiming every core of a busy
// desktop while still using more than one thread on typical machines.
func rembgThreadCount() int {
	threads := runtime.NumCPU() / 2
	if threads < 1 {
		return 1
	}

	return threads
}

// normalizeBackgroundColor validates and lowercases #RRGGBB. An empty value
// means transparent output.
func normalizeBackgroundColor(value string) (string, bool, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return "", false, true
	}
	if len(text) != 7 || text[0] != '#' {
		return "", false, false
	}
	if _, parseErr := strconv.ParseUint(text[1:], 16, 24); parseErr != nil {
		return "", false, false
	}

	return "#" + strings.ToLower(text[1:]), true, true
}

func rembgSupportsInputExtension(extension string) bool {
	for _, supported := range imageInputExtensions {
		if supported == extension {
			return true
		}
	}

	return false
}

func rembgPathKey(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if absolute, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = absolute
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}

	return cleaned
}

func absoluteImagePath(path string) string {
	if absolute, absErr := filepath.Abs(path); absErr == nil {
		return absolute
	}

	return path
}

func rembgItemFailuresError(failures []BackgroundRemoveItemFailure, total int, details map[string]any) *BackgroundRemoveError {
	if len(failures) == 1 && total == 1 {
		failure := failures[0]

		return &BackgroundRemoveError{
			Code:     failure.Code,
			Message:  failure.Message,
			Details:  details,
			Cause:    failure.Cause,
			Failures: failures,
		}
	}

	return &BackgroundRemoveError{
		Code:     BackgroundRemoveCodeBatchFailed,
		Message:  fmt.Sprintf("%d of %d images could not be processed.", len(failures), total),
		Details:  details,
		Failures: failures,
	}
}

func newBackgroundRemoveCancelled(cause error) *BackgroundRemoveError {
	return &BackgroundRemoveError{
		Code:    BackgroundRemoveCodeCancelled,
		Message: "background removal was canceled",
		Cause:   cause,
	}
}

// rembgCancellationCause reports whether a run was cancelled and returns the
// most specific cause available. The context wins because the runner also
// returns context.Canceled when CommandContext kills the interpreter.
// rembgCancellationCause reports whether a failed run was cancelled. A nil run
// error is a successful run even when the context was cancelled afterwards; a
// late ambient cancel must never reclassify validated output.
func rembgCancellationCause(ctx context.Context, runErr error) (error, bool) {
	if runErr == nil {
		return nil, false
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err(), true
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return runErr, true
	}

	return nil, false
}

// rembgBoundMessage keeps a bridge error readable in Details without letting a
// traceback-sized string reach the response.
func rembgBoundMessage(message string, maxRunes int) string {
	text := strings.TrimSpace(message)
	if text == "" {
		return "unknown background removal error"
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}

	return string(runes[:maxRunes]) + "..."
}

// rembgWithIndex adds the batch position to an item-level error without losing
// the engine details.
func rembgWithIndex(err *BackgroundRemoveError, index int) *BackgroundRemoveError {
	if err == nil {
		return nil
	}

	details := make(map[string]any, len(err.Details)+1)
	for key, value := range err.Details {
		details[key] = value
	}
	details["index"] = index
	err.Details = details

	return err
}
