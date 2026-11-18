package image

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"fileforge-desktop/internal/toolchain"
)

// rembgStubCall records one RunRembg invocation.
type rembgStubCall struct {
	Dir        string
	PythonPath string
	Args       []string
	Env        []string
}

// rembgStubRunner answers the bridge without Python. onRun writes the fake
// workspace outputs and emits the JSON lines the real bridge would print.
type rembgStubRunner struct {
	mu    sync.Mutex
	calls []rembgStubCall
	onRun func(ctx context.Context, call rembgStubCall, emit func(string)) error
}

func (r *rembgStubRunner) RunRembg(ctx context.Context, dir, pythonPath string, args []string, env []string, onLine func(string)) error {
	call := rembgStubCall{
		Dir:        dir,
		PythonPath: pythonPath,
		Args:       append([]string{}, args...),
		Env:        append([]string{}, env...),
	}

	r.mu.Lock()
	r.calls = append(r.calls, call)
	onRun := r.onRun
	r.mu.Unlock()

	if onRun == nil {
		return nil
	}

	return onRun(ctx, call, onLine)
}

func (r *rembgStubRunner) snapshotCalls() []rembgStubCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	calls := make([]rembgStubCall, len(r.calls))
	for index, call := range r.calls {
		calls[index] = rembgStubCall{
			Dir:        call.Dir,
			PythonPath: call.PythonPath,
			Args:       append([]string{}, call.Args...),
			Env:        append([]string{}, call.Env...),
		}
	}

	return calls
}

func rembgStubArg(t *testing.T, args []string, name string) string {
	t.Helper()

	for index := 0; index+1 < len(args); index++ {
		if args[index] == name {
			return args[index+1]
		}
	}

	t.Fatalf("expected %s in argv %v", name, args)

	return ""
}

func rembgEnvValue(env []string, key string) (string, int) {
	prefix := key + "="
	value := ""
	count := 0
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			value = strings.TrimPrefix(entry, prefix)
			count++
		}
	}

	return value, count
}

func rembgJSONLine(t *testing.T, event map[string]any) string {
	t.Helper()

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encode stub bridge event: %v", err)
	}

	return string(payload)
}

func writeRembgFixturePNG(t *testing.T, path string, width, height int, transparent bool) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent && x == 0 && y == 0 {
				alpha = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: alpha})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png fixture %s: %v", path, err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png fixture %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png fixture %s: %v", path, err)
	}
}

func writeRembgOpaqueGrayPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetGray(x, y, color.Gray{Y: uint8((x + y) % 256)})
		}
	}

	writeRembgEncodedPNG(t, path, img)
}

// writeRembgAlpha254PNG writes a color-type-6 PNG whose pixels are translucent
// but never transparent, which exercises the alpha-pixel negative branch.
func writeRembgAlpha254PNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 40, B: 60, A: 254})
		}
	}

	writeRembgEncodedPNG(t, path, img)
}

// writeRembgOpaqueRGBPNG writes a fully opaque truecolor PNG. Go's encoder
// chooses color type 2 for opaque images, which is what the bridge produces
// after compositing on a background color.
func writeRembgOpaqueRGBPNG(t *testing.T, path string, width, height int, rgba color.RGBA) {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: rgba.R, G: rgba.G, B: rgba.B, A: 255})
		}
	}

	writeRembgEncodedPNG(t, path, img)
}

func writeRembgEncodedPNG(t *testing.T, path string, img image.Image) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create png dir: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png %s: %v", path, err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png %s: %v", path, err)
	}
}

// rembgStubInputs extracts the positional input paths from a bridge argv.
func rembgStubInputs(args []string) []string {
	inputs := make([]string, 0, len(args))
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--model", "--out-dir", "--background":
			index++
		default:
			inputs = append(inputs, args[index])
		}
	}

	return inputs
}

// writeRembgModelAt writes a sparse file with the plausibility size the engine
// requires. The checksum is the toolchain installer's responsibility.
func writeRembgModelAt(t *testing.T, home string) string {
	t.Helper()

	model := toolchain.DefaultRembgModel
	path := filepath.Join(home, "models", model, model+".onnx")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create model dir: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create model fixture: %v", err)
	}
	if err := file.Truncate(rembgMinimumModelBytes); err != nil {
		_ = file.Close()
		t.Fatalf("size model fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close model fixture: %v", err)
	}

	return path
}

func writeRembgPythonAt(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "bin", "python")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create python dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python fixture: %v", err)
	}

	return path
}

func TestRembgBridgeSourcePinsTheValidatedContract(t *testing.T) {
	source := string(rembgBridgeSource)

	for _, want := range []string{
		`new_session(args.model, providers=["CPUExecutionProvider"])`,
		"remove(data, session=session, decontaminate=True)",
		`"models", model, model + ".onnx"`,
		`os.environ.get("U2NET_HOME") or os.environ.get("REMBG_HOME")`,
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("bridge source is missing %q", want)
		}
	}
}

func TestRemoveBackgroundsRunsOneBridgeProcess(t *testing.T) {
	workingDir := t.TempDir()
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := t.TempDir()
	firstInput := filepath.Join(inputDir, "first.png")
	secondInput := filepath.Join(inputDir, "second.png")
	writeRembgFixturePNG(t, firstInput, 16, 12, false)
	writeRembgFixturePNG(t, secondInput, 16, 12, false)

	outputDir := t.TempDir()
	firstOutput := filepath.Join(outputDir, "first_nobg.png")
	secondOutput := filepath.Join(outputDir, "second_nobg.png")

	t.Setenv("REMBG_HOME", "inherited-rembg-home")
	t.Setenv("U2NET_HOME", "inherited-u2net-home")
	t.Setenv("PYTHONPATH", "/ambient/python-path")
	t.Setenv("PYTHONHOME", "/ambient/python-home")
	t.Setenv("PYTHONSTARTUP", "/ambient/python-startup")

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		bridge, readErr := os.ReadFile(call.Args[0])
		if readErr != nil {
			t.Fatalf("read the written bridge: %v", readErr)
		}
		if !strings.Contains(string(bridge), "decontaminate=True") {
			t.Fatal("expected the embedded bridge to be written into the workspace")
		}

		outDir := rembgStubArg(t, call.Args, "--out-dir")
		if filepath.Base(call.Args[0]) != rembgBridgeFileName {
			t.Fatalf("expected the embedded bridge in the workspace, got %q", call.Args[0])
		}
		if filepath.Dir(outDir) != call.Dir || filepath.Base(outDir) != "out-0" {
			t.Fatalf("expected <workspace>/out-0 as the first chunk output folder, got %q (workspace %q)", outDir, call.Dir)
		}
		writeRembgFixturePNG(t, filepath.Join(outDir, "out-0.png"), 16, 12, true)
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0,
			"output": filepath.Join(outDir, "out-0.png"), "width": 16, "height": 12,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 2}))

		writeRembgFixturePNG(t, filepath.Join(outDir, "out-1.png"), 16, 12, true)
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 1,
			"output": filepath.Join(outDir, "out-1.png"), "width": 16, "height": 12,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 1, "total": 2}))

		return nil
	}}

	var progress [][2]int
	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		workingDir,
		[]BackgroundRemoveItem{
			{InputPath: firstInput, OutputPath: firstOutput},
			{InputPath: secondInput, OutputPath: secondOutput},
		},
		BackgroundRemoveOptions{},
		func(current, total int) {
			progress = append(progress, [2]int{current, total})
		},
	)
	if err != nil {
		t.Fatalf("expected the batch to succeed, got %v", err)
	}

	calls := runner.snapshotCalls()
	if len(calls) != 1 {
		t.Fatalf("expected one bridge process for the whole batch, got %d", len(calls))
	}
	call := calls[0]
	if call.PythonPath != pythonPath {
		t.Fatalf("expected python %q, got %q", pythonPath, call.PythonPath)
	}
	if filepath.Base(call.Dir) == "" || !strings.HasPrefix(filepath.Base(call.Dir), "fileforge-rembg-") {
		t.Fatalf("expected the workspace as the process directory, got %q", call.Dir)
	}
	if len(call.Args) != 7 {
		t.Fatalf("expected bridge + 6 argv entries, got %v", call.Args)
	}
	if call.Args[1] != "--model" || call.Args[2] != toolchain.DefaultRembgModel {
		t.Fatalf("expected the pinned model, got %v", call.Args[:3])
	}
	if call.Args[3] != "--out-dir" {
		t.Fatalf("expected --out-dir, got %v", call.Args[3:5])
	}
	if !slices.Equal(call.Args[5:], []string{firstInput, secondInput}) {
		t.Fatalf("expected the input paths last, got %v", call.Args[5:])
	}

	rembgHome, rembgCount := rembgEnvValue(call.Env, "REMBG_HOME")
	if rembgCount != 1 || rembgHome != home {
		t.Fatalf("expected exactly one REMBG_HOME=%q, got %q (count %d)", home, rembgHome, rembgCount)
	}
	u2netHome, u2netCount := rembgEnvValue(call.Env, "U2NET_HOME")
	if u2netCount != 1 || u2netHome != home {
		t.Fatalf("expected exactly one U2NET_HOME=%q, got %q (count %d)", home, u2netHome, u2netCount)
	}
	if strings.Contains(strings.Join(call.Env, "\n"), "inherited-rembg-home") ||
		strings.Contains(strings.Join(call.Env, "\n"), "inherited-u2net-home") {
		t.Fatal("expected inherited RemBG homes to be replaced")
	}
	if pythonNoUserSite, count := rembgEnvValue(call.Env, "PYTHONNOUSERSITE"); count != 1 || pythonNoUserSite != "1" {
		t.Fatalf("expected PYTHONNOUSERSITE=1, got %q (count %d)", pythonNoUserSite, count)
	}
	for _, key := range []string{"PYTHONPATH", "PYTHONHOME", "PYTHONSTARTUP"} {
		if value, count := rembgEnvValue(call.Env, key); count != 0 {
			t.Fatalf("expected inherited %s to be removed, got %q (count %d)", key, value, count)
		}
	}
	if strings.Contains(strings.Join(call.Env, "\n"), "/ambient/") {
		t.Fatal("expected ambient Python import hooks to be absent from the child environment")
	}
	if threads, count := rembgEnvValue(call.Env, "OMP_NUM_THREADS"); count != 1 || threads == "" {
		t.Fatalf("expected one bounded OMP_NUM_THREADS, got %q (count %d)", threads, count)
	}
	pathValue, pathCount := rembgEnvValue(call.Env, "PATH")
	if pathCount != 1 {
		t.Fatalf("expected exactly one PATH entry, got %d", pathCount)
	}
	if first := strings.Split(pathValue, string(os.PathListSeparator))[0]; first != filepath.Dir(pythonPath) {
		t.Fatalf("expected the venv bin folder first in PATH, got %q", pathValue)
	}

	if len(outcomes) != 2 {
		t.Fatalf("expected two outcomes, got %d", len(outcomes))
	}
	if outcomes[0].OutputPath != firstOutput || outcomes[0].Width != 16 || outcomes[0].Height != 12 {
		t.Fatalf("unexpected first outcome: %+v", outcomes[0])
	}
	if outcomes[1].OutputPath != secondOutput || outcomes[1].Width != 16 || outcomes[1].Height != 12 {
		t.Fatalf("unexpected second outcome: %+v", outcomes[1])
	}
	for _, path := range []string{firstOutput, secondOutput} {
		colorType, colorErr := pngColorTypeOf(path)
		if colorErr != nil || colorType != pngColorTypeRGBA {
			t.Fatalf("expected an RGBA PNG at %s, color type %d err %v", path, colorType, colorErr)
		}
	}
	if !slices.Equal(progress, [][2]int{{1, 2}, {2, 2}}) {
		t.Fatalf("expected per-item progress, got %v", progress)
	}

	entries, readErr := os.ReadDir(workingDir)
	if readErr != nil {
		t.Fatalf("read working dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the temporary workspace to be removed, found %v", entries)
	}
}

func TestRemoveBackgroundsPassesBackgroundColor(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 10, 8, false)
	outputPath := filepath.Join(t.TempDir(), "photo_nobg.png")

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		writeRembgOpaqueRGBPNG(t, filepath.Join(outDir, "out-0.png"), 10, 8, color.RGBA{R: 0xa1, G: 0xb2, B: 0xc3, A: 0xff})
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0,
			"output": filepath.Join(outDir, "out-0.png"), "width": 10, "height": 8,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 1}))

		return nil
	}}

	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: outputPath}},
		BackgroundRemoveOptions{BackgroundColor: "#A1B2C3"},
		nil,
	)
	if err != nil {
		t.Fatalf("expected the run to succeed, got %v", err)
	}

	call := runner.snapshotCalls()[0]
	if len(call.Args) != 8 {
		t.Fatalf("expected --background in argv, got %v", call.Args)
	}
	if call.Args[5] != "--background" || call.Args[6] != "#a1b2c3" {
		t.Fatalf("expected the normalized background color, got %v", call.Args[5:])
	}
	if outcome := outcomes[0]; outcome.Width != 10 || outcome.Height != 8 {
		t.Fatalf("unexpected outcome: %+v", outcome)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected the published output: %v", statErr)
	}

	colorType, colorErr := pngColorTypeOf(outputPath)
	if colorErr != nil || colorType != pngColorTypeRGB {
		t.Fatalf("expected an RGB (color type 2) composite, got type %d err %v", colorType, colorErr)
	}

	file, openErr := os.Open(outputPath)
	if openErr != nil {
		t.Fatalf("open the published composite: %v", openErr)
	}
	defer file.Close()

	decoded, decodeErr := png.Decode(file)
	if decodeErr != nil {
		t.Fatalf("decode the published composite: %v", decodeErr)
	}
	red, green, blue, alpha := decoded.At(0, 0).RGBA()
	if red>>8 != 0xa1 || green>>8 != 0xb2 || blue>>8 != 0xc3 || alpha != 0xffff {
		t.Fatalf("expected exact composite color #a1b2c3, got #%02x%02x%02x a=%04x", red>>8, green>>8, blue>>8, alpha)
	}
}

func TestRemoveBackgroundsFailsBeforeRunnerWithoutRuntime(t *testing.T) {
	tests := []struct {
		name      string
		prepare   func(t *testing.T, home string) (string, string)
		wantCode  string
		wantCalls int
	}{
		{
			name: "missing python",
			prepare: func(t *testing.T, home string) (string, string) {
				return filepath.Join(t.TempDir(), "missing-python"), writeRembgModelAt(t, home)
			},
			wantCode: BackgroundRemoveCodeRuntimeUnavailable,
		},
		{
			name: "missing model",
			prepare: func(t *testing.T, home string) (string, string) {
				modelPath := filepath.Join(home, "models", toolchain.DefaultRembgModel, toolchain.DefaultRembgModel+".onnx")
				return writeRembgPythonAt(t), modelPath
			},
			wantCode: BackgroundRemoveCodeModelUnavailable,
		},
		{
			name: "short model",
			prepare: func(t *testing.T, home string) (string, string) {
				modelPath := filepath.Join(home, "models", toolchain.DefaultRembgModel, toolchain.DefaultRembgModel+".onnx")
				if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
					t.Fatalf("create model dir: %v", err)
				}
				if err := os.WriteFile(modelPath, []byte("short"), 0o644); err != nil {
					t.Fatalf("write short model: %v", err)
				}
				return writeRembgPythonAt(t), modelPath
			},
			wantCode: BackgroundRemoveCodeModelUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			pythonPath, modelPath := test.prepare(t, home)

			inputPath := filepath.Join(t.TempDir(), "photo.png")
			writeRembgFixturePNG(t, inputPath, 4, 4, false)

			runner := &rembgStubRunner{onRun: func(context.Context, rembgStubCall, func(string)) error {
				t.Fatal("the runner must not be called without a complete pack")
				return nil
			}}

			_, err := RemoveBackgrounds(
				context.Background(),
				runner,
				pythonPath,
				modelPath,
				t.TempDir(),
				[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: filepath.Join(t.TempDir(), "photo_nobg.png")}},
				BackgroundRemoveOptions{},
				nil,
			)
			if err == nil {
				t.Fatal("expected the run to fail")
			}

			var rembgErr *BackgroundRemoveError
			if !errors.As(err, &rembgErr) {
				t.Fatalf("expected a typed error, got %T", err)
			}
			if rembgErr.Code != test.wantCode {
				t.Fatalf("expected code %q, got %q", test.wantCode, rembgErr.Code)
			}
			if calls := len(runner.snapshotCalls()); calls != test.wantCalls {
				t.Fatalf("expected %d runner calls, got %d", test.wantCalls, calls)
			}
		})
	}
}

func TestRemoveBackgroundsRefusesExistingDestination(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 4, 4, false)

	outputPath := filepath.Join(t.TempDir(), "photo_nobg.png")
	if err := os.WriteFile(outputPath, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write existing destination: %v", err)
	}

	runner := &rembgStubRunner{onRun: func(context.Context, rembgStubCall, func(string)) error {
		t.Fatal("an existing destination must fail before Python starts")
		return nil
	}}

	_, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: outputPath}},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected the existing destination to fail")
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) || rembgErr.Code != BackgroundRemoveCodeOutputExists {
		t.Fatalf("expected %s, got %v", BackgroundRemoveCodeOutputExists, err)
	}
	content, readErr := os.ReadFile(outputPath)
	if readErr != nil || string(content) != "keep me" {
		t.Fatalf("expected the existing file to be untouched, got %q (%v)", content, readErr)
	}
	if calls := len(runner.snapshotCalls()); calls != 0 {
		t.Fatalf("expected no runner calls, got %d", calls)
	}
}

func TestRemoveBackgroundsValidatesBridgeOutput(t *testing.T) {
	tests := []struct {
		name        string
		background  string
		wantMessage string
		write       func(t *testing.T, outDir string) (string, int, int)
	}{
		{
			name: "dimension mismatch",
			write: func(t *testing.T, outDir string) (string, int, int) {
				path := filepath.Join(outDir, "out-0.png")
				writeRembgFixturePNG(t, path, 8, 6, true)
				return path, 9, 6
			},
		},
		{
			name: "not RGBA",
			write: func(t *testing.T, outDir string) (string, int, int) {
				path := filepath.Join(outDir, "out-0.png")
				writeRembgOpaqueGrayPNG(t, path, 8, 6)
				return path, 8, 6
			},
		},
		{
			name:        "alpha 254 only",
			wantMessage: errRembgNoTransparentPixels.Error(),
			write: func(t *testing.T, outDir string) (string, int, int) {
				path := filepath.Join(outDir, "out-0.png")
				writeRembgAlpha254PNG(t, path, 8, 6)
				return path, 8, 6
			},
		},
		{
			name:       "background output is not RGB",
			background: "#336699",
			write: func(t *testing.T, outDir string) (string, int, int) {
				path := filepath.Join(outDir, "out-0.png")
				writeRembgOpaqueGrayPNG(t, path, 8, 6)
				return path, 8, 6
			},
		},
		{
			name: "missing file",
			write: func(t *testing.T, outDir string) (string, int, int) {
				return filepath.Join(outDir, "missing.png"), 8, 6
			},
		},
		{
			name: "outside the workspace",
			write: func(t *testing.T, outDir string) (string, int, int) {
				path := filepath.Join(filepath.Dir(outDir), "outside.png")
				writeRembgFixturePNG(t, path, 8, 6, true)
				return path, 8, 6
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			modelPath := writeRembgModelAt(t, home)
			pythonPath := writeRembgPythonAt(t)

			inputPath := filepath.Join(t.TempDir(), "photo.png")
			writeRembgFixturePNG(t, inputPath, 8, 6, false)
			outputPath := filepath.Join(t.TempDir(), "photo_nobg.png")

			runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
				outDir := rembgStubArg(t, call.Args, "--out-dir")
				output, width, height := test.write(t, outDir)
				emit(rembgJSONLine(t, map[string]any{
					"event": "done", "index": 0,
					"output": output, "width": width, "height": height,
				}))
				emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 1}))

				return nil
			}}

			outcomes, err := RemoveBackgrounds(
				context.Background(),
				runner,
				pythonPath,
				modelPath,
				t.TempDir(),
				[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: outputPath}},
				BackgroundRemoveOptions{BackgroundColor: test.background},
				nil,
			)
			if err == nil {
				t.Fatal("expected the invalid output to fail")
			}
			if len(outcomes) != 0 {
				t.Fatalf("expected no outcomes, got %v", outcomes)
			}

			var rembgErr *BackgroundRemoveError
			if !errors.As(err, &rembgErr) || len(rembgErr.Failures) != 1 {
				t.Fatalf("expected one typed item failure, got %v", err)
			}
			if rembgErr.Failures[0].Code != BackgroundRemoveCodeVerifyFailed {
				t.Fatalf("expected %s, got %+v", BackgroundRemoveCodeVerifyFailed, rembgErr.Failures[0])
			}
			if test.wantMessage != "" && rembgErr.Failures[0].Message != test.wantMessage {
				t.Fatalf("expected message %q, got %q", test.wantMessage, rembgErr.Failures[0].Message)
			}
			if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("an invalid bridge output must never be published")
			}
		})
	}
}

func TestRemoveBackgroundsRefusesDestinationCreatedDuringRun(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 6, 4, false)
	outputPath := filepath.Join(t.TempDir(), "photo_nobg.png")

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		writeRembgFixturePNG(t, filepath.Join(outDir, "out-0.png"), 6, 4, true)

		// The destination appears after preflight: publication must refuse it
		// instead of overwriting a file this run does not own.
		if err := os.WriteFile(outputPath, []byte("late file"), 0o644); err != nil {
			t.Fatalf("write late destination: %v", err)
		}

		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0,
			"output": filepath.Join(outDir, "out-0.png"), "width": 6, "height": 4,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 1}))

		return nil
	}}

	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: outputPath}},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected the late destination to be refused")
	}
	if len(outcomes) != 0 {
		t.Fatalf("expected no outcomes, got %+v", outcomes)
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) || len(rembgErr.Failures) != 1 {
		t.Fatalf("expected one typed failure, got %v", err)
	}
	if rembgErr.Failures[0].Code != BackgroundRemoveCodeOutputExists {
		t.Fatalf("expected %s, got %+v", BackgroundRemoveCodeOutputExists, rembgErr.Failures[0])
	}
	content, readErr := os.ReadFile(outputPath)
	if readErr != nil || string(content) != "late file" {
		t.Fatalf("expected the late file untouched, got %q (%v)", content, readErr)
	}

	entries, readErr := os.ReadDir(filepath.Dir(outputPath))
	if readErr != nil {
		t.Fatalf("read the output dir: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-rembg-") {
			t.Fatalf("expected no temporary or placeholder file, found %q", entry.Name())
		}
	}
}

func TestRemoveBackgroundsAggregatesPerItemErrors(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := t.TempDir()
	firstInput := filepath.Join(inputDir, "first.png")
	secondInput := filepath.Join(inputDir, "second.png")
	writeRembgFixturePNG(t, firstInput, 6, 4, false)
	writeRembgFixturePNG(t, secondInput, 6, 4, false)

	outputDir := t.TempDir()
	firstOutput := filepath.Join(outputDir, "first_nobg.png")
	secondOutput := filepath.Join(outputDir, "second_nobg.png")

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		writeRembgFixturePNG(t, filepath.Join(outDir, "out-0.png"), 6, 4, true)
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0,
			"output": filepath.Join(outDir, "out-0.png"), "width": 6, "height": 4,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 2}))
		emit(rembgJSONLine(t, map[string]any{"event": "error", "index": 1, "message": "onnxruntime exploded"}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 1, "total": 2}))

		return nil
	}}

	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{
			{InputPath: firstInput, OutputPath: firstOutput},
			{InputPath: secondInput, OutputPath: secondOutput},
		},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected one item failure to be reported")
	}

	if len(outcomes) != 0 {
		t.Fatalf("expected publish-or-nothing to clear outcomes, got %+v", outcomes)
	}
	for _, path := range []string{firstOutput, secondOutput} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected %s to be absent after rollback", path)
		}
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) {
		t.Fatalf("expected a typed error, got %T", err)
	}
	if rembgErr.Code != BackgroundRemoveCodeBatchFailed {
		t.Fatalf("expected %s, got %q", BackgroundRemoveCodeBatchFailed, rembgErr.Code)
	}
	if len(rembgErr.Failures) != 2 {
		t.Fatalf("expected the bridge failure and the rollback result, got %+v", rembgErr.Failures)
	}

	byIndex := make(map[int]BackgroundRemoveItemFailure, len(rembgErr.Failures))
	for _, failure := range rembgErr.Failures {
		byIndex[failure.Index] = failure
	}
	if failure := byIndex[0]; failure.Code != BackgroundRemoveCodeBatchFailed || !strings.Contains(failure.Message, "Not kept") {
		t.Fatalf("expected the first item to report its removed output, got %+v", failure)
	}
	if failure := byIndex[1]; failure.Code != BackgroundRemoveCodeProcessFailed ||
		failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "onnxruntime exploded") {
		t.Fatalf("expected the bridge failure with its raw cause, got %+v", failure)
	}
}

func TestRemoveBackgroundsCancellationRollsBackPublishedOutputs(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := t.TempDir()
	firstInput := filepath.Join(inputDir, "first.png")
	secondInput := filepath.Join(inputDir, "second.png")
	writeRembgFixturePNG(t, firstInput, 6, 4, false)
	writeRembgFixturePNG(t, secondInput, 6, 4, false)

	outputDir := t.TempDir()
	firstOutput := filepath.Join(outputDir, "first_nobg.png")
	secondOutput := filepath.Join(outputDir, "second_nobg.png")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workingDir := t.TempDir()
	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		writeRembgFixturePNG(t, filepath.Join(outDir, "out-0.png"), 6, 4, true)
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0,
			"output": filepath.Join(outDir, "out-0.png"), "width": 6, "height": 4,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 2}))
		cancel()

		return ctx.Err()
	}}

	outcomes, err := RemoveBackgrounds(
		ctx,
		runner,
		pythonPath,
		modelPath,
		workingDir,
		[]BackgroundRemoveItem{
			{InputPath: firstInput, OutputPath: firstOutput},
			{InputPath: secondInput, OutputPath: secondOutput},
		},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if len(outcomes) != 0 {
		t.Fatalf("expected publish-or-nothing, got %+v", outcomes)
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) || rembgErr.Code != BackgroundRemoveCodeCancelled {
		t.Fatalf("expected %s, got %v", BackgroundRemoveCodeCancelled, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation cause to unwrap, got %v", err)
	}
	if _, statErr := os.Stat(firstOutput); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("cancellation must remove an output published by this call")
	}
	entries, readErr := os.ReadDir(workingDir)
	if readErr != nil {
		t.Fatalf("read working dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the workspace removed after cancellation, found %v", entries)
	}
}

func TestRemoveBackgroundsProcessFailureCleansWorkspace(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := t.TempDir()
	firstInput := filepath.Join(inputDir, "first.png")
	secondInput := filepath.Join(inputDir, "second.png")
	writeRembgFixturePNG(t, firstInput, 6, 4, false)
	writeRembgFixturePNG(t, secondInput, 6, 4, false)

	outputDir := t.TempDir()
	workingDir := t.TempDir()

	runner := &rembgStubRunner{onRun: func(context.Context, rembgStubCall, func(string)) error {
		return errors.New("interpreter crashed")
	}}

	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		workingDir,
		[]BackgroundRemoveItem{
			{InputPath: firstInput, OutputPath: filepath.Join(outputDir, "first_nobg.png")},
			{InputPath: secondInput, OutputPath: filepath.Join(outputDir, "second_nobg.png")},
		},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected a process failure")
	}
	if len(outcomes) != 0 {
		t.Fatalf("expected no published outcomes, got %+v", outcomes)
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) {
		t.Fatalf("expected a typed error, got %T", err)
	}
	if len(rembgErr.Failures) != 2 {
		t.Fatalf("expected both items reported, got %+v", rembgErr.Failures)
	}
	for _, failure := range rembgErr.Failures {
		if failure.Code != BackgroundRemoveCodeProcessFailed {
			t.Fatalf("expected %s, got %+v", BackgroundRemoveCodeProcessFailed, failure)
		}
		if failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "interpreter crashed") {
			t.Fatalf("expected the process cause, got %v", failure.Cause)
		}
	}

	entries, readErr := os.ReadDir(workingDir)
	if readErr != nil {
		t.Fatalf("read working dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the workspace removed after a process failure, found %v", entries)
	}
}

// rembgShortTempDir keeps test paths short enough that 100 of them fit inside
// one chunk budget on every platform.
func rembgShortTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "t9-")
	if err != nil {
		t.Fatalf("create short temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return dir
}

func TestRemoveBackgroundsChunksLargeBatches(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := rembgShortTempDir(t)
	outputDir := rembgShortTempDir(t)

	const total = 250
	items := make([]BackgroundRemoveItem, 0, total)
	for index := 0; index < total; index++ {
		inputPath := filepath.Join(inputDir, fmt.Sprintf("f%03d.png", index))
		writeRembgFixturePNG(t, inputPath, 4, 3, false)
		items = append(items, BackgroundRemoveItem{
			InputPath:  inputPath,
			OutputPath: filepath.Join(outputDir, fmt.Sprintf("o%03d.png", index)),
		})
	}

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		inputs := rembgStubInputs(call.Args)
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		for local := range inputs {
			outputPath := filepath.Join(outDir, fmt.Sprintf("out-%d.png", local))
			writeRembgFixturePNG(t, outputPath, 4, 3, true)
			emit(rembgJSONLine(t, map[string]any{
				"event": "done", "index": local, "output": outputPath, "width": 4, "height": 3,
			}))
			emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": local, "total": len(inputs)}))
		}

		return nil
	}}

	var progress [][2]int
	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		items,
		BackgroundRemoveOptions{},
		func(current, total int) {
			progress = append(progress, [2]int{current, total})
		},
	)
	if err != nil {
		t.Fatalf("expected the chunked batch to succeed, got %v", err)
	}

	calls := runner.snapshotCalls()
	if len(calls) != 3 {
		t.Fatalf("expected exactly 3 bridge processes for 250 items, got %d", len(calls))
	}

	sizes := make([]int, 0, len(calls))
	for _, call := range calls {
		sizes = append(sizes, len(rembgStubInputs(call.Args)))
	}
	if !slices.Equal(sizes, []int{100, 100, 50}) {
		t.Fatalf("expected chunk sizes 100/100/50, got %v", sizes)
	}
	if len(outcomes) != total {
		t.Fatalf("expected %d outcomes, got %d", total, len(outcomes))
	}
	for _, item := range items {
		if _, statErr := os.Stat(item.OutputPath); statErr != nil {
			t.Fatalf("expected %s to be published: %v", item.OutputPath, statErr)
		}
	}
	if len(progress) != total {
		t.Fatalf("expected %d progress updates, got %d", total, len(progress))
	}
	if progress[len(progress)-1] != [2]int{total, total} {
		t.Fatalf("expected final progress %d/%d, got %v", total, total, progress[len(progress)-1])
	}
	for index := 1; index < len(progress); index++ {
		if progress[index][0] <= progress[index-1][0] {
			t.Fatalf("progress must be monotonic, got %v", progress)
		}
	}
}

func TestRembgItemChunksCharacterBudget(t *testing.T) {
	items := make([]BackgroundRemoveItem, 0, 60)
	for index := 0; index < 60; index++ {
		items = append(items, BackgroundRemoveItem{InputPath: strings.Repeat("x", 201)})
	}

	chunks := rembgItemChunks(items)
	if len(chunks) != 2 {
		t.Fatalf("expected the character budget to make two chunks, got %+v", chunks)
	}
	if size := chunks[0].end - chunks[0].start; size != 49 {
		t.Fatalf("expected 49 paths in the first chunk (49*201 <= 10000), got %d", size)
	}
	if size := chunks[1].end - chunks[1].start; size != 11 {
		t.Fatalf("expected the remaining 11 paths in the second chunk, got %d", size)
	}
}

func TestRemoveBackgroundsLaterChunkFailureRollsBackEarlierChunk(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := rembgShortTempDir(t)
	outputDir := rembgShortTempDir(t)

	const total = 150
	items := make([]BackgroundRemoveItem, 0, total)
	for index := 0; index < total; index++ {
		inputPath := filepath.Join(inputDir, fmt.Sprintf("f%03d.png", index))
		writeRembgFixturePNG(t, inputPath, 4, 3, false)
		items = append(items, BackgroundRemoveItem{
			InputPath:  inputPath,
			OutputPath: filepath.Join(outputDir, fmt.Sprintf("o%03d.png", index)),
		})
	}

	callIndex := 0
	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		inputs := rembgStubInputs(call.Args)
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		currentCall := callIndex
		callIndex++

		if currentCall == 0 {
			for local := range inputs {
				outputPath := filepath.Join(outDir, fmt.Sprintf("out-%d.png", local))
				writeRembgFixturePNG(t, outputPath, 4, 3, true)
				emit(rembgJSONLine(t, map[string]any{
					"event": "done", "index": local, "output": outputPath, "width": 4, "height": 3,
				}))
				emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": local, "total": len(inputs)}))
			}

			return nil
		}

		emit(rembgJSONLine(t, map[string]any{"event": "error", "index": 0, "message": "later chunk exploded"}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": len(inputs)}))

		return nil
	}}

	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		items,
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected the later chunk failure to fail the job")
	}
	if len(outcomes) != 0 {
		t.Fatalf("expected publish-or-nothing across chunks, got %+v", outcomes)
	}
	if calls := len(runner.snapshotCalls()); calls != 2 {
		t.Fatalf("expected two chunks, got %d", calls)
	}
	for _, item := range items {
		if _, statErr := os.Stat(item.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected %s to be removed by the job-wide rollback", item.OutputPath)
		}
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) {
		t.Fatalf("expected a typed error, got %T", err)
	}
	byIndex := make(map[int]BackgroundRemoveItemFailure, len(rembgErr.Failures))
	for _, failure := range rembgErr.Failures {
		byIndex[failure.Index] = failure
	}
	if failure := byIndex[100]; failure.Code != BackgroundRemoveCodeProcessFailed ||
		failure.Cause == nil || !strings.Contains(failure.Cause.Error(), "later chunk exploded") {
		t.Fatalf("expected the later chunk bridge failure, got %+v", failure)
	}
	if failure := byIndex[0]; failure.Code != BackgroundRemoveCodeBatchFailed || !strings.Contains(failure.Message, "Not kept") {
		t.Fatalf("expected the earlier chunk output to report its rollback, got %+v", failure)
	}
}

func TestRemoveBackgroundsIgnoresMalformedAndOutOfRangeEvents(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := t.TempDir()
	firstInput := filepath.Join(inputDir, "first.png")
	secondInput := filepath.Join(inputDir, "second.png")
	writeRembgFixturePNG(t, firstInput, 4, 3, false)
	writeRembgFixturePNG(t, secondInput, 4, 3, false)

	outputDir := t.TempDir()
	firstOutput := filepath.Join(outputDir, "first_nobg.png")
	secondOutput := filepath.Join(outputDir, "second_nobg.png")

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		firstWorkspaceOutput := filepath.Join(outDir, "out-0.png")
		secondWorkspaceOutput := filepath.Join(outDir, "out-1.png")
		writeRembgFixturePNG(t, firstWorkspaceOutput, 4, 3, true)
		writeRembgFixturePNG(t, secondWorkspaceOutput, 4, 3, true)

		emit("not json")
		emit("{oops}")
		emit("[]")
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": -1, "total": 2}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 99, "total": 2}))
		emit(rembgJSONLine(t, map[string]any{"event": "done", "index": -1, "output": "ignored.png", "width": 4, "height": 3}))
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0, "output": firstWorkspaceOutput, "width": 4, "height": 3,
		}))
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0, "output": filepath.Join(outDir, "duplicate.png"), "width": 4, "height": 3,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "error", "index": 9, "message": "ignored"}))
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 1, "output": secondWorkspaceOutput, "width": 4, "height": 3,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 2}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 1, "total": 2}))

		return nil
	}}

	var progress [][2]int
	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{
			{InputPath: firstInput, OutputPath: firstOutput},
			{InputPath: secondInput, OutputPath: secondOutput},
		},
		BackgroundRemoveOptions{},
		func(current, total int) {
			progress = append(progress, [2]int{current, total})
		},
	)
	if err != nil {
		t.Fatalf("expected malformed and out-of-range lines to be ignored, got %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected two outcomes, got %+v", outcomes)
	}
	for _, path := range []string{firstOutput, secondOutput} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to be published: %v", path, statErr)
		}
	}
	if !slices.Equal(progress, [][2]int{{1, 2}, {2, 2}}) {
		t.Fatalf("expected valid progress only, got %v", progress)
	}
}

func TestRembgStderrTailBounds(t *testing.T) {
	tail := &rembgStderrTail{maxLines: 3, maxBytes: 40}
	for index := 0; index < 10; index++ {
		tail.add(fmt.Sprintf("line-%d", index))
	}

	got := tail.String()
	if !strings.Contains(got, "line-9") {
		t.Fatalf("expected the newest stderr line, got %q", got)
	}
	if strings.Contains(got, "line-0") {
		t.Fatalf("expected old stderr lines to be dropped, got %q", got)
	}
	if len(got) > 40 {
		t.Fatalf("expected at most 40 bytes, got %d: %q", len(got), got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected valid UTF-8, got %q", got)
	}

	byteTail := &rembgStderrTail{maxLines: 5, maxBytes: 7}
	byteTail.add("aaaaaaé")
	got = byteTail.String()
	if got != "aaaaaé" {
		t.Fatalf("expected a rune-boundary trim to %q, got %q", "aaaaaé", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("expected a valid UTF-8 tail, got %q", got)
	}
}

func TestRemoveBackgroundsReportsProcessFailureStderr(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 4, 3, false)

	runner := &rembgStubRunner{onRun: func(context.Context, rembgStubCall, func(string)) error {
		return &RembgCommandError{Err: errors.New("exit status 1"), Stderr: "traceback line"}
	}}

	_, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: filepath.Join(t.TempDir(), "photo_nobg.png")}},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected the process failure")
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) {
		t.Fatalf("expected a typed error, got %T", err)
	}
	if rembgErr.Code != BackgroundRemoveCodeProcessFailed {
		t.Fatalf("expected %s, got %q", BackgroundRemoveCodeProcessFailed, rembgErr.Code)
	}
	if len(rembgErr.Failures) != 1 || rembgErr.Failures[0].Cause == nil ||
		!strings.Contains(rembgErr.Failures[0].Cause.Error(), "traceback line") {
		t.Fatalf("expected the stderr tail in the failure cause, got %+v", rembgErr.Failures)
	}
	if stderr, _ := rembgErr.Details["stderr"].(string); stderr != "traceback line" {
		t.Fatalf("expected stderr details, got %+v", rembgErr.Details)
	}
}

func TestRemoveBackgroundsExitAfterValidatedOutputsSucceeds(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputDir := t.TempDir()
	firstInput := filepath.Join(inputDir, "first.png")
	secondInput := filepath.Join(inputDir, "second.png")
	writeRembgFixturePNG(t, firstInput, 4, 3, false)
	writeRembgFixturePNG(t, secondInput, 4, 3, false)

	outputDir := t.TempDir()
	firstOutput := filepath.Join(outputDir, "first_nobg.png")
	secondOutput := filepath.Join(outputDir, "second_nobg.png")

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		for local := 0; local < 2; local++ {
			outputPath := filepath.Join(outDir, fmt.Sprintf("out-%d.png", local))
			writeRembgFixturePNG(t, outputPath, 4, 3, true)
			emit(rembgJSONLine(t, map[string]any{
				"event": "done", "index": local, "output": outputPath, "width": 4, "height": 3,
			}))
			emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": local, "total": 2}))
		}

		return &RembgCommandError{Err: errors.New("exit status 5"), Stderr: "late interpreter warning"}
	}}

	outcomes, err := RemoveBackgrounds(
		context.Background(),
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{
			{InputPath: firstInput, OutputPath: firstOutput},
			{InputPath: secondInput, OutputPath: secondOutput},
		},
		BackgroundRemoveOptions{},
		nil,
	)
	if err != nil {
		t.Fatalf("validated outputs must win over a trailing non-zero exit, got %v", err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected two outcomes, got %+v", outcomes)
	}
	for _, path := range []string{firstOutput, secondOutput} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to remain published: %v", path, statErr)
		}
	}
}

func TestRemoveBackgroundsLateCancelAfterSuccessKeepsOutputs(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 4, 3, false)
	outputPath := filepath.Join(t.TempDir(), "photo_nobg.png")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		output := filepath.Join(outDir, "out-0.png")
		writeRembgFixturePNG(t, output, 4, 3, true)
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0, "output": output, "width": 4, "height": 3,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 1}))
		cancel()

		return nil
	}}

	outcomes, err := RemoveBackgrounds(
		ctx,
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: outputPath}},
		BackgroundRemoveOptions{},
		nil,
	)
	if err != nil {
		t.Fatalf("a late ambient cancel must not fail a successful run, got %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %+v", outcomes)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected the validated output to remain, got %v", statErr)
	}
}

func TestRemoveBackgroundsCancellationReportsCleanupErrors(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 4, 3, false)
	outputPath := filepath.Join(t.TempDir(), "photo_nobg.png")

	previousRemove := rembgRemoveFile
	rembgRemoveFile = func(string) error { return errors.New("remove denied") }
	t.Cleanup(func() {
		rembgRemoveFile = previousRemove
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &rembgStubRunner{onRun: func(_ context.Context, call rembgStubCall, emit func(string)) error {
		outDir := rembgStubArg(t, call.Args, "--out-dir")
		output := filepath.Join(outDir, "out-0.png")
		writeRembgFixturePNG(t, output, 4, 3, true)
		emit(rembgJSONLine(t, map[string]any{
			"event": "done", "index": 0, "output": output, "width": 4, "height": 3,
		}))
		emit(rembgJSONLine(t, map[string]any{"event": "progress", "index": 0, "total": 1}))
		cancel()

		return ctx.Err()
	}}

	outcomes, err := RemoveBackgrounds(
		ctx,
		runner,
		pythonPath,
		modelPath,
		t.TempDir(),
		[]BackgroundRemoveItem{{InputPath: inputPath, OutputPath: outputPath}},
		BackgroundRemoveOptions{},
		nil,
	)
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if len(outcomes) != 0 {
		t.Fatalf("expected no outcomes, got %+v", outcomes)
	}

	var rembgErr *BackgroundRemoveError
	if !errors.As(err, &rembgErr) || rembgErr.Code != BackgroundRemoveCodeCancelled {
		t.Fatalf("expected %s, got %v", BackgroundRemoveCodeCancelled, err)
	}
	cleanupErrors, _ := rembgErr.Details["cleanupErrors"].([]string)
	if len(cleanupErrors) == 0 {
		t.Fatalf("expected rollback failures in details, got %+v", rembgErr.Details)
	}
}

func TestPublishBackgroundRemovePNGRemovesPlaceholderOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.png")
	writeRembgFixturePNG(t, source, 4, 3, true)
	destination := filepath.Join(dir, "dest.png")

	previousRename := rembgRenameFile
	rembgRenameFile = func(string, string) error { return errors.New("rename denied") }
	t.Cleanup(func() {
		rembgRenameFile = previousRename
	})

	publishErr := publishBackgroundRemovePNG(source, destination)
	if publishErr == nil {
		t.Fatal("expected the rename to fail")
	}
	if publishErr.Code != BackgroundRemoveCodePublishFailed {
		t.Fatalf("expected %s, got %q", BackgroundRemoveCodePublishFailed, publishErr.Code)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the placeholder to be removed, got %v", statErr)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read the destination dir: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-rembg-") {
			t.Fatalf("expected no temporary or placeholder file, found %q", entry.Name())
		}
	}
}

func TestRembgPNGHasTransparentPixel(t *testing.T) {
	transparentNRGBA := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			transparentNRGBA.SetNRGBA(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 254})
		}
	}
	transparentNRGBA.SetNRGBA(1, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 0})

	opaqueNRGBA := image.NewNRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			opaqueNRGBA.SetNRGBA(x, y, color.NRGBA{R: 10, G: 20, B: 30, A: 254})
		}
	}

	transparentRGBA := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			transparentRGBA.SetRGBA(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	transparentRGBA.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 0})

	transparentNRGBA64 := image.NewNRGBA64(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			transparentNRGBA64.SetNRGBA64(x, y, color.NRGBA64{R: 1, G: 2, B: 3, A: 0xffff})
		}
	}
	transparentNRGBA64.SetNRGBA64(0, 0, color.NRGBA64{R: 1, G: 2, B: 3, A: 0})

	tests := []struct {
		name string
		img  image.Image
		want bool
	}{
		{name: "NRGBA with a zero alpha pixel", img: transparentNRGBA, want: true},
		{name: "NRGBA with alpha 254 only", img: opaqueNRGBA, want: false},
		{name: "RGBA with a zero alpha pixel", img: transparentRGBA, want: true},
		{name: "NRGBA64 with a zero alpha pixel", img: transparentNRGBA64, want: true},
		{name: "gray", img: image.NewGray(image.Rect(0, 0, 2, 2)), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := rembgPNGHasTransparentPixel(test.img); got != test.want {
				t.Fatalf("expected %v, got %v", test.want, got)
			}
		})
	}
}

func TestRemoveBackgroundsRejectsInvalidRequests(t *testing.T) {
	home := t.TempDir()
	modelPath := writeRembgModelAt(t, home)
	pythonPath := writeRembgPythonAt(t)

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writeRembgFixturePNG(t, inputPath, 4, 4, false)
	heicPath := filepath.Join(t.TempDir(), "photo.heic")
	if err := os.WriteFile(heicPath, []byte("fake"), 0o644); err != nil {
		t.Fatalf("write heic fixture: %v", err)
	}

	tests := []struct {
		name       string
		items      []BackgroundRemoveItem
		background string
		wantCode   string
	}{
		{
			name:     "no items",
			items:    nil,
			wantCode: BackgroundRemoveCodeInvalidInput,
		},
		{
			name: "unsupported input",
			items: []BackgroundRemoveItem{{
				InputPath:  heicPath,
				OutputPath: filepath.Join(t.TempDir(), "photo_nobg.png"),
			}},
			wantCode: BackgroundRemoveCodeInputUnsupported,
		},
		{
			name: "output is not png",
			items: []BackgroundRemoveItem{{
				InputPath:  inputPath,
				OutputPath: filepath.Join(t.TempDir(), "photo_nobg.webp"),
			}},
			wantCode: BackgroundRemoveCodeOutputInvalid,
		},
		{
			name: "output collides with input",
			items: []BackgroundRemoveItem{{
				InputPath:  inputPath,
				OutputPath: inputPath,
			}},
			wantCode: BackgroundRemoveCodeOutputInvalid,
		},
		{
			name: "invalid background color",
			items: []BackgroundRemoveItem{{
				InputPath:  inputPath,
				OutputPath: filepath.Join(t.TempDir(), "photo_nobg.png"),
			}},
			background: "red",
			wantCode:   BackgroundRemoveCodeInvalidOption,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &rembgStubRunner{onRun: func(context.Context, rembgStubCall, func(string)) error {
				t.Fatal("the runner must not be called for an invalid request")
				return nil
			}}

			_, err := RemoveBackgrounds(
				context.Background(),
				runner,
				pythonPath,
				modelPath,
				t.TempDir(),
				test.items,
				BackgroundRemoveOptions{BackgroundColor: test.background},
				nil,
			)
			if err == nil {
				t.Fatal("expected the request to fail")
			}

			var rembgErr *BackgroundRemoveError
			if !errors.As(err, &rembgErr) || rembgErr.Code != test.wantCode {
				t.Fatalf("expected %q, got %v", test.wantCode, err)
			}
			if calls := len(runner.snapshotCalls()); calls != 0 {
				t.Fatalf("expected no runner calls, got %d", calls)
			}
		})
	}
}

// TestRemoveBackgroundsRealRembg is the opt-in end-to-end check. It needs the
// managed pack installed and an image path in FILEFORGE_REMBG_E2E_IMAGE.
func TestRemoveBackgroundsRealRembg(t *testing.T) {
	if testing.Short() {
		t.Skip("real rembg run skipped in short mode")
	}

	imagePath := strings.TrimSpace(os.Getenv("FILEFORGE_REMBG_E2E_IMAGE"))
	if imagePath == "" {
		t.Skip("set FILEFORGE_REMBG_E2E_IMAGE to an image to run the real rembg check")
	}

	info, statErr := os.Stat(imagePath)
	if statErr != nil || !info.Mode().IsRegular() {
		t.Skipf("FILEFORGE_REMBG_E2E_IMAGE does not point to a regular file: %s", imagePath)
	}

	dataDir, err := toolchain.DataDir()
	if err != nil {
		t.Skipf("resolve the application data directory: %v", err)
	}
	pythonPath, pythonOK := toolchain.ManagedRembgPythonPath(dataDir)
	if !pythonOK {
		t.Skip("the managed rembg virtual environment is not installed")
	}
	modelPath, modelOK := toolchain.ManagedRembgModelPath(dataDir, toolchain.DefaultRembgModel)
	if !modelOK {
		t.Skip("the managed rembg model is not installed")
	}
	if _, readinessErr := resolveBackgroundRemoveRuntimeAt(dataDir); readinessErr != nil {
		t.Fatalf("the installed pack must pass the tool readiness gate, got %v", readinessErr)
	}

	outputPath := filepath.Join(t.TempDir(), "cutout.png")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	outcomes, removeErr := RemoveBackgrounds(
		ctx,
		nil,
		pythonPath,
		modelPath,
		"",
		[]BackgroundRemoveItem{{InputPath: imagePath, OutputPath: outputPath}},
		BackgroundRemoveOptions{},
		nil,
	)
	if removeErr != nil {
		t.Fatalf("real rembg run failed: %v", removeErr)
	}
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %d", len(outcomes))
	}

	colorType, colorErr := pngColorTypeOf(outputPath)
	if colorErr != nil {
		t.Fatalf("inspect the real output: %v", colorErr)
	}
	if colorType != pngColorTypeRGBA {
		t.Fatalf("expected an RGBA PNG, got color type %d", colorType)
	}

	file, openErr := os.Open(outputPath)
	if openErr != nil {
		t.Fatalf("open the real output: %v", openErr)
	}
	defer file.Close()

	decoded, decodeErr := png.Decode(file)
	if decodeErr != nil {
		t.Fatalf("decode the real output: %v", decodeErr)
	}
	if !rembgPNGHasTransparentPixel(decoded) {
		t.Fatal("expected the real rembg output to contain transparent pixels")
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != outcomes[0].Width || bounds.Dy() != outcomes[0].Height {
		t.Fatalf("outcome %dx%d does not match the PNG %dx%d", outcomes[0].Width, outcomes[0].Height, bounds.Dx(), bounds.Dy())
	}
	t.Logf("real rembg published a %dx%d RGBA PNG", bounds.Dx(), bounds.Dy())
}

func TestRembgHomeFromModelPath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "rembg")
	modelPath := filepath.Join(home, "models", toolchain.DefaultRembgModel, toolchain.DefaultRembgModel+".onnx")

	got, err := rembgHomeFromModelPath(modelPath)
	if err != nil {
		t.Fatalf("expected a valid layout, got %v", err)
	}
	if got != home {
		t.Fatalf("expected %q, got %q", home, got)
	}

	if _, err := rembgHomeFromModelPath(filepath.Join(t.TempDir(), "flat.onnx")); err == nil {
		t.Fatal("expected a flat model path to be rejected")
	}
}
