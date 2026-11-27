package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"fileforge-desktop/internal/platform/execx"
)

func downloadTestRequest(t *testing.T) Request {
	t.Helper()

	return Request{
		URL:       "https://www.youtube.com/watch?v=abc",
		OutputDir: t.TempDir(),
		Kind:      KindVideo,
	}
}

func TestDownloadHappyPath(t *testing.T) {
	req := downloadTestRequest(t)
	outputPath := filepath.Join(req.OutputDir, "Clip [abc].mp4")

	runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
		emit("ff-dl 0 2048 0 NA")
		emit("ff-dl 1024 2048 512.5 2")
		emit("ff-dl 1536 2048 500 1")
		if err := os.WriteFile(outputPath, []byte("media"), 0o644); err != nil {
			return execx.Result{}, err
		}
		emit("ff-dl 2048 2048 NA 0")
		emit("[download] 100% of 2.00KiB") // informational noise must not win
		emit(outputPath)
		return execx.Result{}, nil
	}}

	var samples []Progress
	path, downloadErr := Download(context.Background(), runner, req, func(sample Progress) {
		samples = append(samples, sample)
	})
	if downloadErr != nil {
		t.Fatalf("expected a successful download, got %v", downloadErr)
	}
	if path != outputPath {
		t.Fatalf("expected path %q, got %q", outputPath, path)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected one command, got %d", len(runner.calls))
	}
	if runner.calls[0].path != toolBinary {
		t.Fatalf("expected %s, got %s", toolBinary, runner.calls[0].path)
	}
	if !argsContain(runner.calls[0].args, req.OutputDir) {
		t.Fatalf("expected the output dir in the args, got %v", runner.calls[0].args)
	}
	if got := runner.calls[0].args[len(runner.calls[0].args)-1]; got != req.URL {
		t.Fatalf("expected the URL last, got %q", got)
	}

	wantPercents := []float64{0, 50, 75, 100}
	gotPercents := make([]float64, 0, len(samples))
	for _, sample := range samples {
		gotPercents = append(gotPercents, sample.Percent)
	}
	if !reflect.DeepEqual(gotPercents, wantPercents) {
		t.Fatalf("expected percents %v, got %v", wantPercents, gotPercents)
	}
}

func TestDownloadKeepsUnknownTotalIndeterminate(t *testing.T) {
	req := downloadTestRequest(t)
	outputPath := filepath.Join(req.OutputDir, "Clip [abc].mp4")

	runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
		emit("ff-dl 512 NA 1024.5 5")
		if err := os.WriteFile(outputPath, []byte("media"), 0o644); err != nil {
			return execx.Result{}, err
		}
		emit(outputPath)
		return execx.Result{}, nil
	}}

	var samples []Progress
	if _, downloadErr := Download(context.Background(), runner, req, func(sample Progress) {
		samples = append(samples, sample)
	}); downloadErr != nil {
		t.Fatalf("expected a successful download, got %v", downloadErr)
	}

	if len(samples) != 1 {
		t.Fatalf("expected one progress sample, got %d", len(samples))
	}
	if samples[0].Percent != 0 || samples[0].TotalBytes != 0 || samples[0].DownloadedBytes != 512 {
		t.Fatalf("unexpected indeterminate sample: %+v", samples[0])
	}
	if samples[0].SpeedBPS != 1024.5 || samples[0].ETA != 5 {
		t.Fatalf("expected speed and eta to survive, got %+v", samples[0])
	}
}

func TestDownloadMissingOutputFile(t *testing.T) {
	req := downloadTestRequest(t)
	reportedPath := filepath.Join(req.OutputDir, "Clip [abc].mp4")

	runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
		emit(reportedPath)
		return execx.Result{}, nil
	}}

	path, downloadErr := Download(context.Background(), runner, req, nil)
	if downloadErr == nil {
		t.Fatalf("expected a NO_OUTPUT error, got path %q", path)
	}
	if downloadErr.Code != CodeNoOutput || downloadErr.DetailCode != detailOutputMissing {
		t.Fatalf("unexpected error: %+v", downloadErr)
	}
	if downloadErr.Details["path"] != reportedPath {
		t.Fatalf("expected the reported path in Details, got %v", downloadErr.Details)
	}
}

func TestDownloadWithoutReportedPath(t *testing.T) {
	req := downloadTestRequest(t)
	runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
		emit("ff-dl 1 1 1 0")
		return execx.Result{}, nil
	}}

	_, downloadErr := Download(context.Background(), runner, req, nil)
	if downloadErr == nil || downloadErr.Code != CodeNoOutput || downloadErr.DetailCode != detailNoOutputPath {
		t.Fatalf("expected a no-path error, got %+v", downloadErr)
	}
}

func TestDownloadCanceled(t *testing.T) {
	req := downloadTestRequest(t)
	ctx, cancel := context.WithCancel(context.Background())

	runner := &fakeRunner{run: func(_ runnerCall, _ func(string)) (execx.Result, error) {
		cancel()
		return execx.Result{}, context.Canceled
	}}

	_, downloadErr := Download(ctx, runner, req, nil)
	if downloadErr == nil || downloadErr.Code != CodeCanceled {
		t.Fatalf("expected a canceled error, got %+v", downloadErr)
	}
}

func TestDownloadKilledProcessMapsToCanceled(t *testing.T) {
	req := downloadTestRequest(t)
	runner := &fakeRunner{run: func(_ runnerCall, _ func(string)) (execx.Result, error) {
		return execx.Result{ExitCode: -1}, &execx.Error{ExitCode: -1, Err: errors.New("signal: killed")}
	}}

	_, downloadErr := Download(context.Background(), runner, req, nil)
	if downloadErr == nil || downloadErr.Code != CodeCanceled || downloadErr.DetailCode != detailProcessKilled {
		t.Fatalf("expected a canceled error, got %+v", downloadErr)
	}
}

func TestDownloadCommandFailureKeepsBoundedStderr(t *testing.T) {
	req := downloadTestRequest(t)
	runner := &fakeRunner{run: func(_ runnerCall, _ func(string)) (execx.Result, error) {
		return execx.Result{ExitCode: 1, Stderr: "ERROR: unable to download video data"}, errors.New("exit status 1")
	}}

	_, downloadErr := Download(context.Background(), runner, req, nil)
	if downloadErr == nil || downloadErr.Code != CodeExecutionFailed {
		t.Fatalf("expected an execution error, got %+v", downloadErr)
	}
	if downloadErr.Details["exitCode"] != 1 {
		t.Fatalf("expected exitCode 1 in Details, got %v", downloadErr.Details)
	}
	stderr, _ := downloadErr.Details["stderr"].(string)
	if stderr == "" {
		t.Fatalf("expected the stderr tail in Details, got %v", downloadErr.Details)
	}
}

func TestDownloadReportsMissingRuntime(t *testing.T) {
	req := downloadTestRequest(t)
	runner := &fakeRunner{run: func(_ runnerCall, _ func(string)) (execx.Result, error) {
		return execx.Result{ExitCode: -1}, &execx.Error{ExitCode: -1, Err: exec.ErrNotFound}
	}}

	_, downloadErr := Download(context.Background(), runner, req, nil)
	if downloadErr == nil || downloadErr.Code != CodeRuntimeUnavailable {
		t.Fatalf("expected a runtime error, got %+v", downloadErr)
	}
}

func TestDownloadDoesNotCreateMissingOutputDir(t *testing.T) {
	baseDir := t.TempDir()
	missingDir := filepath.Join(baseDir, "missing")
	runner := &fakeRunner{}

	_, downloadErr := Download(context.Background(), runner, Request{
		URL:       "https://www.youtube.com/watch?v=abc",
		OutputDir: missingDir,
		Kind:      KindVideo,
	}, nil)
	if downloadErr == nil || downloadErr.Code != CodeExecutionFailed || downloadErr.DetailCode != detailOutputDirNotFound {
		t.Fatalf("expected an output dir error, got %+v", downloadErr)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("no command must run with a missing output dir, got %d calls", len(runner.calls))
	}
	if _, statErr := os.Stat(missingDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the output directory must not be created, got %v", statErr)
	}
}
