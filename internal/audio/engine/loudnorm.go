package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"fileforge-desktop/internal/platform/ffmpegex"
)

// EBU R128 target presets. The enum names are stable options; the numeric value
// is the integrated loudness the second pass aims for.
const (
	LoudnormStreaming = "streaming"
	LoudnormBroadcast = "broadcast"
	LoudnormPodcast   = "podcast"

	DefaultTruePeak = -1.5
)

// LoudnormTarget is one selectable loudness target.
type LoudnormTarget struct {
	Name               string
	IntegratedLoudness float64
}

var loudnormTargets = []LoudnormTarget{
	{Name: LoudnormStreaming, IntegratedLoudness: -14},
	{Name: LoudnormBroadcast, IntegratedLoudness: -23},
	{Name: LoudnormPodcast, IntegratedLoudness: -16},
}

// ResolveLoudnormTarget accepts a preset name or its numeric LUFS value. An
// empty value selects streaming (-14 LUFS).
func ResolveLoudnormTarget(raw string) (LoudnormTarget, *AudioError) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return loudnormTargets[0], nil
	}

	for _, target := range loudnormTargets {
		if target.Name == key {
			return target, nil
		}
	}

	if value, err := strconv.ParseFloat(key, 64); err == nil {
		for _, target := range loudnormTargets {
			if math.Abs(target.IntegratedLoudness-value) < 0.01 {
				return target, nil
			}
		}
	}

	return LoudnormTarget{}, &AudioError{
		Code:    ErrorCodeTargetLufsInvalid,
		Message: "targetLufs must be streaming, broadcast, podcast or one of -14, -16, -23",
	}
}

// ValidateTruePeak accepts a true-peak ceiling between -9 and 0 dBTP. The
// default is -1.5 dBTP when the option is omitted.
func ValidateTruePeak(value float64) *AudioError {
	if math.IsNaN(value) || math.IsInf(value, 0) || value > 0 || value < -9 {
		return &AudioError{Code: ErrorCodeTruePeakInvalid, Message: "truePeak must be between -9 and 0 dBTP"}
	}

	return nil
}

// LoudnormResult is the measured outcome of one normalization: the input
// loudness, the achieved output loudness, and the target applied.
type LoudnormResult struct {
	TargetName        string
	TargetLUFS        float64
	TruePeak          float64
	InputLUFS         float64
	InputTruePeak     float64
	OutputLUFS        float64
	OutputTruePeak    float64
	NormalizationType string
}

// loudnormReport mirrors the JSON summary printed by the loudnorm filter.
type loudnormReport struct {
	InputI            string `json:"input_i"`
	InputTP           string `json:"input_tp"`
	InputLRA          string `json:"input_lra"`
	InputThresh       string `json:"input_thresh"`
	OutputI           string `json:"output_i"`
	OutputTP          string `json:"output_tp"`
	OutputLRA         string `json:"output_lra"`
	OutputThresh      string `json:"output_thresh"`
	NormalizationType string `json:"normalization_type"`
	TargetOffset      string `json:"target_offset"`
}

// ParseLoudnormReport extracts the last JSON object from a loudnorm log. It is
// exported for tests that do not want to run ffmpeg.
func ParseLoudnormReport(output string) (LoudnormResult, bool) {
	report, ok := parseLoudnormReport(output)
	if !ok {
		return LoudnormResult{}, false
	}

	return LoudnormResult{
		InputLUFS:         parseLoudnessValue(report.InputI),
		InputTruePeak:     parseLoudnessValue(report.InputTP),
		OutputLUFS:        parseLoudnessValue(report.OutputI),
		OutputTruePeak:    parseLoudnessValue(report.OutputTP),
		NormalizationType: strings.TrimSpace(report.NormalizationType),
	}, true
}

func parseLoudnormReport(output string) (loudnormReport, bool) {
	start := strings.LastIndex(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return loudnormReport{}, false
	}

	var report loudnormReport
	if err := json.Unmarshal([]byte(output[start:end+1]), &report); err != nil {
		return loudnormReport{}, false
	}
	if strings.TrimSpace(report.InputI) == "" {
		return loudnormReport{}, false
	}

	return report, true
}

func parseLoudnessValue(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}

	return value
}

// BuildLoudnormFilter builds the first-pass measurement filter.
func BuildLoudnormFilter(target LoudnormTarget, truePeak float64, printFormat string) string {
	filter := fmt.Sprintf("loudnorm=I=%.1f:TP=%.1f:LRA=11", target.IntegratedLoudness, truePeak)
	if strings.TrimSpace(printFormat) != "" {
		filter += ":print_format=" + strings.TrimSpace(printFormat)
	}

	return filter
}

// BuildLoudnormApplyFilter builds the second-pass filter from the first-pass
// measurement. linear mode keeps the original dynamics when possible.
func BuildLoudnormApplyFilter(target LoudnormTarget, truePeak float64, report loudnormReport) string {
	return fmt.Sprintf(
		"loudnorm=I=%.1f:TP=%.1f:LRA=11:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true:print_format=json",
		target.IntegratedLoudness,
		truePeak,
		defaultLoudnessValue(report.InputI, "-70"),
		defaultLoudnessValue(report.InputTP, "0"),
		defaultLoudnessValue(report.InputLRA, "0"),
		defaultLoudnessValue(report.InputThresh, "-70"),
		defaultLoudnessValue(report.TargetOffset, "0"),
	)
}

func defaultLoudnessValue(raw, fallback string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}

	return trimmed
}

// measureArgs builds the first pass: decode the whole input, run the loudnorm
// filter with its JSON summary, and write nothing. The log level must be info
// because the filter prints its report there.
func measureArgs(inputPath string, target LoudnormTarget, truePeak float64) []string {
	return []string{
		"-hide_banner",
		"-nostats",
		"-nostdin",
		"-loglevel", "info",
		"-i", strings.TrimSpace(inputPath),
		"-af", BuildLoudnormFilter(target, truePeak, "json"),
		"-f", "null",
		"-",
	}
}

// applyArgs builds the second pass: apply the measured values and encode the
// chosen output format.
func applyArgs(req Request, target LoudnormTarget, truePeak float64, report loudnormReport, outputPath string) ([]string, *AudioError) {
	format, formatErr := FormatByName(req.Format)
	if formatErr != nil {
		return nil, formatErr
	}

	args := []string{
		"-hide_banner",
		"-nostats",
		"-nostdin",
		"-loglevel", "info",
		"-y",
		"-i", strings.TrimSpace(req.InputPath),
		"-vn",
		"-af", BuildLoudnormApplyFilter(target, truePeak, report),
		"-c:a", format.Encoder,
	}
	if !format.Lossless {
		bitrate := req.BitrateKbps
		if bitrate <= 0 {
			bitrate = format.DefaultBitrateKbps
		}
		args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate))
	}
	if format.Name == FormatM4A {
		args = append(args, "-movflags", "+faststart")
	}

	return append(args, outputPath), nil
}

// Normalize runs the EBU R128 two-pass loudness normalization. Pass one
// measures the input; pass two applies the measured values and reports the
// achieved output loudness from the same JSON summary.
func Normalize(ctx context.Context, probe RuntimeProbe, runner ffmpegex.CommandRunner, req Request, target LoudnormTarget, truePeak float64, onProgress func(percent float64, message string)) (LoudnormResult, *AudioError) {
	probe = resolveRuntimeProbe(probe)
	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		return LoudnormResult{}, &AudioError{Code: runtimeErrorCode(runtimeErr), Message: runtimeErrorMessage(runtimeErr), Cause: runtimeErr}
	}
	if runner == nil {
		runner = &ffmpegex.ExecCommandRunner{}
	}
	capture, ok := runner.(ffmpegex.CaptureCommandRunner)
	if !ok {
		return LoudnormResult{}, &AudioError{Code: ErrorCodeExecutionFailed, Message: "the ffmpeg runner does not support loudness capture"}
	}

	if validationErr := ValidateRequest(req); validationErr != nil {
		return LoudnormResult{}, validationErr
	}

	durationSeconds := probeSourceDuration(ctx, req.InputPath)

	// Pass one: measure. Half of the progress bar belongs to this decode pass.
	measureProgress := mapProgress(onProgress, 0, 50, "Measuring loudness…")
	_, measureStderr, measureErr := capture.RunCapture(ctx, "ffmpeg", measureArgs(req.InputPath, target, truePeak), durationSeconds, measureProgress)
	if measureErr != nil {
		return LoudnormResult{}, mapExecutionError(measureErr, "loudness measurement")
	}
	report, parsed := parseLoudnormReport(measureStderr)
	if !parsed {
		return LoudnormResult{}, &AudioError{Code: ErrorCodeLoudnormFailed, Message: "ffmpeg did not return a loudness measurement"}
	}

	tempPath, tempErr := ffmpegex.TempOutputPath(req.OutputPath)
	if tempErr != nil {
		return LoudnormResult{}, &AudioError{Code: ErrorCodeOutputWriteFailed, Message: "audio output could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			ffmpegex.RemoveTempOutput(tempPath)
		}
	}()

	args, argsErr := applyArgs(req, target, truePeak, report, tempPath)
	if argsErr != nil {
		return LoudnormResult{}, argsErr
	}

	// Pass two: apply. The JSON summary reports the achieved output values.
	applyProgress := mapProgress(onProgress, 50, 100, "Applying loudness normalization…")
	_, applyStderr, applyErr := capture.RunCapture(ctx, "ffmpeg", args, durationSeconds, applyProgress)
	if applyErr != nil {
		return LoudnormResult{}, mapExecutionError(applyErr, "loudness normalization")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return LoudnormResult{}, mapExecutionError(ctxErr, "loudness normalization")
	}

	applied := report
	if appliedReport, appliedOK := parseLoudnormReport(applyStderr); appliedOK {
		applied = appliedReport
	}

	if publishErr := ffmpegex.PublishOutput(tempPath, req.OutputPath); publishErr != nil {
		return LoudnormResult{}, &AudioError{Code: ErrorCodeOutputWriteFailed, Message: "audio output could not be published", Cause: publishErr}
	}
	published = true

	return LoudnormResult{
		TargetName:        target.Name,
		TargetLUFS:        target.IntegratedLoudness,
		TruePeak:          truePeak,
		InputLUFS:         parseLoudnessValue(report.InputI),
		InputTruePeak:     parseLoudnessValue(report.InputTP),
		OutputLUFS:        parseLoudnessValue(applied.OutputI),
		OutputTruePeak:    parseLoudnessValue(applied.OutputTP),
		NormalizationType: strings.TrimSpace(applied.NormalizationType),
	}, nil
}

// mapProgress rescales one pass's 0..100 into a slice of the overall 0..100.
func mapProgress(onProgress func(float64, string), start, end float64, message string) func(float64, string) {
	if onProgress == nil {
		return nil
	}

	return func(percent float64, _ string) {
		scaled := start + (end-start)*clampAudioPercent(percent)/100
		onProgress(scaled, message)
	}
}

func clampAudioPercent(percent float64) float64 {
	if math.IsNaN(percent) || percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}

	return percent
}
