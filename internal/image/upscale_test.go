package image

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

// upscaleStubCall records one RunUpscale invocation.
type upscaleStubCall struct {
	dir        string
	pythonPath string
	args       []string
	env        []string
}

func (c upscaleStubCall) argAfter(flag string) string {
	for index, arg := range c.args {
		if arg == flag && index+1 < len(c.args) {
			return c.args[index+1]
		}
	}

	return ""
}

type upscaleStubRunner struct {
	mu      sync.Mutex
	calls   []upscaleStubCall
	handler func(ctx context.Context, call upscaleStubCall, onLine func(string)) error
}

func (r *upscaleStubRunner) RunUpscale(ctx context.Context, dir, pythonPath string, args []string, env []string, onLine func(string)) error {
	r.mu.Lock()
	call := upscaleStubCall{
		dir:        dir,
		pythonPath: pythonPath,
		args:       append([]string{}, args...),
		env:        append([]string{}, env...),
	}
	r.calls = append(r.calls, call)
	r.mu.Unlock()

	if r.handler != nil {
		return r.handler(ctx, call, onLine)
	}

	return nil
}

func (r *upscaleStubRunner) recorded() []upscaleStubCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]upscaleStubCall{}, r.calls...)
}

func upscaleEvent(t *testing.T, event string, index, width, height int, output string) string {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"event":  event,
		"index":  index,
		"total":  1,
		"output": output,
		"width":  width,
		"height": height,
	})
	if err != nil {
		t.Fatalf("marshal stub event: %v", err)
	}

	return string(payload)
}

func writeUpscalePNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 200, A: 255})
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the PNG folder: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// withUpscaleTestRuntime installs a fake managed runtime so plan tests never
// touch the real application data directory.
func withUpscaleTestRuntime(t *testing.T) (dataDir, pythonPath string) {
	t.Helper()

	dataDir = t.TempDir()
	pythonPath = filepath.Join(dataDir, "python")
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write the fake interpreter: %v", err)
	}

	original := resolveUpscaleRuntime
	resolveUpscaleRuntime = func() (upscaleRuntime, *UpscaleError) {
		return upscaleRuntime{PythonPath: pythonPath, DataDir: dataDir}, nil
	}
	t.Cleanup(func() {
		resolveUpscaleRuntime = original
	})

	return dataDir, pythonPath
}

// fakeUpscalePython writes a plausibility-sized interpreter placeholder.
func fakeUpscalePython(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "python")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write the fake interpreter: %v", err)
	}

	return path
}

// fakeCustomUpscaleModel writes a plausibility-sized .onnx file.
func fakeCustomUpscaleModel(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "custom.onnx")
	payload := make([]byte, upscaleMinimumModelBytes)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write the fake model: %v", err)
	}

	return path
}

func upscaleTestRequest(mode string, inputs []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDImageUpscale,
		Mode:       mode,
		InputPaths: inputs,
		Options:    options,
	}
}

func TestUpscalePlanRejectsUnsupportedScale(t *testing.T) {
	_, _ = withUpscaleTestRuntime(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)

	tool := NewUpscaleTool()
	plan, planErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{input}, map[string]any{
		"scale":     3,
		"modelPath": model,
	}))

	if planErr == nil {
		t.Fatalf("expected an unsupported scale to fail: %+v", plan)
	}
	if planErr.DetailCode != UpscaleCodeInvalidOption {
		t.Fatalf("detail code = %q, want %q", planErr.DetailCode, UpscaleCodeInvalidOption)
	}
	if planErr.Code != "VALIDATION_INVALID_INPUT" {
		t.Fatalf("canonical code = %q, want VALIDATION_INVALID_INPUT", planErr.Code)
	}
}

func TestUpscalePlanRejectsFractionalScale(t *testing.T) {
	_, _ = withUpscaleTestRuntime(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)

	tool := NewUpscaleTool()
	_, planErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{input}, map[string]any{
		"scale":     2.5,
		"modelPath": model,
	}))

	if planErr == nil || planErr.DetailCode != UpscaleCodeInvalidOption {
		t.Fatalf("expected a fractional scale to be rejected, got %+v", planErr)
	}
}

func TestUpscalePlanRejectsUnsupportedFormat(t *testing.T) {
	_, _ = withUpscaleTestRuntime(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)

	tool := NewUpscaleTool()
	_, planErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{input}, map[string]any{
		"format":    "gif",
		"modelPath": model,
	}))

	if planErr == nil {
		t.Fatal("expected an unsupported format to fail")
	}
	if planErr.DetailCode != "IMAGE_UPSCALE_FORMAT_INVALID" {
		t.Fatalf("detail code = %q, want IMAGE_UPSCALE_FORMAT_INVALID", planErr.DetailCode)
	}
}

func TestUpscalePlanReportsMissingRuntime(t *testing.T) {
	original := resolveUpscaleRuntime
	resolveUpscaleRuntime = func() (upscaleRuntime, *UpscaleError) {
		return upscaleRuntime{}, &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: UpscaleRuntimeMissingReason,
		}
	}
	t.Cleanup(func() {
		resolveUpscaleRuntime = original
	})

	tool := NewUpscaleTool()
	state := tool.RuntimeState(context.Background())
	if state.Status != "degraded" || state.Healthy {
		t.Fatalf("expected a degraded runtime state, got %+v", state)
	}
	if state.Reason != UpscaleRuntimeMissingReason {
		t.Fatalf("runtime reason = %q", state.Reason)
	}

	_, planErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{"/tmp/missing.png"}, nil))
	if planErr == nil {
		t.Fatal("expected the plan to fail without the runtime")
	}
	if planErr.Code != "RUNTIME_DEP_MISSING" || planErr.DetailCode != UpscaleCodeRuntimeUnavailable {
		t.Fatalf("unexpected dependency error: %+v", planErr)
	}
	if !strings.Contains(planErr.Message, "upscale pack") {
		t.Fatalf("expected the runtime message to name the pack, got %q", planErr.Message)
	}
}

func TestUpscalePlanReportsMissingModel(t *testing.T) {
	dataDir, _ := withUpscaleTestRuntime(t)
	input := filepath.Join(dataDir, "photo.png")
	writeUpscalePNG(t, input, 8, 8)

	tool := NewUpscaleTool()
	_, planErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{input}, map[string]any{
		"scale": 2,
	}))
	if planErr == nil {
		t.Fatal("expected the plan to fail without the model")
	}
	if planErr.Code != "RUNTIME_DEP_MISSING" || planErr.DetailCode != UpscaleCodeModelUnavailable {
		t.Fatalf("unexpected model dependency error: %+v", planErr)
	}
	if !strings.Contains(planErr.Message, "upscale model") {
		t.Fatalf("expected the model message to name the model, got %q", planErr.Message)
	}

	if state := tool.RuntimeState(context.Background()); state.Status != "degraded" {
		t.Fatalf("expected a degraded runtime state without the model, got %+v", state)
	}

	_, animeErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{input}, map[string]any{
		"model": "anime",
	}))
	if animeErr == nil || animeErr.DetailCode != UpscaleCodeModelUnavailable {
		t.Fatalf("expected the anime choice to require a custom model: %+v", animeErr)
	}
	if !strings.Contains(animeErr.Message, "anime model") {
		t.Fatalf("expected the anime guidance, got %q", animeErr.Message)
	}
}

func TestUpscalePlanBatchAndCollisionNaming(t *testing.T) {
	dataDir, _ := withUpscaleTestRuntime(t)
	first := filepath.Join(dataDir, "a.png")
	second := filepath.Join(dataDir, "b.png")
	writeUpscalePNG(t, first, 8, 8)
	writeUpscalePNG(t, second, 8, 8)
	existing := filepath.Join(dataDir, "a_2x.png")
	writeUpscalePNG(t, existing, 16, 16)
	model := fakeCustomUpscaleModel(t, dataDir)

	tool := NewUpscaleTool()
	plan, planErr := tool.Plan(context.Background(), upscaleTestRequest("batch", []string{first, second}, map[string]any{
		"scale":     2,
		"modelPath": model,
	}))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected two planned outputs, got %+v", plan.Outputs)
	}
	if got := filepath.Base(plan.Outputs[0].OutputPath); got != "a_2x-2.png" {
		t.Fatalf("expected the collision-safe name a_2x-2.png, got %q", got)
	}
	if !plan.Outputs[0].Adjusted || len(plan.Outputs[0].Warnings) == 0 {
		t.Fatalf("expected the adjusted output to carry a warning: %+v", plan.Outputs[0])
	}
	if got := filepath.Base(plan.Outputs[1].OutputPath); got != "b_2x.png" {
		t.Fatalf("expected b_2x.png, got %q", got)
	}
	if plan.Message != "Ready to upscale 2 images 2x." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}
	if plan.EffectiveOptions["scale"] != 2 || plan.EffectiveOptions["format"] != "png" {
		t.Fatalf("unexpected effective options: %+v", plan.EffectiveOptions)
	}
}

func TestUpscalePlanUsesTheRequestedScaleSuffix(t *testing.T) {
	dataDir, _ := withUpscaleTestRuntime(t)
	input := filepath.Join(dataDir, "photo.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dataDir)

	tool := NewUpscaleTool()
	plan, planErr := tool.Plan(context.Background(), upscaleTestRequest("single", []string{input}, map[string]any{
		"scale":     4,
		"modelPath": model,
		"format":    "jpeg",
	}))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	if got := filepath.Base(plan.Outputs[0].OutputPath); got != "photo_4x.jpg" {
		t.Fatalf("expected photo_4x.jpg, got %q", got)
	}
}

func TestUpscaleRunsBridgeWithExpectedArguments(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	output := filepath.Join(dir, "photo_4x.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)
	python := fakeUpscalePython(t, dir)

	runner := &upscaleStubRunner{}
	runner.handler = func(_ context.Context, call upscaleStubCall, onLine func(string)) error {
		outDir := call.argAfter("--out-dir")
		generated := filepath.Join(outDir, "out-0.png")
		writeUpscalePNG(t, generated, 32, 32)
		onLine(upscaleEvent(t, "done", 0, 32, 32, generated))
		onLine(upscaleEvent(t, "progress", 0, 0, 0, ""))

		return nil
	}

	outcomes, err := UpscaleImages(context.Background(), runner, python, model, "", []UpscaleItem{
		{InputPath: input, OutputPath: output},
	}, UpscaleOptions{Scale: 4, TileSize: 256, Format: "png"}, nil)
	if err != nil {
		t.Fatalf("unexpected upscale error: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Width != 32 || outcomes[0].Height != 32 {
		t.Fatalf("unexpected outcomes: %+v", outcomes)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected the published output: %v", err)
	}

	calls := runner.recorded()
	if len(calls) != 1 {
		t.Fatalf("expected one bridge process, got %d", len(calls))
	}
	call := calls[0]
	if call.pythonPath != python {
		t.Fatalf("interpreter = %q", call.pythonPath)
	}
	if call.argAfter("--model") != model {
		t.Fatalf("model = %q", call.argAfter("--model"))
	}
	if call.argAfter("--scale") != "4" || call.argAfter("--tile") != "256" || call.argAfter("--format") != "png" {
		t.Fatalf("unexpected bridge flags: %v", call.args)
	}
	if !strings.HasSuffix(call.argAfter("--out-dir"), string(filepath.Separator)+"out-0") {
		t.Fatalf("unexpected out dir: %q", call.argAfter("--out-dir"))
	}
	if last := call.args[len(call.args)-1]; last != absoluteImagePath(input) {
		t.Fatalf("input argument = %q, want %q", last, absoluteImagePath(input))
	}
	if !containsString(call.env, "PYTHONNOUSERSITE=1") {
		t.Fatalf("expected a sanitized environment, got %v", call.env)
	}
}

func TestUpscaleRejectsWrongOutputDimensions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	output := filepath.Join(dir, "photo_2x.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)
	python := fakeUpscalePython(t, dir)

	runner := &upscaleStubRunner{}
	runner.handler = func(_ context.Context, call upscaleStubCall, onLine func(string)) error {
		generated := filepath.Join(call.argAfter("--out-dir"), "out-0.png")
		writeUpscalePNG(t, generated, 10, 10)
		onLine(upscaleEvent(t, "done", 0, 10, 10, generated))

		return nil
	}

	_, err := UpscaleImages(context.Background(), runner, python, model, "", []UpscaleItem{
		{InputPath: input, OutputPath: output},
	}, UpscaleOptions{Scale: 2, TileSize: 128, Format: "png"}, nil)
	if err == nil {
		t.Fatal("expected a wrong-size output to fail verification")
	}

	var upscaleErr *UpscaleError
	if !errors.As(err, &upscaleErr) {
		t.Fatalf("expected an UpscaleError, got %T", err)
	}
	if len(upscaleErr.Failures) != 1 || upscaleErr.Failures[0].Code != UpscaleCodeVerifyFailed {
		t.Fatalf("expected a verification failure, got %+v", upscaleErr.Failures)
	}
	if !strings.Contains(upscaleErr.Failures[0].Cause.Error(), "want 16x16") {
		t.Fatalf("expected the expected dimensions in the cause, got %v", upscaleErr.Failures[0].Cause)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed verification must not publish output, got %v", statErr)
	}
}

func TestUpscaleRejectsWrongOutputFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	output := filepath.Join(dir, "photo_2x.webp")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)
	python := fakeUpscalePython(t, dir)

	runner := &upscaleStubRunner{}
	runner.handler = func(_ context.Context, call upscaleStubCall, onLine func(string)) error {
		generated := filepath.Join(call.argAfter("--out-dir"), "out-0.webp")
		writeUpscalePNG(t, generated, 16, 16)
		onLine(upscaleEvent(t, "done", 0, 16, 16, generated))

		return nil
	}

	_, err := UpscaleImages(context.Background(), runner, python, model, "", []UpscaleItem{
		{InputPath: input, OutputPath: output},
	}, UpscaleOptions{Scale: 2, TileSize: 128, Format: "webp"}, nil)
	if err == nil {
		t.Fatal("expected a PNG payload in a .webp file to fail verification")
	}
	var upscaleErr *UpscaleError
	if !errors.As(err, &upscaleErr) || upscaleErr.Failures[0].Code != UpscaleCodeVerifyFailed {
		t.Fatalf("expected a verification failure, got %+v", err)
	}
}

func TestUpscaleCancellationRollsBackPublishedOutputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	output := filepath.Join(dir, "photo_2x.png")
	writeUpscalePNG(t, input, 8, 8)
	model := fakeCustomUpscaleModel(t, dir)
	python := fakeUpscalePython(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &upscaleStubRunner{}
	runner.handler = func(runCtx context.Context, call upscaleStubCall, onLine func(string)) error {
		generated := filepath.Join(call.argAfter("--out-dir"), "out-0.png")
		writeUpscalePNG(t, generated, 16, 16)
		onLine(upscaleEvent(t, "done", 0, 16, 16, generated))
		cancel()
		<-runCtx.Done()

		return runCtx.Err()
	}

	_, err := UpscaleImages(ctx, runner, python, model, "", []UpscaleItem{
		{InputPath: input, OutputPath: output},
	}, UpscaleOptions{Scale: 2, TileSize: 128, Format: "png"}, nil)
	if err == nil {
		t.Fatal("expected a cancellation")
	}
	var upscaleErr *UpscaleError
	if !errors.As(err, &upscaleErr) || upscaleErr.Code != UpscaleCodeCancelled {
		t.Fatalf("expected a cancelled error, got %+v", err)
	}
	if _, statErr := os.Stat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancellation must roll back published output, got %v", statErr)
	}
}

func TestUpscaleBridgeSourcePinsTheValidatedContract(t *testing.T) {
	source := string(upscaleBridgeSource)
	for _, needle := range []string{
		"--model", "--scale", "--tile", "--format", "--quality", "--out-dir",
		`"progress"`, `"done"`, `"error"`, "CPUExecutionProvider",
	} {
		if !strings.Contains(source, needle) {
			t.Fatalf("bridge source is missing %q", needle)
		}
	}
}

func TestUpscaleRunnerBoundsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stderr fixture uses a POSIX shell")
	}

	runner := &ExecUpscaleCommandRunner{}
	err := runner.RunUpscale(context.Background(), t.TempDir(), "/bin/sh", []string{
		"-c", "for i in $(seq 1 200); do echo line-$i >&2; done; exit 3",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected the command to fail")
	}
	var commandErr *UpscaleCommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("expected an UpscaleCommandError, got %T", err)
	}
	if !strings.Contains(commandErr.Stderr, "line-200") {
		t.Fatalf("expected the newest stderr line, got %q", commandErr.Stderr)
	}
	if strings.Contains(commandErr.Stderr, "line-1\n") {
		t.Fatalf("expected old stderr lines to be dropped, got %q", commandErr.Stderr)
	}
	if len(commandErr.Stderr) > rembgStderrMaxBytes {
		t.Fatalf("stderr tail is not bounded: %d bytes", len(commandErr.Stderr))
	}
	lines := strings.Count(commandErr.Stderr, "\n") + 1
	if lines > rembgStderrMaxLines {
		t.Fatalf("stderr tail has %d lines, want at most %d", lines, rembgStderrMaxLines)
	}
}

func TestUpscaleRunnerCancellationKillsTheProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the process-tree fixture uses a POSIX shell")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "alive")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &ExecUpscaleCommandRunner{}
	started := make(chan struct{})
	go func() {
		close(started)
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	<-started

	startedAt := time.Now()
	err := runner.RunUpscale(ctx, dir, "/bin/sh", []string{
		// The background subshell would survive killing only the direct child
		// and would create the marker; only a process-group kill stops it.
		"-c", "sleep 1; echo alive > " + marker + " & wait",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected the killed process to report an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected a context cancellation error, got %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %s; the process tree was not killed", elapsed)
	}

	time.Sleep(1200 * time.Millisecond)
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the child process survived cancellation, marker exists: %v", statErr)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}
