package image

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/toolchain"
)

func backgroundRemoveTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDImageBackground,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func backgroundRemoveTestRuntime() backgroundRemoveRuntime {
	model := toolchain.DefaultRembgModel
	home := filepath.Join("managed", "rembg")

	return backgroundRemoveRuntime{
		PythonPath: filepath.Join("managed", "rembg-venv", "bin", "python"),
		HomeDir:    home,
		ModelPath:  filepath.Join(home, "models", model, model+".onnx"),
		Model:      model,
	}
}

func stubBackgroundRemoveRuntime(t *testing.T, runtime backgroundRemoveRuntime, runtimeErr *BackgroundRemoveError) {
	t.Helper()

	previous := resolveBackgroundRemoveRuntime
	resolveBackgroundRemoveRuntime = func() (backgroundRemoveRuntime, *BackgroundRemoveError) {
		return runtime, runtimeErr
	}
	t.Cleanup(func() {
		resolveBackgroundRemoveRuntime = previous
	})
}

type backgroundRemoveEngineCapture struct {
	PythonPath string
	ModelPath  string
	WorkingDir string
	Items      []BackgroundRemoveItem
	Options    BackgroundRemoveOptions
}

type backgroundRemoveEngineFunc func(
	ctx context.Context,
	runner RembgCommandRunner,
	pythonPath, modelPath, workingDir string,
	items []BackgroundRemoveItem,
	opts BackgroundRemoveOptions,
	onProgress func(current, total int),
) ([]BackgroundRemoveOutcome, error)

func stubBackgroundRemoveEngine(t *testing.T, fn backgroundRemoveEngineFunc) *[]backgroundRemoveEngineCapture {
	t.Helper()

	captures := make([]backgroundRemoveEngineCapture, 0, 1)
	previous := runBackgroundRemove
	runBackgroundRemove = func(
		ctx context.Context,
		runner RembgCommandRunner,
		pythonPath, modelPath, workingDir string,
		items []BackgroundRemoveItem,
		opts BackgroundRemoveOptions,
		onProgress func(current, total int),
	) ([]BackgroundRemoveOutcome, error) {
		captures = append(captures, backgroundRemoveEngineCapture{
			PythonPath: pythonPath,
			ModelPath:  modelPath,
			WorkingDir: workingDir,
			Items:      append([]BackgroundRemoveItem{}, items...),
			Options:    opts,
		})

		return fn(ctx, runner, pythonPath, modelPath, workingDir, items, opts, onProgress)
	}
	t.Cleanup(func() {
		runBackgroundRemove = previous
	})

	return &captures
}

func writeBackgroundRemoveAnimatedGIF(t *testing.T, path string) {
	t.Helper()

	palette := color.Palette{color.NRGBA{R: 30, G: 160, B: 220, A: 255}, color.NRGBA{R: 240, G: 40, B: 40, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 12, 10), palette)
	second := image.NewPaletted(image.Rect(0, 0, 12, 10), palette)
	for x := 0; x < 12; x++ {
		first.SetColorIndex(x, 0, 0)
		second.SetColorIndex(x, 9, 1)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create animated gif: %v", err)
	}
	if err := gif.EncodeAll(file, &gif.GIF{
		Image: []*image.Paletted{first, second},
		Delay: []int{10, 10},
	}); err != nil {
		_ = file.Close()
		t.Fatalf("encode animated gif: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close animated gif: %v", err)
	}
}

func TestBackgroundRemoveToolManifest(t *testing.T) {
	tool := NewBackgroundRemoveTool()
	manifest := tool.Manifest()

	if manifest.ToolID != "tool.image.background" {
		t.Fatalf("expected tool.image.background, got %q", manifest.ToolID)
	}
	if manifest.Name != "Remove Background" {
		t.Fatalf("expected Remove Background, got %q", manifest.Name)
	}
	if manifest.Description == "" {
		t.Fatal("expected a description")
	}
	if manifest.Domain != "image" {
		t.Fatalf("expected domain image, got %q", manifest.Domain)
	}
	if manifest.Capability != ToolIDImageBackground {
		t.Fatalf("expected capability %q, got %q", ToolIDImageBackground, manifest.Capability)
	}
	if manifest.Version != "1.0" {
		t.Fatalf("expected version 1.0, got %q", manifest.Version)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatalf("expected single and batch support, got %+v", manifest)
	}
	if !slices.Equal(manifest.InputExtensions, imageInputExtensions) {
		t.Fatalf("expected the shared image inputs, got %v", manifest.InputExtensions)
	}
	if !slices.Equal(manifest.OutputExtensions, []string{"png"}) {
		t.Fatalf("expected png output only, got %v", manifest.OutputExtensions)
	}
	if !slices.Equal(manifest.RuntimeDeps, []string{"rembg", "rembg-model"}) {
		t.Fatalf("expected rembg runtime deps, got %v", manifest.RuntimeDeps)
	}
	for _, tag := range []string{"image", "background", "rembg"} {
		if !slices.Contains(manifest.Tags, tag) {
			t.Fatalf("expected tag %q in %v", tag, manifest.Tags)
		}
	}
	if tool.ID() != manifest.ToolID || tool.Capability() != manifest.Capability {
		t.Fatal("expected ID and Capability to match the manifest")
	}
}

func TestBackgroundRemoveToolPlanDefaults(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	runtime := backgroundRemoveTestRuntime()
	stubBackgroundRemoveRuntime(t, runtime, nil)
	engineCalls := stubBackgroundRemoveEngine(t, func(context.Context, RembgCommandRunner, string, string, string, []BackgroundRemoveItem, BackgroundRemoveOptions, func(int, int)) ([]BackgroundRemoveOutcome, error) {
		t.Fatal("planning must not run the engine")
		return nil, nil
	})
	tool := NewBackgroundRemoveTool()

	plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a success plan")
	}
	if plan.Message != "Ready to remove the background of 1 image." {
		t.Fatalf("unexpected plan message %q", plan.Message)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected one planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo_nobg.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != imageOutputKindFile || output.OutputCount != 1 {
		t.Fatalf("unexpected output metadata: %+v", output)
	}
	if output.Exists || output.Adjusted {
		t.Fatalf("expected a fresh, unadjusted output, got exists=%v adjusted=%v", output.Exists, output.Adjusted)
	}
	if _, statErr := os.Stat(output.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("plan must not create the output file")
	}
	if got := plan.EffectiveOptions["backgroundColor"]; got != "" {
		t.Fatalf("expected transparent output by default, got %v", got)
	}
	if len(*engineCalls) != 0 {
		t.Fatalf("expected no engine calls, got %v", *engineCalls)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", plan.Warnings)
	}
}

func TestBackgroundRemoveToolPlanCollisions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)
	existing := filepath.Join(dir, "photo_nobg.png")
	if err := os.WriteFile(existing, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write collision fixture: %v", err)
	}

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected the collision plan to succeed, got %v", planErr)
	}
	if want := filepath.Join(dir, "photo_nobg-2.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if !plan.Outputs[0].Adjusted || len(plan.Outputs[0].Warnings) == 0 {
		t.Fatalf("expected an adjustment warning, got %+v", plan.Outputs[0])
	}
	if !strings.Contains(plan.Outputs[0].Warnings[0], "is already in use") {
		t.Fatalf("expected an in-use warning, got %q", plan.Outputs[0].Warnings[0])
	}
	content, readErr := os.ReadFile(existing)
	if readErr != nil || string(content) != "existing" {
		t.Fatalf("expected the collision fixture untouched, got %q (%v)", content, readErr)
	}
}

func TestBackgroundRemoveToolPlanReservesInputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	// The second input has the natural output name of the first one. The plan
	// must never resolve one input's output onto another input.
	otherInput := filepath.Join(dir, "photo_nobg.png")
	writePlanPNG(t, otherInput, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	batchPlan, batchErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("batch", []string{input, otherInput}, nil))
	if batchErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", batchErr)
	}
	if len(batchPlan.Outputs) != 2 {
		t.Fatalf("expected two outputs, got %d", len(batchPlan.Outputs))
	}
	if want := filepath.Join(dir, "photo_nobg-2.png"); batchPlan.Outputs[0].OutputPath != want {
		t.Fatalf("expected the first output to skip the reserved input %q, got %q", want, batchPlan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(dir, "photo_nobg_nobg.png"); batchPlan.Outputs[1].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, batchPlan.Outputs[1].OutputPath)
	}
	for _, output := range batchPlan.Outputs {
		if fsx.SameFile(output.OutputPath, input) || fsx.SameFile(output.OutputPath, otherInput) {
			t.Fatalf("planned output %q collides with a batch input", output.OutputPath)
		}
	}
}

func TestBackgroundRemoveToolPlanOutputDir(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	input := filepath.Join(inputDir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, map[string]any{"outputDir": outputDir}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if want := filepath.Join(outputDir, "photo_nobg.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if outputDir == inputDir {
		t.Fatal("test setup error: output and input folders must differ")
	}

	missingPlan, missingErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, map[string]any{"outputDir": filepath.Join(outputDir, "missing")}))
	if missingErr == nil {
		t.Fatal("expected a missing output folder to fail")
	}
	if missingErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected %s, got %q", models.ErrorCodeValidationInvalidInput, missingErr.Code)
	}
	if missingPlan.Success {
		t.Fatal("expected the failed plan to report failure")
	}
}

func TestBackgroundRemoveToolPlanWarnsAnimatedGIF(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "animated.gif")
	writeBackgroundRemoveAnimatedGIF(t, input)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	want := "animated.gif: " + imageBackgroundAnimatedWarning
	if !slices.Contains(plan.Warnings, want) {
		t.Fatalf("expected the animated GIF warning %q, got %v", want, plan.Warnings)
	}
	if wantOutput := filepath.Join(dir, "animated_nobg.png"); plan.Outputs[0].OutputPath != wantOutput {
		t.Fatalf("expected %q, got %q", wantOutput, plan.Outputs[0].OutputPath)
	}
}

func TestBackgroundRemoveToolPlanRuntimeStates(t *testing.T) {
	input := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, input, 40, 30)

	tests := []struct {
		name        string
		runtimeErr  *BackgroundRemoveError
		wantStatus  string
		wantReason  string
		wantJobCode string
		wantDetail  string
		wantHealthy bool
	}{
		{
			name: "missing venv",
			runtimeErr: &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeRuntimeUnavailable,
				Message: BackgroundRemoveRuntimeMissingReason,
			},
			wantStatus:  "degraded",
			wantReason:  BackgroundRemoveRuntimeMissingReason,
			wantJobCode: models.ErrorCodeRuntimeDepMissing,
			wantDetail:  BackgroundRemoveCodeRuntimeUnavailable,
			wantHealthy: false,
		},
		{
			name: "missing or short model",
			runtimeErr: &BackgroundRemoveError{
				Code:    BackgroundRemoveCodeModelUnavailable,
				Message: BackgroundRemoveModelMissingReason,
			},
			wantStatus:  "degraded",
			wantReason:  BackgroundRemoveModelMissingReason,
			wantJobCode: models.ErrorCodeRuntimeDepMissing,
			wantDetail:  BackgroundRemoveCodeModelUnavailable,
			wantHealthy: false,
		},
		{
			name:        "ready",
			wantStatus:  "enabled",
			wantHealthy: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), test.runtimeErr)
			tool := NewBackgroundRemoveTool()

			state := tool.RuntimeState(context.Background())
			if state.Status != test.wantStatus || state.Healthy != test.wantHealthy {
				t.Fatalf("unexpected runtime state: %+v", state)
			}
			if state.Reason != test.wantReason {
				t.Fatalf("expected reason %q, got %q", test.wantReason, state.Reason)
			}

			plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
			if test.runtimeErr == nil {
				if planErr != nil {
					t.Fatalf("expected the plan to succeed, got %v", planErr)
				}
				return
			}

			if planErr == nil {
				t.Fatal("expected the plan to fail without the pack")
			}
			if planErr.Code != test.wantJobCode || planErr.DetailCode != test.wantDetail {
				t.Fatalf("expected %s/%s, got %s/%s", test.wantJobCode, test.wantDetail, planErr.Code, planErr.DetailCode)
			}
			if !strings.Contains(planErr.Message, "Open Settings and run Install / repair") {
				t.Fatalf("expected the repair hint, got %q", planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected the failed plan to report failure")
			}
		})
	}
}

func TestResolveBackgroundRemoveRuntimeAt(t *testing.T) {
	dataDir := t.TempDir()
	root := toolchain.ToolchainDir(dataDir)
	pythonPath, _ := toolchain.ManagedRembgPythonPath(dataDir)
	spec := toolchain.DefaultRembgModelSpec()
	modelPath, _ := toolchain.ManagedRembgModelPath(dataDir, spec.Model)
	homeDir, _ := toolchain.ManagedRembgHome(dataDir)

	sitePackages := filepath.Join(root, "rembg-venv", "lib", "python3.11", "site-packages")
	if runtime.GOOS == "windows" {
		sitePackages = filepath.Join(root, "rembg-venv", "Lib", "site-packages")
	}

	if _, runtimeErr := resolveBackgroundRemoveRuntimeAt(dataDir); runtimeErr == nil || runtimeErr.Code != BackgroundRemoveCodeRuntimeUnavailable {
		t.Fatalf("expected %s without the venv, got %v", BackgroundRemoveCodeRuntimeUnavailable, runtimeErr)
	}

	if err := os.MkdirAll(filepath.Dir(pythonPath), 0o755); err != nil {
		t.Fatalf("create venv dir: %v", err)
	}
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write venv python: %v", err)
	}
	if _, runtimeErr := resolveBackgroundRemoveRuntimeAt(dataDir); runtimeErr == nil || runtimeErr.Code != BackgroundRemoveCodeModelUnavailable {
		t.Fatalf("expected %s without the model, got %v", BackgroundRemoveCodeModelUnavailable, runtimeErr)
	}

	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatalf("create model dir: %v", err)
	}
	if err := os.WriteFile(modelPath, []byte("short"), 0o644); err != nil {
		t.Fatalf("write short model: %v", err)
	}
	if _, runtimeErr := resolveBackgroundRemoveRuntimeAt(dataDir); runtimeErr == nil || runtimeErr.Code != BackgroundRemoveCodeModelUnavailable {
		t.Fatalf("expected a short model to be rejected, got %v", runtimeErr)
	}

	if err := os.Truncate(modelPath, spec.SizeBytes); err != nil {
		t.Fatalf("size model fixture: %v", err)
	}
	if _, runtimeErr := resolveBackgroundRemoveRuntimeAt(dataDir); runtimeErr == nil || runtimeErr.Code != BackgroundRemoveCodeRuntimeUnavailable {
		t.Fatalf("expected a venv without packages to be rejected, got %v", runtimeErr)
	} else if !strings.Contains(runtimeErr.Message, "Open Settings and run Install / repair") {
		t.Fatalf("expected the Settings hint, got %q", runtimeErr.Message)
	}

	if err := os.MkdirAll(filepath.Join(sitePackages, "rembg"), 0o755); err != nil {
		t.Fatalf("create rembg package dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sitePackages, "rembg", "__init__.py"), []byte("# rembg\n"), 0o644); err != nil {
		t.Fatalf("write rembg package marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sitePackages, "onnxruntime-1.32.0.dist-info"), 0o755); err != nil {
		t.Fatalf("create onnxruntime dist-info: %v", err)
	}
	if _, runtimeErr := resolveBackgroundRemoveRuntimeAt(dataDir); runtimeErr == nil || runtimeErr.Code != BackgroundRemoveCodeRuntimeUnavailable {
		t.Fatalf("expected a missing .ready marker to be rejected, got %v", runtimeErr)
	} else if !strings.Contains(runtimeErr.Message, "not verified") {
		t.Fatalf("expected the not-verified reason, got %q", runtimeErr.Message)
	}

	if err := os.WriteFile(filepath.Join(homeDir, ".ready"), []byte("ready"), 0o644); err != nil {
		t.Fatalf("write the readiness marker: %v", err)
	}
	resolved, runtimeErr := resolveBackgroundRemoveRuntimeAt(dataDir)
	if runtimeErr != nil {
		t.Fatalf("expected a ready pack, got %v", runtimeErr)
	}
	if resolved.PythonPath != pythonPath || resolved.ModelPath != modelPath || resolved.HomeDir != homeDir || resolved.Model != spec.Model {
		t.Fatalf("unexpected runtime paths: %+v", resolved)
	}
}

func TestBackgroundRemoveToolBackgroundColorOption(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	tests := []struct {
		name       string
		options    map[string]any
		wantColor  string
		wantDetail string
	}{
		{name: "absent", options: nil, wantColor: ""},
		{name: "empty", options: map[string]any{"backgroundColor": ""}, wantColor: ""},
		{name: "upper case", options: map[string]any{"backgroundColor": "#A1B2C3"}, wantColor: "#a1b2c3"},
		{name: "padded", options: map[string]any{"backgroundColor": " #a1b2c3 "}, wantColor: "#a1b2c3"},
		{name: "named color", options: map[string]any{"backgroundColor": "red"}, wantDetail: BackgroundRemoveCodeInvalidOption},
		{name: "short hex", options: map[string]any{"backgroundColor": "#12345"}, wantDetail: BackgroundRemoveCodeInvalidOption},
		{name: "non hex", options: map[string]any{"backgroundColor": "#GGGGGG"}, wantDetail: BackgroundRemoveCodeInvalidOption},
		{name: "eight digits", options: map[string]any{"backgroundColor": "#a1b2c3ff"}, wantDetail: BackgroundRemoveCodeInvalidOption},
		{name: "wrong type", options: map[string]any{"backgroundColor": 123}, wantDetail: BackgroundRemoveCodeInvalidOption},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, test.options))
			if test.wantDetail != "" {
				if planErr == nil {
					t.Fatal("expected the color to be rejected")
				}
				if planErr.DetailCode != test.wantDetail {
					t.Fatalf("expected %q, got %q", test.wantDetail, planErr.DetailCode)
				}
				if planErr.Code != models.ErrorCodeValidationInvalidInput {
					t.Fatalf("expected %s, got %q", models.ErrorCodeValidationInvalidInput, planErr.Code)
				}
				if plan.Success {
					t.Fatal("expected the failed plan to report failure")
				}
				return
			}

			if planErr != nil {
				t.Fatalf("expected the plan to succeed, got %v", planErr)
			}
			if got := plan.EffectiveOptions["backgroundColor"]; got != test.wantColor {
				t.Fatalf("expected effective color %q, got %v", test.wantColor, got)
			}
		})
	}
}

func TestBackgroundRemoveToolExecuteSingle(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	runtime := backgroundRemoveTestRuntime()
	stubBackgroundRemoveRuntime(t, runtime, nil)
	captures := stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, onProgress func(int, int)) ([]BackgroundRemoveOutcome, error) {
		onProgress(1, 1)
		return []BackgroundRemoveOutcome{{OutputPath: items[0].OutputPath, Width: 1024, Height: 768}}, nil
	})
	tool := NewBackgroundRemoveTool()

	var progress []models.JobProgress
	item, jobErr := tool.ExecuteSingleWithProgress(
		context.Background(),
		backgroundRemoveTestRequest("single", []string{input}, map[string]any{"backgroundColor": "#A1B2C3"}),
		func(update models.JobProgress) {
			progress = append(progress, update)
		},
	)
	if jobErr != nil {
		t.Fatalf("expected the single run to succeed, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful result item, got %+v", item)
	}
	if want := filepath.Join(dir, "photo_nobg.png"); item.OutputPath != want {
		t.Fatalf("expected %q, got %q", want, item.OutputPath)
	}
	if item.Message != "background #a1b2c3, 1024×768" {
		t.Fatalf("unexpected result message %q", item.Message)
	}
	if !slices.Equal(item.Outputs, []string{item.OutputPath}) || item.OutputCount != 1 {
		t.Fatalf("unexpected outputs: %+v", item)
	}

	if len(*captures) != 1 {
		t.Fatalf("expected one engine call, got %d", len(*captures))
	}
	capture := (*captures)[0]
	if capture.PythonPath != runtime.PythonPath || capture.ModelPath != runtime.ModelPath {
		t.Fatalf("expected the resolved runtime, got %+v", capture)
	}
	if capture.WorkingDir != "" {
		t.Fatalf("expected the OS temp workspace, got %q", capture.WorkingDir)
	}
	if capture.Options.BackgroundColor != "#a1b2c3" || capture.Options.Model != toolchain.DefaultRembgModel {
		t.Fatalf("unexpected options: %+v", capture.Options)
	}
	if len(capture.Items) != 1 || capture.Items[0].InputPath != input {
		t.Fatalf("unexpected items: %+v", capture.Items)
	}

	messages := make([]string, 0, len(progress))
	for _, update := range progress {
		messages = append(messages, update.Message)
		if update.Total != 1 {
			t.Fatalf("expected total 1, got %+v", update)
		}
	}
	if !slices.Contains(messages, "removing background 1/1") {
		t.Fatalf("expected the start message, got %v", messages)
	}
	if messages[len(messages)-1] != "processed 1/1" {
		t.Fatalf("expected the final progress to be processed 1/1, got %v", messages)
	}
}

func TestBackgroundRemoveToolExecuteSingleTransparentMessage(t *testing.T) {
	input := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, input, 1024, 768)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, opts BackgroundRemoveOptions, _ func(int, int)) ([]BackgroundRemoveOutcome, error) {
		if opts.BackgroundColor != "" {
			t.Fatalf("expected transparent output, got %+v", opts)
		}
		return []BackgroundRemoveOutcome{{OutputPath: items[0].OutputPath, Width: 1024, Height: 768}}, nil
	})
	tool := NewBackgroundRemoveTool()

	item, jobErr := tool.ExecuteSingle(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
	if jobErr != nil {
		t.Fatalf("expected the run to succeed, got %v", jobErr)
	}
	if item.Message != "transparent PNG 1024×768" {
		t.Fatalf("unexpected result message %q", item.Message)
	}
}

func TestBackgroundRemoveToolExecuteBatchProgressAndFailures(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
		filepath.Join(dir, "third.png"),
	}
	for _, input := range inputs {
		writePlanPNG(t, input, 40, 30)
	}

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	captures := stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, onProgress func(int, int)) ([]BackgroundRemoveOutcome, error) {
		onProgress(1, len(items))
		onProgress(2, len(items))
		onProgress(3, len(items))

		return []BackgroundRemoveOutcome{
			{OutputPath: items[0].OutputPath, Width: 40, Height: 30},
			{OutputPath: items[2].OutputPath, Width: 40, Height: 30},
		}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeBatchFailed,
			Message: "1 of 3 images could not be processed.",
			Failures: []BackgroundRemoveItemFailure{{
				Index:      1,
				InputPath:  inputs[1],
				OutputPath: strings.Replace(inputs[1], "second.png", "second_nobg.png", 1),
				Code:       BackgroundRemoveCodeProcessFailed,
				Message:    "Could not remove the background from second.png.",
				Cause:      errors.New("raw onnxruntime error"),
			}},
		}
	})
	tool := NewBackgroundRemoveTool()

	var progress []models.JobProgress
	items, jobErr := tool.ExecuteBatch(
		context.Background(),
		backgroundRemoveTestRequest("batch", inputs, nil),
		func(update models.JobProgress) {
			progress = append(progress, update)
		},
	)
	if jobErr == nil {
		t.Fatal("expected the batch to report the item failure")
	}
	if jobErr.Code != models.ErrorCodeExecIOTransient || jobErr.DetailCode != BackgroundRemoveCodeProcessFailed {
		t.Fatalf("unexpected job error: %+v", jobErr)
	}
	if len(items) != 3 {
		t.Fatalf("expected three result items, got %d", len(items))
	}
	if !items[0].Success || !items[2].Success {
		t.Fatalf("expected the valid items to succeed: %+v", items)
	}
	if items[1].Success {
		t.Fatalf("expected the middle item to fail: %+v", items[1])
	}
	if items[1].Error == nil || items[1].Error.Details["error"] != "raw onnxruntime error" {
		t.Fatalf("expected the raw cause only in details, got %+v", items[1].Error)
	}
	if items[0].Message != "transparent PNG 40×30" {
		t.Fatalf("unexpected success message %q", items[0].Message)
	}

	if len(*captures) != 1 {
		t.Fatalf("expected one engine call for the whole batch, got %d", len(*captures))
	}
	if len((*captures)[0].Items) != 3 {
		t.Fatalf("expected all three items in one process, got %+v", (*captures)[0].Items)
	}

	messages := make([]string, 0, len(progress))
	for _, update := range progress {
		messages = append(messages, update.Message)
		if update.Total != 3 {
			t.Fatalf("expected total 3, got %+v", update)
		}
	}
	for _, want := range []string{"processed 1/3", "processed 2/3", "processed 3/3"} {
		if !slices.Contains(messages, want) {
			t.Fatalf("expected %q in progress %v", want, messages)
		}
	}
}

func TestBackgroundRemoveToolExecuteBatchCancelledLastItem(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
		filepath.Join(dir, "last.png"),
	}
	for _, input := range inputs {
		writePlanPNG(t, input, 40, 30)
	}

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, onProgress func(int, int)) ([]BackgroundRemoveOutcome, error) {
		onProgress(1, len(items))
		onProgress(2, len(items))
		cancel()

		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeCancelled,
			Message: "background removal was canceled",
			Cause:   context.Canceled,
		}
	})
	tool := NewBackgroundRemoveTool()

	items, jobErr := tool.ExecuteBatch(ctx, backgroundRemoveTestRequest("batch", inputs, nil), nil)
	if jobErr == nil {
		t.Fatal("expected cancellation")
	}
	if jobErr.Code != models.ErrorCodeCancelledByUser || jobErr.DetailCode != BackgroundRemoveCodeCancelled {
		t.Fatalf("expected a canonical cancellation, got %+v", jobErr)
	}
	if !strings.Contains(jobErr.Message, "operation cancelled") {
		t.Fatalf("expected the cancellation message, got %q", jobErr.Message)
	}
	if len(items) != len(inputs) {
		t.Fatalf("expected one result per planned item, got %d", len(items))
	}
	for index, item := range items {
		if item.Success {
			t.Fatalf("expected item %d to be cancelled, got %+v", index, item)
		}
		if item.Error == nil || item.Error.DetailCode != BackgroundRemoveCodeCancelled {
			t.Fatalf("expected item %d to carry the cancellation, got %+v", index, item.Error)
		}
	}
	if items[len(items)-1].Error.Message != jobErr.Message {
		t.Fatalf("expected the last item to report the same cancellation, got %q", items[len(items)-1].Error.Message)
	}
}

func TestBackgroundRemoveToolExecuteBatchRequiresBatchMode(t *testing.T) {
	input := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, input, 10, 10)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	items, jobErr := tool.ExecuteBatch(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil), nil)
	if jobErr == nil || jobErr.DetailCode != "IMAGE_BACKGROUND_MODE_INVALID" {
		t.Fatalf("expected a mode error, got %v", jobErr)
	}
	if items != nil {
		t.Fatalf("expected no items, got %+v", items)
	}
}

func TestBackgroundRemoveToolExecuteSingleCancellation(t *testing.T) {
	input := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stubBackgroundRemoveEngine(t, func(context.Context, RembgCommandRunner, string, string, string, []BackgroundRemoveItem, BackgroundRemoveOptions, func(int, int)) ([]BackgroundRemoveOutcome, error) {
		cancel()
		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeCancelled,
			Message: "background removal was canceled",
			Cause:   context.Canceled,
		}
	})
	tool := NewBackgroundRemoveTool()

	item, jobErr := tool.ExecuteSingle(ctx, backgroundRemoveTestRequest("single", []string{input}, nil))
	if jobErr == nil || jobErr.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected a canonical cancellation, got %v", jobErr)
	}
	if item.Success {
		t.Fatalf("expected a failed item, got %+v", item)
	}
	if item.Message != jobErr.Message {
		t.Fatalf("expected the failure item to mirror the job message, got %q", item.Message)
	}
}

func TestBackgroundRemoveToolLateCancelKeepsSuccessfulBatch(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
	}
	for _, input := range inputs {
		writePlanPNG(t, input, 40, 30)
	}

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, onProgress func(int, int)) ([]BackgroundRemoveOutcome, error) {
		onProgress(1, len(items))
		onProgress(2, len(items))
		cancel()

		outcomes := make([]BackgroundRemoveOutcome, 0, len(items))
		for _, item := range items {
			outcomes = append(outcomes, BackgroundRemoveOutcome{OutputPath: item.OutputPath, Width: 40, Height: 30})
		}

		return outcomes, nil
	})
	tool := NewBackgroundRemoveTool()

	items, jobErr := tool.ExecuteBatch(ctx, backgroundRemoveTestRequest("batch", inputs, nil), nil)
	if jobErr != nil {
		t.Fatalf("a late ambient cancel must not cancel a successful batch, got %v", jobErr)
	}
	if len(items) != len(inputs) {
		t.Fatalf("expected %d result items, got %d", len(inputs), len(items))
	}
	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
	}
}

func TestBackgroundRemoveToolCancellationPropagatesCleanupErrors(t *testing.T) {
	input := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	stubBackgroundRemoveEngine(t, func(context.Context, RembgCommandRunner, string, string, string, []BackgroundRemoveItem, BackgroundRemoveOptions, func(int, int)) ([]BackgroundRemoveOutcome, error) {
		return nil, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeCancelled,
			Message: "background removal was canceled",
			Details: map[string]any{"cleanupErrors": []string{"photo_nobg.png: remove denied"}},
			Cause:   context.Canceled,
		}
	})
	tool := NewBackgroundRemoveTool()

	item, jobErr := tool.ExecuteSingle(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
	if jobErr == nil || jobErr.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected a canonical cancellation, got %v", jobErr)
	}
	cleanupErrors, _ := jobErr.Details["cleanupErrors"].([]string)
	if len(cleanupErrors) != 1 || !strings.Contains(cleanupErrors[0], "remove denied") {
		t.Fatalf("expected cleanup errors in the job details, got %+v", jobErr.Details)
	}
	if item.Error == nil || item.Error.Details["cleanupErrors"] == nil {
		t.Fatalf("expected the failure item to keep the cleanup errors, got %+v", item.Error)
	}
}

func TestBackgroundRemoveToolRequestValidationCodes(t *testing.T) {
	dir := t.TempDir()
	firstInput := filepath.Join(dir, "first.png")
	secondInput := filepath.Join(dir, "second.png")
	writePlanPNG(t, firstInput, 20, 10)
	writePlanPNG(t, secondInput, 20, 10)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	tool := NewBackgroundRemoveTool()

	tests := []struct {
		name       string
		mode       string
		inputs     []string
		wantDetail string
	}{
		{name: "missing inputs", mode: "single", inputs: nil, wantDetail: "IMAGE_BACKGROUND_INPUT_INVALID"},
		{name: "single with two inputs", mode: "single", inputs: []string{firstInput, secondInput}, wantDetail: "IMAGE_BACKGROUND_SINGLE_INPUT_INVALID"},
		{name: "empty input item", mode: "batch", inputs: []string{firstInput, " "}, wantDetail: "IMAGE_BACKGROUND_INPUT_INVALID"},
		{name: "invalid mode", mode: "", inputs: []string{firstInput}, wantDetail: "IMAGE_BACKGROUND_MODE_INVALID"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest(test.mode, test.inputs, nil))
			if planErr == nil {
				t.Fatal("expected the request to be rejected")
			}
			if planErr.Code != models.ErrorCodeValidationInvalidInput {
				t.Fatalf("expected %s, got %q", models.ErrorCodeValidationInvalidInput, planErr.Code)
			}
			if planErr.DetailCode != test.wantDetail {
				t.Fatalf("expected detail %q, got %q", test.wantDetail, planErr.DetailCode)
			}
		})
	}
}

func TestBackgroundRemoveToolPlanMissingPackDoesNotRunEngine(t *testing.T) {
	input := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveRuntime{}, &BackgroundRemoveError{
		Code:    BackgroundRemoveCodeRuntimeUnavailable,
		Message: BackgroundRemoveRuntimeMissingReason,
	})
	stubBackgroundRemoveEngine(t, func(context.Context, RembgCommandRunner, string, string, string, []BackgroundRemoveItem, BackgroundRemoveOptions, func(int, int)) ([]BackgroundRemoveOutcome, error) {
		t.Fatal("a missing pack must never spawn Python")
		return nil, nil
	})
	tool := NewBackgroundRemoveTool()

	plan, planErr := tool.Plan(context.Background(), backgroundRemoveTestRequest("single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected the missing pack to fail")
	}
	if planErr.Message != BackgroundRemoveRuntimeMissingReason {
		t.Fatalf("expected the repair reason, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected the failed plan to report failure")
	}
}

func TestBackgroundRemoveToolExecutePlanHonorsResolvedPlan(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	captures := stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, _ func(int, int)) ([]BackgroundRemoveOutcome, error) {
		return []BackgroundRemoveOutcome{{OutputPath: items[0].OutputPath, Width: 120, Height: 90}}, nil
	})
	tool := NewBackgroundRemoveTool()

	request := backgroundRemoveTestRequest("single", []string{input}, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}

	requestOutput := plan.Outputs[0].OutputPath
	plannedOutput := filepath.Join(dir, "resolved-plan.png")
	plan.Outputs[0].OutputPath = plannedOutput

	items, execErr := tool.ExecutePlan(context.Background(), request, plan, nil)
	if execErr != nil {
		t.Fatalf("expected the planned run to succeed, got %v", execErr)
	}
	if len(items) != 1 || !items[0].Success || items[0].OutputPath != plannedOutput {
		t.Fatalf("expected a success item at %q, got %+v", plannedOutput, items)
	}
	if len(*captures) != 1 || len((*captures)[0].Items) != 1 {
		t.Fatalf("expected one engine call with one item, got %+v", *captures)
	}
	if got := (*captures)[0].Items[0].OutputPath; got != plannedOutput {
		t.Fatalf("expected the engine to run the planned output %q, got %q", plannedOutput, got)
	}
	if requestOutput == plannedOutput {
		t.Fatal("test must mutate the planned output path")
	}
}

func TestBackgroundRemoveToolExecutePlanBatchSubset(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 40, 30)
	writePlanPNG(t, second, 40, 30)

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	captures := stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, _ func(int, int)) ([]BackgroundRemoveOutcome, error) {
		outcomes := make([]BackgroundRemoveOutcome, 0, len(items))
		for _, item := range items {
			outcomes = append(outcomes, BackgroundRemoveOutcome{OutputPath: item.OutputPath, Width: 40, Height: 30})
		}
		return outcomes, nil
	})
	tool := NewBackgroundRemoveTool()

	request := backgroundRemoveTestRequest("batch", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}

	subset := plan
	subset.Outputs = plan.Outputs[1:]

	items, execErr := tool.ExecutePlan(context.Background(), request, subset, nil)
	if execErr != nil {
		t.Fatalf("expected the subset to succeed, got %v", execErr)
	}
	if len(items) != 1 || !items[0].Success || items[0].InputPath != second || items[0].OutputPath != plan.Outputs[1].OutputPath {
		t.Fatalf("expected only the second planned output, got %+v", items)
	}
	if len(*captures) != 1 || len((*captures)[0].Items) != 1 {
		t.Fatalf("expected one engine call with only the subset, got %+v", *captures)
	}
	if got := (*captures)[0].Items[0].InputPath; got != second {
		t.Fatalf("expected the engine to run %q, got %q", second, got)
	}
}

func TestBackgroundRemoveToolExecutePlanPreservesOrderAndItemFailures(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
		filepath.Join(dir, "third.png"),
	}
	for _, input := range inputs {
		writePlanPNG(t, input, 40, 30)
	}

	stubBackgroundRemoveRuntime(t, backgroundRemoveTestRuntime(), nil)
	stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, _ func(int, int)) ([]BackgroundRemoveOutcome, error) {
		return []BackgroundRemoveOutcome{
			{OutputPath: items[0].OutputPath, Width: 40, Height: 30},
			{OutputPath: items[2].OutputPath, Width: 40, Height: 30},
		}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeBatchFailed,
			Message: "1 of 3 images could not be processed.",
			Failures: []BackgroundRemoveItemFailure{{
				Index:      1,
				InputPath:  inputs[1],
				OutputPath: strings.Replace(inputs[1], "second.png", "second_nobg.png", 1),
				Code:       BackgroundRemoveCodeProcessFailed,
				Message:    "Could not remove the background from second.png.",
			}},
		}
	})
	tool := NewBackgroundRemoveTool()

	request := backgroundRemoveTestRequest("batch", inputs, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), request, plan, nil)
	if execErr == nil {
		t.Fatal("expected the item failure")
	}
	if execErr.DetailCode != BackgroundRemoveCodeProcessFailed {
		t.Fatalf("expected %s, got %q", BackgroundRemoveCodeProcessFailed, execErr.DetailCode)
	}
	if len(items) != 3 {
		t.Fatalf("expected three result items, got %d", len(items))
	}
	for index, item := range items {
		if item.InputPath != inputs[index] {
			t.Fatalf("expected plan order at %d, got %+v", index, item)
		}
	}
	if !items[0].Success || !items[2].Success || items[1].Success {
		t.Fatalf("expected isolated per-item failures, got %+v", items)
	}
}

func TestBackgroundRemoveToolExecutePlanResolvesRuntimeOnce(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 40, 30)
	writePlanPNG(t, second, 40, 30)

	runtime := backgroundRemoveTestRuntime()
	resolutionCalls := 0
	previous := resolveBackgroundRemoveRuntime
	resolveBackgroundRemoveRuntime = func() (backgroundRemoveRuntime, *BackgroundRemoveError) {
		resolutionCalls++
		return runtime, nil
	}
	t.Cleanup(func() {
		resolveBackgroundRemoveRuntime = previous
	})

	stubBackgroundRemoveEngine(t, func(_ context.Context, _ RembgCommandRunner, _, _, _ string, items []BackgroundRemoveItem, _ BackgroundRemoveOptions, _ func(int, int)) ([]BackgroundRemoveOutcome, error) {
		outcomes := make([]BackgroundRemoveOutcome, 0, len(items))
		for _, item := range items {
			outcomes = append(outcomes, BackgroundRemoveOutcome{OutputPath: item.OutputPath, Width: 40, Height: 30})
		}
		return outcomes, nil
	})
	tool := NewBackgroundRemoveTool()

	request := backgroundRemoveTestRequest("batch", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}

	resolutionCalls = 0
	if _, execErr := tool.ExecutePlan(context.Background(), request, plan, nil); execErr != nil {
		t.Fatalf("expected the planned run to succeed, got %v", execErr)
	}
	if resolutionCalls != 1 {
		t.Fatalf("expected one runtime resolution per plan, got %d", resolutionCalls)
	}
}
