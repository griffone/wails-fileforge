package engine

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"strconv"
	"strings"
)

// Progress is one parsed yt-dlp progress sample. Percent stays 0 while the
// total is unknown (NA) so the UI can show an indeterminate bar instead of a
// fabricated number.
type Progress struct {
	Percent         float64
	DownloadedBytes int64
	TotalBytes      int64
	SpeedBPS        float64
	ETA             int64
}

// Download runs the chosen quality for one link and returns the final file
// path. The path is the after_move:filepath line yt-dlp prints once the merge
// finished; a success without a real file is reported as CodeNoOutput.
func Download(ctx context.Context, runner CommandRunner, req Request, onProgress func(Progress)) (string, *WebError) {
	args, buildErr := BuildArgs(req)
	if buildErr != nil {
		return "", buildErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	tracker := &progressTracker{onProgress: onProgress}
	var lastPath string
	result, runErr := runner.Run(ctx, toolBinary, args, func(line string) {
		if tracker.handle(line) {
			return
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "[") {
			return
		}
		lastPath = trimmed
	})
	if runErr != nil {
		return "", classifyCommandError(ctx, runErr, result, CodeExecutionFailed, "the download failed")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", &WebError{
			Code:       CodeCanceled,
			DetailCode: detailCanceled,
			Message:    "the download was canceled",
			Details:    map[string]any{"cause": ctxErr.Error()},
		}
	}

	finalPath := strings.TrimSpace(lastPath)
	if finalPath == "" {
		return "", &WebError{
			Code:       CodeNoOutput,
			DetailCode: detailNoOutputPath,
			Message:    "yt-dlp did not report an output file",
		}
	}

	info, statErr := os.Stat(finalPath)
	if statErr != nil {
		details := map[string]any{"path": finalPath}
		if errors.Is(statErr, fs.ErrNotExist) {
			details["reason"] = "not_found"
		} else {
			details["error"] = statErr.Error()
		}

		return "", &WebError{
			Code:       CodeNoOutput,
			DetailCode: detailOutputMissing,
			Message:    "the reported output file does not exist",
			Details:    details,
		}
	}
	if info.IsDir() {
		return "", &WebError{
			Code:       CodeNoOutput,
			DetailCode: detailOutputMissing,
			Message:    "the reported output path is a directory, not a media file",
			Details:    map[string]any{"path": finalPath, "reason": "not_a_file"},
		}
	}

	return finalPath, nil
}

// parseDownloadProgress reads one `ff-dl` line. A partial or malformed line is
// ignored: the next full line restores the stream.
func parseDownloadProgress(line string) (Progress, bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 5 || fields[0] != downloadProgressPrefix {
		return Progress{}, false
	}

	return Progress{
		DownloadedBytes: progressInt(fields[1]),
		TotalBytes:      progressInt(fields[2]),
		SpeedBPS:        progressFloat(fields[3]),
		ETA:             int64(math.Round(progressFloat(fields[4]))),
	}, true
}

func progressInt(value string) int64 {
	parsed, ok := progressFloatOK(value)
	if !ok {
		return 0
	}

	return int64(parsed)
}

func progressFloat(value string) float64 {
	parsed, _ := progressFloatOK(value)
	return parsed
}

func progressFloatOK(value string) (float64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.EqualFold(trimmed, "NA") || strings.EqualFold(trimmed, "none") {
		return 0, false
	}

	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, false
	}

	return parsed, true
}

// progressTracker turns raw lines into monotonic Progress samples. Percent is
// derived from downloaded/total; when the total is NA the last known percent is
// kept (normally 0) and the sample stays indeterminate.
type progressTracker struct {
	onProgress func(Progress)
	percent    float64
}

func (t *progressTracker) handle(line string) bool {
	sample, ok := parseDownloadProgress(line)
	if !ok {
		return false
	}

	if sample.TotalBytes > 0 {
		percent := float64(sample.DownloadedBytes) / float64(sample.TotalBytes) * 100
		percent = clampPercent(percent)
		if percent < t.percent {
			percent = t.percent
		}
		t.percent = percent
		sample.Percent = percent
	} else {
		sample.Percent = t.percent
	}

	if t.onProgress != nil {
		t.onProgress(sample)
	}

	return true
}

func clampPercent(value float64) float64 {
	switch {
	case math.IsNaN(value), value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}
