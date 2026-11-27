package image

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/platform/execx"
)

// Local upscaling runs a pinned ONNX Real-ESRGAN style x4 model inside the
// dedicated managed virtual environment. One interpreter handles the whole
// batch so the model loads once; the embedded bridge reports JSON lines and the
// engine validates and publishes every output atomically.
const (
	UpscaleCodeRuntimeUnavailable = "IMAGE_UPSCALE_RUNTIME_UNAVAILABLE"
	UpscaleCodeModelUnavailable   = "IMAGE_UPSCALE_MODEL_UNAVAILABLE"
	UpscaleCodeInvalidInput       = "IMAGE_UPSCALE_INPUT_INVALID"
	UpscaleCodeInputNotFound      = "IMAGE_UPSCALE_INPUT_NOT_FOUND"
	UpscaleCodeInputUnsupported   = "IMAGE_UPSCALE_INPUT_UNSUPPORTED"
	UpscaleCodeInvalidOption      = "IMAGE_UPSCALE_OPTION_INVALID"
	UpscaleCodeOutputInvalid      = "IMAGE_UPSCALE_OUTPUT_INVALID"
	UpscaleCodeOutputExists       = "IMAGE_UPSCALE_OUTPUT_ALREADY_EXISTS"
	UpscaleCodeOutputTooLarge     = "IMAGE_UPSCALE_OUTPUT_TOO_LARGE"
	UpscaleCodeProcessFailed      = "IMAGE_UPSCALE_PROCESS_FAILED"
	UpscaleCodeVerifyFailed       = "IMAGE_UPSCALE_VERIFY_FAILED"
	UpscaleCodePublishFailed      = "IMAGE_UPSCALE_PUBLISH_FAILED"
	UpscaleCodeBatchFailed        = "IMAGE_UPSCALE_BATCH_FAILED"
	UpscaleCodeCancelled          = "IMAGE_UPSCALE_CANCELLED"
)

const (
	upscaleTempDirPattern  = "fileforge-upscale-*"
	upscaleTempFilePattern = ".fileforge-upscale-*.tmp"
	upscaleBridgeFileName  = "upscale_bridge.py"

	// upscaleMinimumModelBytes rejects empty or truncated ONNX files before an
	// interpreter starts. The pinned model keeps its weights in an external
	// .data file, so the graph itself is small; the toolchain installer verifies
	// the exact pinned sizes and SHA-256 and the engine only needs a
	// plausibility floor.
	upscaleMinimumModelBytes int64 = 64 << 10

	// upscaleMaxOutputPixels bounds the in-memory canvas the bridge allocates.
	// 64 MP covers a 2x pass on a 4000x3000 photo while a 4x pass on the same
	// input (192 MP) is refused with an actionable message instead of an OOM.
	upscaleMaxOutputPixels int64 = 64_000_000

	upscaleMinTile = 32
	upscaleMaxTile = 1024

	upscaleDefaultQuality = 90
)

// upscaleBridgeSource is the Python bridge written to the workspace on every
// run.
//
//go:embed upscale_bridge.py
var upscaleBridgeSource []byte

// UpscaleOptions describes one upscaling pass.
type UpscaleOptions struct {
	Scale    int
	TileSize int
	Format   string
	Quality  int
}

// UpscaleItem is one input/output pair. Outputs are planned paths.
type UpscaleItem struct {
	InputPath  string
	OutputPath string
}

// UpscaleOutcome reports one published image and its pixel size.
type UpscaleOutcome struct {
	OutputPath string
	Width      int
	Height     int
}

// UpscaleItemFailure is one per-item failure reported by the bridge or by
// publication. Message is user-facing; Cause keeps the raw error.
type UpscaleItemFailure struct {
	Index      int
	InputPath  string
	OutputPath string
	Code       string
	Message    string
	Cause      error
}

// UpscaleError is a typed failure with a stable IMAGE_UPSCALE_* code. A batch
// with some failed items also carries Failures.
type UpscaleError struct {
	Code     string
	Message  string
	Details  map[string]any
	Cause    error
	Failures []UpscaleItemFailure
}

func (e *UpscaleError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *UpscaleError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// UpscaleCommandRunner runs the bridge and streams its stdout lines. The engine
// owns parsing, validation and publication; the runner only executes Python.
//
// Contract: onLine is invoked serially from a single goroutine, never
// concurrently. RunUpscale must return only after the last callback completed
// and the process is fully drained.
type UpscaleCommandRunner interface {
	RunUpscale(ctx context.Context, dir, pythonPath string, args []string, env []string, onLine func(string)) error
}

// UpscaleCommandError keeps the bounded stderr tail next to the process error.
type UpscaleCommandError struct {
	Err    error
	Stderr string
}

func (e *UpscaleCommandError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		// execx already renders the command, exit code and bounded stderr.
		return e.Err.Error()
	}

	return e.Stderr
}

func (e *UpscaleCommandError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// ExecUpscaleCommandRunner runs real interpreters with exec.CommandContext, so
// context cancellation kills the process tree and the model stops loading.
type ExecUpscaleCommandRunner struct{}

func (r *ExecUpscaleCommandRunner) RunUpscale(ctx context.Context, dir, pythonPath string, args []string, env []string, onLine func(string)) error {
	// execx.Run owns the process tree: a process group on Unix, a Job Object on
	// Windows, so a cancelled job kills the interpreter and every child it
	// spawned. It also bounds the stderr tail and splits oversized lines.
	result, err := execx.Run(ctx, execx.Command{
		Path:            pythonPath,
		Args:            args,
		Dir:             dir,
		Env:             env,
		Stdout:          onLine,
		StderrTailLines: rembgStderrMaxLines,
		StderrTailBytes: rembgStderrMaxBytes,
		ScanMaxToken:    rembgScannerMaxBytes,
	})
	if err != nil {
		return &UpscaleCommandError{Err: err, Stderr: result.Stderr}
	}

	return nil
}

// upscaleBridgeEvent is one JSON line emitted by upscale_bridge.py.
type upscaleBridgeEvent struct {
	Event   string `json:"event"`
	Index   int    `json:"index"`
	Total   int    `json:"total"`
	Output  string `json:"output"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Message string `json:"message"`
}

// upscaleItemState tracks what the bridge reported for one planned item.
type upscaleItemState struct {
	reported   bool
	outcome    UpscaleOutcome
	failure    *UpscaleItemFailure
	expectedW  int
	expectedH  int
	scale      int
	format     string
	outputPath string
	inputPath  string
}

// NormalizeUpscaleOptions validates and normalizes one upscale pass. An empty
// format means PNG and a zero quality means the default for JPEG/WebP.
func NormalizeUpscaleOptions(opts UpscaleOptions) (UpscaleOptions, *UpscaleError) {
	normalized := UpscaleOptions{
		Scale:    opts.Scale,
		TileSize: opts.TileSize,
		Format:   normalizeFormatName(opts.Format),
		Quality:  opts.Quality,
	}

	if normalized.Scale != 2 && normalized.Scale != 4 {
		return UpscaleOptions{}, &UpscaleError{
			Code:    UpscaleCodeInvalidOption,
			Message: "scale must be 2 or 4",
			Details: map[string]any{"scale": opts.Scale},
		}
	}

	if normalized.TileSize == 0 {
		normalized.TileSize = 128
	}
	if normalized.TileSize < upscaleMinTile || normalized.TileSize > upscaleMaxTile {
		return UpscaleOptions{}, &UpscaleError{
			Code:    UpscaleCodeInvalidOption,
			Message: fmt.Sprintf("tileSize must be between %d and %d pixels", upscaleMinTile, upscaleMaxTile),
			Details: map[string]any{"tileSize": opts.TileSize},
		}
	}

	if normalized.Format == "" {
		normalized.Format = "png"
	}
	switch normalized.Format {
	case "png":
		normalized.Quality = 0
	case "jpeg", "webp":
		if normalized.Quality == 0 {
			normalized.Quality = upscaleDefaultQuality
		}
		if normalized.Quality < 1 || normalized.Quality > 100 {
			return UpscaleOptions{}, &UpscaleError{
				Code:    UpscaleCodeInvalidOption,
				Message: "quality must be between 1 and 100",
				Details: map[string]any{"quality": opts.Quality},
			}
		}
	default:
		return UpscaleOptions{}, &UpscaleError{
			Code:    UpscaleCodeInvalidOption,
			Message: "format must be png, jpeg or webp",
			Details: map[string]any{"format": opts.Format},
		}
	}

	return normalized, nil
}

// UpscaleImages validates the managed runtime and the batch, splits the batch
// into bridge-sized chunks and publishes each validated image atomically.
//
// Publication policy: an item is published only after its file exists, decodes,
// matches the expected dimensions (input size times the scale) and matches the
// requested format. A destination is never overwritten. Any per-item failure,
// process failure or cancellation removes every output published by this call,
// so a failed or cancelled job leaves no partial results.
func UpscaleImages(
	ctx context.Context,
	runner UpscaleCommandRunner,
	pythonPath, modelPath, workingDir string,
	items []UpscaleItem,
	opts UpscaleOptions,
	onProgress func(current, total int),
) ([]UpscaleOutcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, newUpscaleCancelled(ctx.Err())
	}
	if runner == nil {
		runner = &ExecUpscaleCommandRunner{}
	}

	normalized, optionErr := NormalizeUpscaleOptions(opts)
	if optionErr != nil {
		return nil, optionErr
	}

	resolvedPython, pythonErr := validateUpscalePythonPath(pythonPath)
	if pythonErr != nil {
		return nil, pythonErr
	}
	resolvedModel, modelErr := validateUpscaleModelPath(modelPath)
	if modelErr != nil {
		return nil, modelErr
	}

	if len(items) == 0 {
		return nil, &UpscaleError{
			Code:    UpscaleCodeInvalidInput,
			Message: "at least one input image is required",
		}
	}
	if preflightErr := preflightUpscaleItems(items, normalized); preflightErr != nil {
		return nil, preflightErr
	}

	workspace, workspaceErr := os.MkdirTemp(strings.TrimSpace(workingDir), upscaleTempDirPattern)
	if workspaceErr != nil {
		return nil, &UpscaleError{
			Code:    UpscaleCodeProcessFailed,
			Message: "could not create the upscale workspace",
			Cause:   workspaceErr,
		}
	}
	defer func() {
		_ = os.RemoveAll(workspace)
	}()
	if absolute, absErr := filepath.Abs(workspace); absErr == nil {
		workspace = absolute
	}

	bridgePath := filepath.Join(workspace, upscaleBridgeFileName)
	if writeErr := os.WriteFile(bridgePath, upscaleBridgeSource, 0o644); writeErr != nil {
		return nil, &UpscaleError{
			Code:    UpscaleCodeProcessFailed,
			Message: "could not write the upscale bridge",
			Cause:   writeErr,
		}
	}

	env := upscaleChildEnv(resolvedPython)

	states := make([]upscaleItemState, len(items))
	for index, item := range items {
		meta, metaErr := readPlanImageMeta(item.InputPath)
		if metaErr != nil {
			return nil, &UpscaleError{
				Code:    UpscaleCodeInvalidInput,
				Message: fmt.Sprintf("%s is not a readable image.", filepath.Base(item.InputPath)),
				Details: map[string]any{"inputPath": item.InputPath},
				Cause:   metaErr,
			}
		}
		states[index] = upscaleItemState{
			inputPath:  item.InputPath,
			outputPath: item.OutputPath,
			expectedW:  meta.Width * normalized.Scale,
			expectedH:  meta.Height * normalized.Scale,
			scale:      normalized.Scale,
			format:     normalized.Format,
		}
	}

	published := make([]string, 0, len(items))
	chunks := upscaleItemChunks(items)

	var processDetails map[string]any
	lastProgress := 0
	failed := false

	for chunkIndex, chunk := range chunks {
		if failed {
			break
		}

		chunkOutDir := filepath.Join(workspace, fmt.Sprintf("out-%d", chunkIndex))
		if mkdirErr := os.Mkdir(chunkOutDir, 0o755); mkdirErr != nil {
			chunkErr := &UpscaleError{
				Code:    UpscaleCodeProcessFailed,
				Message: "could not create the upscale workspace output folder",
				Cause:   mkdirErr,
			}

			return nil, upscaleErrorWithCleanup(chunkErr, published)
		}

		chunkInputs := make([]string, 0, chunk.end-chunk.start)
		for index := chunk.start; index < chunk.end; index++ {
			chunkInputs = append(chunkInputs, absoluteImagePath(strings.TrimSpace(items[index].InputPath)))
		}

		args := []string{
			bridgePath,
			"--model", resolvedModel,
			"--scale", strconv.Itoa(normalized.Scale),
			"--tile", strconv.Itoa(normalized.TileSize),
			"--format", normalized.Format,
			"--quality", strconv.Itoa(normalized.Quality),
			"--threads", strconv.Itoa(rembgThreadCount()),
			"--out-dir", chunkOutDir,
		}
		args = append(args, chunkInputs...)

		chunkItemCount := chunk.end - chunk.start
		onLine := func(rawLine string) {
			line := strings.TrimSpace(rawLine)
			if line == "" || !strings.HasPrefix(line, "{") {
				return
			}

			var event upscaleBridgeEvent
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

				width, height, verifyErr := verifyUpscaleBridgeOutput(event, chunkOutDir, *state)
				if verifyErr != nil {
					state.failure = &UpscaleItemFailure{
						Index:      globalIndex,
						InputPath:  item.InputPath,
						OutputPath: item.OutputPath,
						Code:       UpscaleCodeVerifyFailed,
						Message:    fmt.Sprintf("The upscale engine produced an invalid image for %s.", filepath.Base(item.InputPath)),
						Cause:      verifyErr,
					}
					return
				}

				if publishErr := publishUpscaleOutput(event.Output, item.OutputPath); publishErr != nil {
					state.failure = &UpscaleItemFailure{
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
				state.outcome = UpscaleOutcome{
					OutputPath: item.OutputPath,
					Width:      width,
					Height:     height,
				}
			case "error":
				if state.reported {
					return
				}
				state.reported = true
				state.failure = &UpscaleItemFailure{
					Index:      globalIndex,
					InputPath:  item.InputPath,
					OutputPath: item.OutputPath,
					Code:       UpscaleCodeProcessFailed,
					Message:    fmt.Sprintf("Could not upscale %s.", filepath.Base(item.InputPath)),
					Cause:      errors.New(rembgBoundMessage(event.Message, rembgMaxCauseRunes)),
				}
			}
		}

		runErr := runner.RunUpscale(ctx, workspace, resolvedPython, args, env, onLine)

		if cancelledBy, cancelled := upscaleCancellationCause(ctx, runErr); cancelled {
			cancelledErr := newUpscaleCancelled(cancelledBy)

			return nil, upscaleErrorWithCleanup(cancelledErr, published)
		}

		if runErr != nil && errors.Is(runErr, exec.ErrNotFound) {
			runtimeErr := &UpscaleError{
				Code:    UpscaleCodeRuntimeUnavailable,
				Message: fmt.Sprintf("the upscale Python interpreter could not run: %s", resolvedPython),
				Details: map[string]any{"pythonPath": resolvedPython},
				Cause:   runErr,
			}

			return nil, upscaleErrorWithCleanup(runtimeErr, published)
		}

		var processCause error
		if runErr != nil {
			processCause = runErr
			details := map[string]any{"pythonPath": resolvedPython}
			var commandErr *UpscaleCommandError
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
				state.failure = &UpscaleItemFailure{
					Index:      index,
					InputPath:  items[index].InputPath,
					OutputPath: items[index].OutputPath,
					Code:       UpscaleCodeProcessFailed,
					Message:    fmt.Sprintf("The upscale engine produced no result for %s.", filepath.Base(items[index].InputPath)),
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
		cleanupErrors := removePublishedUpscaleOutputs(published)
		details := upscaleCloneDetails(processDetails)
		if len(cleanupErrors) > 0 {
			details["cleanupErrors"] = cleanupErrors
		}

		return nil, upscaleItemFailuresError(upscaleRollbackFailures(items, states), len(items), details)
	}

	outcomes := make([]UpscaleOutcome, 0, len(items))
	for _, state := range states {
		if state.outcome.OutputPath != "" {
			outcomes = append(outcomes, state.outcome)
		}
	}

	return outcomes, nil
}

// upscaleItemChunk is one contiguous global item range handled by one bridge
// process.
type upscaleItemChunk struct {
	start int
	end   int
}

// upscaleItemChunks splits items into argv-sized chunks. A single path longer
// than the budget still gets its own chunk.
func upscaleItemChunks(items []UpscaleItem) []upscaleItemChunk {
	chunks := make([]upscaleItemChunk, 0, len(items)/rembgChunkMaxItems+1)
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

		chunks = append(chunks, upscaleItemChunk{start: start, end: end})
		start = end
	}

	return chunks
}

// upscaleRollbackFailures reports every item after a job-wide rollback.
func upscaleRollbackFailures(items []UpscaleItem, states []upscaleItemState) []UpscaleItemFailure {
	failures := make([]UpscaleItemFailure, 0, len(items))
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
		failures = append(failures, UpscaleItemFailure{
			Index:      index,
			InputPath:  item.InputPath,
			OutputPath: item.OutputPath,
			Code:       UpscaleCodeBatchFailed,
			Message:    message,
		})
	}

	return failures
}

// ValidateUpscaleInput checks that inputPath is an existing regular image with
// a supported extension. It never decodes pixels.
func ValidateUpscaleInput(inputPath string) *UpscaleError {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return &UpscaleError{
			Code:    UpscaleCodeInvalidInput,
			Message: "inputPath is required",
		}
	}

	info, statErr := os.Stat(trimmed)
	if statErr != nil {
		message := fmt.Sprintf("%s cannot be read.", filepath.Base(trimmed))
		if errors.Is(statErr, os.ErrNotExist) {
			message = fmt.Sprintf("%s was not found.", filepath.Base(trimmed))
		}

		return &UpscaleError{
			Code:    UpscaleCodeInputNotFound,
			Message: message,
			Details: map[string]any{"inputPath": trimmed},
			Cause:   statErr,
		}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return &UpscaleError{
			Code:    UpscaleCodeInputUnsupported,
			Message: fmt.Sprintf("%s is not a regular image file.", filepath.Base(trimmed)),
			Details: map[string]any{"inputPath": trimmed},
		}
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(trimmed), "."))
	if !upscaleSupportsInputExtension(extension) {
		return &UpscaleError{
			Code:    UpscaleCodeInputUnsupported,
			Message: fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(trimmed), strings.Join(imageInputExtensions, ", ")),
			Details: map[string]any{"inputPath": trimmed, "extension": extension},
		}
	}

	return nil
}

// preflightUpscaleItems validates every item before Python starts.
func preflightUpscaleItems(items []UpscaleItem, opts UpscaleOptions) *UpscaleError {
	inputKeys := make(map[string]int, len(items))
	for index, item := range items {
		inputPath := strings.TrimSpace(item.InputPath)
		if inputErr := ValidateUpscaleInput(inputPath); inputErr != nil {
			return upscaleWithIndex(inputErr, index)
		}

		meta, metaErr := readPlanImageMeta(inputPath)
		if metaErr != nil {
			return upscaleWithIndex(&UpscaleError{
				Code:    UpscaleCodeInvalidInput,
				Message: fmt.Sprintf("%s is not a readable image.", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath},
				Cause:   metaErr,
			}, index)
		}
		outputPixels := int64(meta.Width*opts.Scale) * int64(meta.Height*opts.Scale)
		if outputPixels > upscaleMaxOutputPixels {
			return upscaleWithIndex(&UpscaleError{
				Code: UpscaleCodeOutputTooLarge,
				Message: fmt.Sprintf(
					"%s at %dx would be %d×%d px (%.1f MP). The limit is %.0f MP; choose %dx or a smaller image.",
					filepath.Base(inputPath), opts.Scale,
					meta.Width*opts.Scale, meta.Height*opts.Scale,
					float64(outputPixels)/1_000_000,
					float64(upscaleMaxOutputPixels)/1_000_000,
					opts.Scale/2,
				),
				Details: map[string]any{
					"inputPath":       inputPath,
					"outputWidth":     meta.Width * opts.Scale,
					"outputHeight":    meta.Height * opts.Scale,
					"outputPixels":    outputPixels,
					"maxOutputPixels": upscaleMaxOutputPixels,
				},
			}, index)
		}

		inputKeys[rembgPathKey(inputPath)] = index
	}

	outputKeys := make(map[string]int, len(items))
	checkedDirs := make(map[string]struct{}, len(items))
	expectedExtension := upscaleOutputExtension(opts.Format)
	for index, item := range items {
		outputPath := strings.TrimSpace(item.OutputPath)
		if outputPath == "" {
			return &UpscaleError{
				Code:    UpscaleCodeOutputInvalid,
				Message: "outputPath is required",
				Details: map[string]any{"index": index, "inputPath": item.InputPath},
			}
		}
		if !strings.EqualFold(filepath.Ext(outputPath), "."+expectedExtension) {
			return &UpscaleError{
				Code:    UpscaleCodeOutputInvalid,
				Message: fmt.Sprintf("outputPath must use the .%s extension", expectedExtension),
				Details: map[string]any{"index": index, "outputPath": outputPath, "format": opts.Format},
			}
		}

		key := rembgPathKey(outputPath)
		if inputIndex, collides := inputKeys[key]; collides {
			return &UpscaleError{
				Code:    UpscaleCodeOutputInvalid,
				Message: fmt.Sprintf("output path collides with input %s.", filepath.Base(items[inputIndex].InputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath, "inputIndex": inputIndex},
			}
		}
		if otherIndex, duplicate := outputKeys[key]; duplicate {
			return &UpscaleError{
				Code:    UpscaleCodeOutputInvalid,
				Message: fmt.Sprintf("output path is planned twice: %s.", filepath.Base(outputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath, "duplicateIndex": otherIndex},
			}
		}
		outputKeys[key] = index

		if _, statErr := os.Stat(outputPath); statErr == nil {
			return &UpscaleError{
				Code:    UpscaleCodeOutputExists,
				Message: fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(outputPath)),
				Details: map[string]any{"index": index, "outputPath": outputPath},
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return &UpscaleError{
				Code:    UpscaleCodeOutputInvalid,
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
			return upscaleFromBackgroundError(dirErr)
		}
	}

	return nil
}

func validateUpscalePythonPath(pythonPath string) (string, *UpscaleError) {
	trimmed := strings.TrimSpace(pythonPath)
	if trimmed == "" {
		return "", &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: "the upscale Python interpreter is not configured",
		}
	}

	resolved := absoluteImagePath(trimmed)
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return "", &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: fmt.Sprintf("the upscale Python interpreter was not found: %s", resolved),
			Details: map[string]any{"pythonPath": resolved},
			Cause:   statErr,
		}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: fmt.Sprintf("the upscale Python interpreter is not a file: %s", resolved),
			Details: map[string]any{"pythonPath": resolved},
		}
	}

	return resolved, nil
}

func validateUpscaleModelPath(modelPath string) (string, *UpscaleError) {
	trimmed := strings.TrimSpace(modelPath)
	if trimmed == "" {
		return "", &UpscaleError{
			Code:    UpscaleCodeModelUnavailable,
			Message: "the upscale model is not configured",
		}
	}

	resolved := absoluteImagePath(trimmed)
	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return "", &UpscaleError{
			Code:    UpscaleCodeModelUnavailable,
			Message: fmt.Sprintf("the upscale model was not found: %s", resolved),
			Details: map[string]any{"modelPath": resolved},
			Cause:   statErr,
		}
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", &UpscaleError{
			Code:    UpscaleCodeModelUnavailable,
			Message: fmt.Sprintf("the upscale model is not a file: %s", resolved),
			Details: map[string]any{"modelPath": resolved},
		}
	}
	if info.Size() < upscaleMinimumModelBytes {
		return "", &UpscaleError{
			Code:    UpscaleCodeModelUnavailable,
			Message: fmt.Sprintf("the upscale model is incomplete: %s", resolved),
			Details: map[string]any{"modelPath": resolved, "sizeBytes": info.Size()},
		}
	}

	return resolved, nil
}

// verifyUpscaleBridgeOutput validates one bridge output before publication:
// the file must live in the workspace output folder, be non-empty, decode with
// the requested format and match the expected dimensions exactly.
func verifyUpscaleBridgeOutput(event upscaleBridgeEvent, outDir string, state upscaleItemState) (int, int, error) {
	outputPath := strings.TrimSpace(event.Output)
	if outputPath == "" {
		return 0, 0, errors.New("the bridge reported an empty output path")
	}

	resolved := absoluteImagePath(outputPath)
	if rembgPathKey(filepath.Dir(resolved)) != rembgPathKey(outDir) {
		return 0, 0, fmt.Errorf("the bridge wrote outside the workspace output folder: %s", outputPath)
	}
	if !strings.EqualFold(filepath.Ext(resolved), "."+upscaleOutputExtension(state.format)) {
		return 0, 0, fmt.Errorf("the bridge output does not use the .%s extension: %s", upscaleOutputExtension(state.format), outputPath)
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

	meta, metaErr := readPlanImageMeta(resolved)
	if metaErr != nil {
		return 0, 0, fmt.Errorf("decode the bridge output: %w", metaErr)
	}
	if meta.Format != state.format {
		return 0, 0, fmt.Errorf("the bridge output is %s, want %s", meta.Format, state.format)
	}
	if meta.Width != state.expectedW || meta.Height != state.expectedH {
		return 0, 0, fmt.Errorf(
			"the bridge output is %dx%d, want %dx%d (input × %d)",
			meta.Width, meta.Height, state.expectedW, state.expectedH, state.scale,
		)
	}
	if event.Width != meta.Width || event.Height != meta.Height {
		return 0, 0, fmt.Errorf("the bridge reported %dx%d but the file is %dx%d", event.Width, event.Height, meta.Width, meta.Height)
	}

	return meta.Width, meta.Height, nil
}

// publishUpscaleOutput writes the validated workspace file next to its
// destination, refuses an existing file and renames atomically.
func publishUpscaleOutput(sourcePath, destinationPath string) *UpscaleError {
	payload, readErr := os.ReadFile(sourcePath)
	if readErr != nil {
		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not read the generated image for %s.", filepath.Base(destinationPath)),
			Cause:   readErr,
		}
	}

	tempFile, tempErr := os.CreateTemp(filepath.Dir(destinationPath), upscaleTempFilePattern)
	if tempErr != nil {
		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
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
		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   writeErr,
		}
	}
	if chmodErr := tempFile.Chmod(0o644); chmodErr != nil {
		_ = tempFile.Close()
		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   chmodErr,
		}
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   closeErr,
		}
	}

	placeholder, placeholderErr := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if placeholderErr != nil {
		if errors.Is(placeholderErr, os.ErrExist) {
			return &UpscaleError{
				Code:    UpscaleCodeOutputExists,
				Message: fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(destinationPath)),
			}
		}

		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   placeholderErr,
		}
	}
	if closeErr := placeholder.Close(); closeErr != nil {
		_ = rembgRemoveFile(destinationPath)

		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   closeErr,
		}
	}

	if renameErr := rembgRenameFile(tempPath, destinationPath); renameErr != nil {
		_ = rembgRemoveFile(destinationPath)

		return &UpscaleError{
			Code:    UpscaleCodePublishFailed,
			Message: fmt.Sprintf("Could not save %s.", filepath.Base(destinationPath)),
			Cause:   renameErr,
		}
	}
	discardTemp = false

	return nil
}

func removePublishedUpscaleOutputs(paths []string) []string {
	cleanupErrors := make([]string, 0)
	for _, path := range paths {
		if err := rembgRemoveFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErrors = append(cleanupErrors, fmt.Sprintf("%s: %v", path, err))
		}
	}

	return cleanupErrors
}

// upscaleErrorWithCleanup rolls back every output published by this call and
// adds the rollback failures to a whole-call error.
func upscaleErrorWithCleanup(err *UpscaleError, published []string) *UpscaleError {
	cleanupErrors := removePublishedUpscaleOutputs(published)
	if len(cleanupErrors) == 0 {
		return err
	}

	if err.Details == nil {
		err.Details = make(map[string]any, 1)
	}
	err.Details["cleanupErrors"] = cleanupErrors

	return err
}

func upscaleCloneDetails(details map[string]any) map[string]any {
	cloned := make(map[string]any, len(details)+1)
	for key, value := range details {
		cloned[key] = value
	}

	return cloned
}

// upscaleChildEnv builds the child environment from os.Environ(), dropping
// Python's ambient import hooks and bounding CPU threads.
func upscaleChildEnv(pythonPath string) []string {
	isManaged := func(key string) bool {
		switch strings.ToUpper(key) {
		case "PYTHONNOUSERSITE", "OMP_NUM_THREADS", "PATH", "PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP":
			return true
		default:
			return false
		}
	}

	env := make([]string, 0, len(os.Environ())+3)
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
		"PYTHONNOUSERSITE=1",
		"OMP_NUM_THREADS="+strconv.Itoa(rembgThreadCount()),
		"PATH="+pathValue,
	)
}

func upscaleSupportsInputExtension(extension string) bool {
	for _, supported := range imageInputExtensions {
		if supported == extension {
			return true
		}
	}

	return false
}

func upscaleOutputExtension(format string) string {
	if format == "jpeg" {
		return "jpg"
	}

	return format
}

func upscaleItemFailuresError(failures []UpscaleItemFailure, total int, details map[string]any) *UpscaleError {
	if len(failures) == 1 && total == 1 {
		failure := failures[0]

		return &UpscaleError{
			Code:     failure.Code,
			Message:  failure.Message,
			Details:  details,
			Cause:    failure.Cause,
			Failures: failures,
		}
	}

	return &UpscaleError{
		Code:     UpscaleCodeBatchFailed,
		Message:  fmt.Sprintf("%d of %d images could not be processed.", len(failures), total),
		Details:  details,
		Failures: failures,
	}
}

func newUpscaleCancelled(cause error) *UpscaleError {
	return &UpscaleError{
		Code:    UpscaleCodeCancelled,
		Message: "upscaling was canceled",
		Cause:   cause,
	}
}

// upscaleCancellationCause reports whether a failed run was cancelled. A nil
// run error is a successful run even when the context was cancelled afterwards.
func upscaleCancellationCause(ctx context.Context, runErr error) (error, bool) {
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

// upscaleWithIndex adds the batch position to an item-level error.
func upscaleWithIndex(err *UpscaleError, index int) *UpscaleError {
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

// upscaleFromBackgroundError adapts a shared output-directory failure.
func upscaleFromBackgroundError(err *BackgroundRemoveError) *UpscaleError {
	if err == nil {
		return nil
	}
	code := UpscaleCodeOutputInvalid
	if err.Code == BackgroundRemoveCodeOutputExists {
		code = UpscaleCodeOutputExists
	}

	return &UpscaleError{
		Code:    code,
		Message: err.Message,
		Details: err.Details,
		Cause:   err.Cause,
	}
}
