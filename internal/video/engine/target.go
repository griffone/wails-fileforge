package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/inspect"
)

const (
	// ErrorCodeVideoTargetInvalid reports a target that cannot be used as a
	// size budget.
	ErrorCodeVideoTargetInvalid = "VIDEO_TARGET_INVALID"
	// ErrorCodeVideoTargetDurationUnavailable reports that the real output
	// duration could not be read, so the target bitrate cannot be computed
	// honestly.
	ErrorCodeVideoTargetDurationUnavailable = "VIDEO_TARGET_DURATION_UNAVAILABLE"
	// ErrorCodeVideoTargetExecutionFailed reports a two-pass encode that could
	// not run or could not produce a file.
	ErrorCodeVideoTargetExecutionFailed = "VIDEO_TARGET_EXECUTION_FAILED"

	// TargetAudioBitrateAACKbps and TargetAudioBitrateOpusKbps are the audio
	// bitrates reserved out of the target budget. They match the bitrate the
	// two-pass target encoder actually asks for, so the subtraction is honest.
	TargetAudioBitrateAACKbps  = 128
	TargetAudioBitrateOpusKbps = 96

	// TargetMaxRetries bounds how many extra passes are attempted after the
	// first encode overshoots the target.
	TargetMaxRetries = 2

	// targetRetryScalePercent is how much of the previous video bitrate is
	// kept for the next attempt.
	targetRetryScalePercent = 90

	targetPassLogName = "fileforge-2pass"
)

// TargetMedia is the source information needed to size a target encode.
type TargetMedia struct {
	DurationSeconds float64
	Height          int
	HasAudio        bool
}

// TargetMediaProbe reports the duration, video height and audio presence of a
// source file. It is a package variable so tests can supply deterministic
// media without ffprobe.
var TargetMediaProbe = func(ctx context.Context, path string) (TargetMedia, bool) {
	metadata, ok := inspect.ProbeVideoMetadata(ctx, path)
	if !ok {
		return TargetMedia{}, false
	}
	if metadata.DurationSeconds <= 0 || math.IsNaN(metadata.DurationSeconds) || math.IsInf(metadata.DurationSeconds, 0) {
		return TargetMedia{}, false
	}

	return TargetMedia{
		DurationSeconds: metadata.DurationSeconds,
		Height:          metadata.Height,
		HasAudio:        strings.TrimSpace(metadata.AudioCodec) != "",
	}, true
}

// ProbeTargetMediaOrDuration is the shared probe for the tool layer and the
// encoder: plan-time warnings and execution cannot disagree. It prefers the
// rich
// metadata probe and falls back to the duration-only seam, assuming audio is
// present so the budget stays conservative. Callers that already stub
// SourceDurationProbe keep working.
func ProbeTargetMediaOrDuration(ctx context.Context, path string) (TargetMedia, bool) {
	if media, ok := TargetMediaProbe(ctx, path); ok {
		return media, true
	}
	duration, ok := SourceDurationProbe(ctx, path)
	if !ok || duration <= 0 {
		return TargetMedia{}, false
	}

	return TargetMedia{DurationSeconds: duration, HasAudio: true}, true
}

// TargetAudioKbps returns the audio bitrate reserved for the target format.
// Zero audio is reserved when the stream is removed or absent.
func TargetAudioKbps(targetFormat string, hasAudio bool) int {
	if !hasAudio {
		return 0
	}
	if strings.EqualFold(strings.TrimSpace(targetFormat), TargetFormatWebM) {
		return TargetAudioBitrateOpusKbps
	}

	return TargetAudioBitrateAACKbps
}

// ComputeTargetVideoBitrateKbps turns a byte target and a real output duration
// into the video bitrate available after reserving the audio stream. The result
// can be zero or negative when the target is smaller than the audio budget; use
// ResolveTargetVideoBitrate to apply the quality floor.
func ComputeTargetVideoBitrateKbps(targetBytes int64, durationSeconds float64, audioKbps int) int {
	if targetBytes <= 0 || durationSeconds <= 0 || math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) {
		return 0
	}
	if audioKbps < 0 {
		audioKbps = 0
	}

	totalKbps := int(math.Floor(float64(targetBytes) * 8 / 1000 / durationSeconds))
	videoKbps := totalKbps - audioKbps
	if videoKbps < 0 {
		return 0
	}

	return videoKbps
}

// MinVideoBitrateKbps returns the quality floor for a resolution. A preset uses
// its own floor; original resolution derives the floor from the source height.
func MinVideoBitrateKbps(resolution string, sourceHeight int) int {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case Resolution480p:
		return 500
	case Resolution720p:
		return 1000
	case Resolution1080p:
		return 2000
	case "", ResolutionOriginal:
		return minVideoBitrateForHeight(sourceHeight)
	default:
		return 1000
	}
}

func minVideoBitrateForHeight(height int) int {
	switch {
	case height >= 2160:
		return 6000
	case height >= 1440:
		return 4000
	case height >= 1080:
		return 2000
	case height >= 720:
		return 1000
	case height >= 480:
		return 500
	case height > 0:
		return 300
	default:
		// An unknown original height is treated as 720p so the floor stays
		// conservative instead of silently allowing a tiny bitrate.
		return 1000
	}
}

// FloorLabel names the floor tier for a warning. It never guesses a height it
// does not have.
func FloorLabel(resolution string, sourceHeight int) string {
	switch strings.ToLower(strings.TrimSpace(resolution)) {
	case Resolution480p:
		return "480p"
	case Resolution720p:
		return "720p"
	case Resolution1080p:
		return "1080p"
	case "", ResolutionOriginal:
		switch {
		case sourceHeight >= 2160:
			return "2160p"
		case sourceHeight >= 1440:
			return "1440p"
		case sourceHeight >= 1080:
			return "1080p"
		case sourceHeight >= 720:
			return "720p"
		case sourceHeight >= 480:
			return "480p"
		case sourceHeight > 0:
			return "the original resolution"
		default:
			return "the assumed 720p tier"
		}
	default:
		return "the default tier"
	}
}

// ResolveTargetVideoBitrate applies the quality floor to the computed bitrate.
// floorApplied is true when the target implied a bitrate below the floor.
func ResolveTargetVideoBitrate(targetBytes int64, durationSeconds float64, audioKbps, floorKbps int) (bitrateKbps int, floorApplied bool) {
	computed := ComputeTargetVideoBitrateKbps(targetBytes, durationSeconds, audioKbps)
	if floorKbps <= 0 {
		return computed, false
	}
	if computed < floorKbps {
		return floorKbps, true
	}

	return computed, false
}

// TargetProgressEvent reports one step of a two-pass target encode. Attempt is
// 1 for the first encode and grows with each retry. Percent is the position
// inside the attempt (0..100 across both passes).
type TargetProgressEvent struct {
	Attempt int
	Pass    int
	Percent float64
	Stage   string
	Message string
}

// TargetEncodeOutcome reports what the target encode actually produced.
type TargetEncodeOutcome struct {
	TargetAttempted bool
	TargetBytes     int64
	ActualBytes     int64
	BitrateKbps     int
	FloorKbps       int
	FloorApplied    bool
	TargetMet       bool
	Attempts        int
}

// TargetEncodeSpec describes one two-pass encode with bounded retries. The
// caller supplies the pass argument builder so convert, trim and merge keep
// their own input wiring while sharing the retry, progress and pass-log logic.
type TargetEncodeSpec struct {
	TargetBytes     int64
	DurationSeconds float64
	AudioKbps       int
	FloorKbps       int
	OutputPath      string
	BuildPassArgs   func(bitrateKbps, pass int, passLogPrefix, outputPath string) ([]string, *VideoError)
	MapRunError     func(error) *VideoError
	OnEvent         func(TargetProgressEvent)
}

// TargetEncode runs a two-pass encode, compares the real file size against the
// target and retries at a reduced bitrate at most TargetMaxRetries times. Every
// attempt reuses the same progress path. The pass log lives in a temporary
// directory that is always removed.
func TargetEncode(ctx context.Context, runner CommandRunner, spec TargetEncodeSpec) (TargetEncodeOutcome, *VideoError) {
	outcome := TargetEncodeOutcome{
		TargetAttempted: true,
		TargetBytes:     spec.TargetBytes,
		FloorKbps:       spec.FloorKbps,
	}

	if spec.TargetBytes <= 0 {
		outcome.TargetAttempted = false
		return outcome, nil
	}
	if spec.DurationSeconds <= 0 || math.IsNaN(spec.DurationSeconds) || math.IsInf(spec.DurationSeconds, 0) {
		return outcome, &VideoError{
			Code:    ErrorCodeVideoTargetDurationUnavailable,
			Message: "the output duration could not be read; a size target cannot be computed",
		}
	}

	bitrateKbps, floorApplied := ResolveTargetVideoBitrate(spec.TargetBytes, spec.DurationSeconds, spec.AudioKbps, spec.FloorKbps)
	outcome.BitrateKbps = bitrateKbps
	outcome.FloorApplied = floorApplied

	passLogDir, tempErr := os.MkdirTemp("", "fileforge-target-pass-*")
	if tempErr != nil {
		return outcome, &VideoError{
			Code:    ErrorCodeVideoTargetExecutionFailed,
			Message: "the two-pass log directory could not be created",
			Cause:   tempErr,
		}
	}
	defer func() {
		_ = os.RemoveAll(passLogDir)
	}()
	passLogPrefix := filepath.Join(passLogDir, targetPassLogName)

	maxAttempts := 1 + TargetMaxRetries
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		outcome.Attempts = attempt

		if ctxErr := ctx.Err(); ctxErr != nil {
			return outcome, mapTargetContextError(spec.MapRunError, ctxErr)
		}

		// A partial file from the previous attempt must not make the size
		// comparison or the retry encode lie.
		removeTempOutput(spec.OutputPath)

		for pass := 1; pass <= 2; pass++ {
			args, argsErr := spec.BuildPassArgs(bitrateKbps, pass, passLogPrefix, spec.OutputPath)
			if argsErr != nil {
				return outcome, argsErr
			}

			stage := fmt.Sprintf("target.pass%d", pass)
			if attempt > 1 {
				stage = fmt.Sprintf("target.retry%d.pass%d", attempt-1, pass)
			}
			passStart := float64((pass - 1) * 50)
			reportProgress := func(percent float64, _ string) {
				if spec.OnEvent == nil {
					return
				}
				spec.OnEvent(TargetProgressEvent{
					Attempt: attempt,
					Pass:    pass,
					Percent: passStart + clampPercent(percent)*50/100,
					Stage:   stage,
					Message: fmt.Sprintf("Encoding pass %d of 2 (attempt %d of %d)… %.0f%%", pass, attempt, maxAttempts, percent),
				})
			}

			runErr := runFFmpegWithProgress(ctx, runner, args, spec.DurationSeconds, reportProgress)
			if runErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return outcome, mapTargetContextError(spec.MapRunError, ctxErr)
				}
				return outcome, mapTargetRunError(spec.MapRunError, runErr)
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return outcome, mapTargetContextError(spec.MapRunError, ctxErr)
			}
		}

		info, statErr := os.Stat(spec.OutputPath)
		if statErr != nil {
			return outcome, &VideoError{
				Code:    ErrorCodeVideoTargetExecutionFailed,
				Message: "the target encode did not produce an output file",
				Cause:   statErr,
			}
		}
		outcome.ActualBytes = info.Size()
		if outcome.ActualBytes <= spec.TargetBytes {
			outcome.TargetMet = true
			return outcome, nil
		}
		if attempt == maxAttempts {
			break
		}

		nextBitrate := bitrateKbps * targetRetryScalePercent / 100
		if nextBitrate < spec.FloorKbps {
			nextBitrate = spec.FloorKbps
		}
		if nextBitrate >= bitrateKbps {
			// The floor already binds; another pass would produce the same
			// size, so report best effort instead of wasting an encode.
			break
		}
		bitrateKbps = nextBitrate
		outcome.BitrateKbps = bitrateKbps
		if spec.OnEvent != nil {
			spec.OnEvent(TargetProgressEvent{
				Attempt: attempt,
				Pass:    2,
				Percent: 100,
				Stage:   "target.retry",
				Message: fmt.Sprintf("Target not met (%s); retrying at %d kbps", formatTargetBytes(outcome.ActualBytes), bitrateKbps),
			})
		}
	}

	outcome.TargetMet = outcome.ActualBytes > 0 && outcome.ActualBytes <= spec.TargetBytes
	return outcome, nil
}

func mapTargetContextError(mapErr func(error) *VideoError, ctxErr error) *VideoError {
	if mapErr != nil {
		return mapErr(ctxErr)
	}

	return &VideoError{Code: "CANCELED", Message: "video target encode canceled", Cause: ctxErr}
}

func mapTargetRunError(mapErr func(error) *VideoError, runErr error) *VideoError {
	if mapErr != nil {
		return mapErr(runErr)
	}
	if errors.Is(runErr, context.Canceled) {
		return &VideoError{Code: "CANCELED", Message: "video target encode canceled", Cause: runErr}
	}

	return &VideoError{Code: ErrorCodeVideoTargetExecutionFailed, Message: "two-pass target encoding failed", Cause: runErr}
}

// appendTargetPassArgs appends the shared encoder settings plus the pass log
// arguments. Pass 1 writes no output and no audio; it only builds the
// statistics pass 2 reads.
func appendTargetPassArgs(args []string, encoding videoEncoding, removeAudio bool, pass int, passLogPrefix, outputPath string) []string {
	if pass == 1 {
		args = appendVideoEncodingArgs(args, encoding, true)
		return append(args, "-pass", "1", "-passlogfile", passLogPrefix, "-f", "null", os.DevNull)
	}

	args = appendVideoEncodingArgs(args, encoding, removeAudio)
	args = append(args, "-pass", "2", "-passlogfile", passLogPrefix)

	return append(args, outputPath)
}

// withTargetAudio sets the audio bitrate the two-pass encoder actually asks
// for, so the budget subtraction matches the encoded stream.
func (e videoEncoding) withTargetAudio(audioKbps int) videoEncoding {
	if audioKbps > 0 {
		e.audioBitrate = fmt.Sprintf("%dk", audioKbps)
	}

	return e
}

func formatTargetBytes(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}

	const (
		kib = 1024.0
		mib = kib * 1024.0
		gib = mib * 1024.0
	)

	switch {
	case float64(bytes) >= gib:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gib)
	case float64(bytes) >= mib:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mib)
	case float64(bytes) >= kib:
		return fmt.Sprintf("%.0f KB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
