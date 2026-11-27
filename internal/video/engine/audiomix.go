package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"fileforge-desktop/internal/platform/fsx"
)

// Audio-in-video sync rebuilds the audio stream while the video stream is
// always copied. The sign convention is the one the media-sync-pack annex
// defines and the analysis engine already reports:
//
//	OffsetSeconds is the position of the source recording's t = 0 on the video
//	timeline. sourceTime = videoTime - offset.
//
// A positive offset delays the source by that many seconds (the recorder
// started after the camera); a negative offset trims that many seconds from the
// source head (the recorder started before the camera).
const (
	ErrorCodeVideoAudioSyncValidation           = "VIDEO_AUDIO_SYNC_VALIDATION_ERROR"
	ErrorCodeVideoAudioSyncModeInvalid          = "VIDEO_AUDIO_SYNC_MODE_INVALID"
	ErrorCodeVideoAudioSyncInputNoVideo         = "VIDEO_AUDIO_SYNC_INPUT_NO_VIDEO"
	ErrorCodeVideoAudioSyncDurationUnavailable  = "VIDEO_AUDIO_SYNC_DURATION_UNAVAILABLE"
	ErrorCodeVideoAudioSyncSourceNotFound       = "VIDEO_AUDIO_SYNC_SOURCE_NOT_FOUND"
	ErrorCodeVideoAudioSyncSourceNoAudio        = "VIDEO_AUDIO_SYNC_SOURCE_NO_AUDIO"
	ErrorCodeVideoAudioSyncOffsetInvalid        = "VIDEO_AUDIO_SYNC_OFFSET_INVALID"
	ErrorCodeVideoAudioSyncSourceStartInvalid   = "VIDEO_AUDIO_SYNC_SOURCE_START_INVALID"
	ErrorCodeVideoAudioSyncSegmentsRequired     = "VIDEO_AUDIO_SYNC_SEGMENTS_REQUIRED"
	ErrorCodeVideoAudioSyncSegmentInvalid       = "VIDEO_AUDIO_SYNC_SEGMENT_INVALID"
	ErrorCodeVideoAudioSyncSegmentOverlap       = "VIDEO_AUDIO_SYNC_SEGMENT_OVERLAP"
	ErrorCodeVideoAudioSyncSegmentLimit         = "VIDEO_AUDIO_SYNC_SEGMENT_LIMIT"
	ErrorCodeVideoAudioSyncGainInvalid          = "VIDEO_AUDIO_SYNC_GAIN_INVALID"
	ErrorCodeVideoAudioSyncOriginalAudioInvalid = "VIDEO_AUDIO_SYNC_ORIGINAL_AUDIO_INVALID"
	ErrorCodeVideoAudioSyncOriginalAudioMissing = "VIDEO_AUDIO_SYNC_ORIGINAL_AUDIO_MISSING"
	ErrorCodeVideoAudioSyncBitrateInvalid       = "VIDEO_AUDIO_SYNC_BITRATE_INVALID"
	ErrorCodeVideoAudioSyncOutputExists         = "VIDEO_AUDIO_SYNC_OUTPUT_EXISTS"
	ErrorCodeVideoAudioSyncOutputCollides       = "VIDEO_AUDIO_SYNC_OUTPUT_COLLIDES_INPUT"
	ErrorCodeVideoAudioSyncCodecUnavailable     = "VIDEO_AUDIO_SYNC_CODEC_UNAVAILABLE"
	ErrorCodeVideoAudioSyncCancelled            = "VIDEO_AUDIO_SYNC_CANCELLED"
	ErrorCodeVideoAudioSyncExecutionFailed      = "VIDEO_AUDIO_SYNC_EXECUTION_FAILED"
	ErrorCodeVideoAudioSyncInputOpenFailed      = "VIDEO_AUDIO_SYNC_INPUT_OPEN_FAILED"
	ErrorCodeVideoAudioSyncOutputWriteFailed    = "VIDEO_AUDIO_SYNC_OUTPUT_WRITE_FAILED"
	ErrorCodeVideoAudioSyncLoudnormFailed       = "VIDEO_AUDIO_SYNC_LOUDNORM_FAILED"

	MixModeReplace  = "replace"
	MixModeSections = "sections"

	MixOriginalRemove = "remove"
	MixOriginalKeep   = "keep"
	MixOriginalDuck   = "duck"

	// MixMaxSegments bounds the filter graph. Every segment adds atrim, volume,
	// fades, adelay and one amix input; a hundred is already a very long graph
	// and anything above it is almost certainly a bad UI state.
	MixMaxSegments = 100

	MixDefaultBitrateKbps = 256
	MixMinBitrateKbps     = 64
	MixMaxBitrateKbps     = 512

	MixDefaultFadeSeconds = 0.015

	MixMinGainDB = -60.0
	MixMaxGainDB = 24.0

	mixSampleRate        = 48000
	mixPreviewVideoWidth = 640
	mixPreviewAudioKbps  = 128
	mixPreviewCRF        = 28

	mixLoudnormTarget   = -14.0
	mixLoudnormTruePeak = -1.5
)

// MixSegment is one placed block of a source recording on the video timeline.
// VideoStart/VideoEnd are video-timeline seconds; SourceStart is the source
// offset to trim before the block plays.
type MixSegment struct {
	VideoStart  float64 `json:"videoStart"`
	VideoEnd    float64 `json:"videoEnd"`
	SourceStart float64 `json:"sourceStart"`
	SourcePath  string  `json:"sourcePath"`
	GainDB      float64 `json:"gainDb"`
	FadeSeconds float64 `json:"fadeSeconds"`
}

type MixRequest struct {
	VideoPath  string
	OutputPath string
	// Mode is "replace" (one source on the whole timeline) or "sections"
	// (explicit blocks that may leave gaps).
	Mode string
	// SourcePath, OffsetSeconds and SourceStartSeconds are the replace-mode
	// inputs. OffsetSeconds follows the package sign convention.
	SourcePath         string
	OffsetSeconds      float64
	SourceStartSeconds float64
	// OriginalAudio is "remove" (default), "keep" or "duck". The original
	// branch is only mixed in for keep/duck.
	OriginalAudio  string
	OriginalGainDB float64
	SourceGainDB   float64
	// Segments are the sections-mode blocks. They must be sorted by VideoStart
	// and must not overlap.
	Segments []MixSegment
	// AudioBitrateKbps is the AAC bitrate. Zero selects the default.
	AudioBitrateKbps int
	// Normalize runs the two-pass EBU R128 loudnorm path on the mixed audio.
	Normalize bool
	// DurationSeconds is the video duration. The tail pads and cuts the mixed
	// audio to exactly this length.
	DurationSeconds float64
}

// normalizeMixMode returns the effective mode, treating an empty value as the
// replace default.
func normalizeMixMode(mode string) string {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return MixModeReplace
	}

	return normalized
}

// normalizeMixOriginalAudio returns the effective original-audio policy.
func normalizeMixOriginalAudio(policy string) string {
	normalized := strings.ToLower(strings.TrimSpace(policy))
	if normalized == "" {
		return MixOriginalRemove
	}

	return normalized
}

// ValidateMix validates everything the graph builder needs: sources exist,
// times are finite and ordered, gains are inside the safe range and the output
// does not collide with an input. It never probes media content; the tool layer
// supplies the stream checks.
func ValidateMix(req MixRequest) *VideoError {
	videoPath := strings.TrimSpace(req.VideoPath)
	if videoPath == "" {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: "videoPath is required"}
	}
	if _, err := os.Stat(videoPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("video does not exist: %s", videoPath), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("video is not accessible: %s", videoPath), Cause: err}
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: "outputPath is required"}
	}

	duration := req.DurationSeconds
	if !isFinite(duration) || duration <= 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncDurationUnavailable, Message: "the video duration must be a positive number"}
	}

	bitrate := effectiveMixBitrate(req.AudioBitrateKbps)
	if bitrate < MixMinBitrateKbps || bitrate > MixMaxBitrateKbps {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncBitrateInvalid,
			Message: fmt.Sprintf("audioBitrateKbps must be between %d and %d", MixMinBitrateKbps, MixMaxBitrateKbps),
		}
	}

	mode := normalizeMixMode(req.Mode)
	switch mode {
	case MixModeReplace:
		if validationErr := validateReplaceMix(req, duration); validationErr != nil {
			return validationErr
		}
	case MixModeSections:
		if validationErr := validateSectionsMix(req, duration); validationErr != nil {
			return validationErr
		}
	default:
		return &VideoError{Code: ErrorCodeVideoAudioSyncModeInvalid, Message: "mode must be replace or sections"}
	}

	originalAudio := normalizeMixOriginalAudio(req.OriginalAudio)
	switch originalAudio {
	case MixOriginalRemove, MixOriginalKeep, MixOriginalDuck:
	default:
		return &VideoError{Code: ErrorCodeVideoAudioSyncOriginalAudioInvalid, Message: "originalAudio must be remove, keep, or duck"}
	}
	if gainErr := validateMixGain(req.OriginalGainDB, "originalGainDb"); gainErr != nil {
		return gainErr
	}
	if gainErr := validateMixGain(req.SourceGainDB, "sourceGainDb"); gainErr != nil {
		return gainErr
	}

	for _, sourcePath := range mixSourcePaths(req) {
		if fsx.NormalizePathKey(sourcePath) == fsx.NormalizePathKey(outputPath) {
			return &VideoError{Code: ErrorCodeVideoAudioSyncOutputCollides, Message: "outputPath collides with a source input"}
		}
	}
	if fsx.NormalizePathKey(videoPath) == fsx.NormalizePathKey(outputPath) {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputCollides, Message: "outputPath collides with the video input"}
	}

	if _, err := os.Stat(outputPath); err == nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
	}

	return nil
}

func validateReplaceMix(req MixRequest, duration float64) *VideoError {
	sourcePath := strings.TrimSpace(req.SourcePath)
	if sourcePath == "" {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: "sourcePath is required in replace mode"}
	}
	if _, err := os.Stat(sourcePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoAudioSyncSourceNotFound, Message: fmt.Sprintf("source does not exist: %s", sourcePath), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("source is not accessible: %s", sourcePath), Cause: err}
	}

	if !isFinite(req.OffsetSeconds) {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOffsetInvalid, Message: "offsetSeconds must be a finite number"}
	}
	if !isFinite(req.SourceStartSeconds) || req.SourceStartSeconds < 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncSourceStartInvalid, Message: "sourceStartSeconds must be >= 0"}
	}
	if req.OffsetSeconds >= duration {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncOffsetInvalid,
			Message: fmt.Sprintf("offsetSeconds (%.3f s) must be before the video duration (%.3f s)", req.OffsetSeconds, duration),
		}
	}

	return nil
}

func validateSectionsMix(req MixRequest, duration float64) *VideoError {
	if len(req.Segments) == 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncSegmentsRequired, Message: "at least 1 segment is required in sections mode"}
	}
	if len(req.Segments) > MixMaxSegments {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncSegmentLimit,
			Message: fmt.Sprintf("at most %d segments are supported", MixMaxSegments),
		}
	}

	for index, segment := range req.Segments {
		if validationErr := validateMixSegment(index, segment, duration); validationErr != nil {
			return validationErr
		}
	}

	return validateSegmentsDoNotOverlap(req.Segments)
}

func validateMixSegment(index int, segment MixSegment, duration float64) *VideoError {
	number := index + 1

	if !isFinite(segment.VideoStart) || !isFinite(segment.VideoEnd) {
		return &VideoError{Code: ErrorCodeVideoAudioSyncSegmentInvalid, Message: fmt.Sprintf("segment %d must have finite videoStart and videoEnd times", number)}
	}
	if segment.VideoStart < 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncSegmentInvalid, Message: fmt.Sprintf("segment %d starts at %.3f s; videoStart must be >= 0", number, segment.VideoStart)}
	}
	if segment.VideoEnd <= segment.VideoStart {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncSegmentInvalid,
			Message: fmt.Sprintf("segment %d ends at %.3f s; videoEnd must be greater than videoStart (%.3f s)", number, segment.VideoEnd, segment.VideoStart),
		}
	}
	if segment.VideoEnd > duration+1e-6 {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncSegmentInvalid,
			Message: fmt.Sprintf("segment %d ends at %.3f s, after the video duration (%.3f s)", number, segment.VideoEnd, duration),
		}
	}
	if strings.TrimSpace(segment.SourcePath) == "" {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("segment %d requires a sourcePath", number)}
	}
	if _, err := os.Stat(strings.TrimSpace(segment.SourcePath)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoAudioSyncSourceNotFound, Message: fmt.Sprintf("segment %d source does not exist: %s", number, strings.TrimSpace(segment.SourcePath)), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("segment %d source is not accessible: %s", number, strings.TrimSpace(segment.SourcePath)), Cause: err}
	}
	if !isFinite(segment.SourceStart) || segment.SourceStart < 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncSourceStartInvalid, Message: fmt.Sprintf("segment %d sourceStart must be >= 0", number)}
	}
	if !isFinite(segment.FadeSeconds) || segment.FadeSeconds < 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncSegmentInvalid, Message: fmt.Sprintf("segment %d fadeSeconds must be >= 0", number)}
	}
	if !isFinite(segment.GainDB) || segment.GainDB < MixMinGainDB || segment.GainDB > MixMaxGainDB {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncGainInvalid,
			Message: fmt.Sprintf("segment %d gainDb must be between %.0f and %.0f", number, MixMinGainDB, MixMaxGainDB),
		}
	}

	return nil
}

// validateSegmentsDoNotOverlap sorts a copy so validation never mutates the
// request and reports the two overlapping blocks by their one-based index.
func validateSegmentsDoNotOverlap(segments []MixSegment) *VideoError {
	order := make([]int, len(segments))
	for index := range order {
		order[index] = index
	}
	sortIndicesByVideoStart(order, segments)

	for position := 1; position < len(order); position++ {
		previous := segments[order[position-1]]
		current := segments[order[position]]
		if current.VideoStart < previous.VideoEnd-1e-6 {
			return &VideoError{
				Code: ErrorCodeVideoAudioSyncSegmentOverlap,
				Message: fmt.Sprintf(
					"segments %d and %d overlap: segment %d starts at %.3f s before segment %d ends at %.3f s",
					order[position-1]+1, order[position]+1, order[position]+1, current.VideoStart, order[position-1]+1, previous.VideoEnd,
				),
			}
		}
	}

	return nil
}

func sortIndicesByVideoStart(order []int, segments []MixSegment) {
	for index := 1; index < len(order); index++ {
		value := order[index]
		position := index
		for position > 0 && segments[order[position-1]].VideoStart > segments[value].VideoStart {
			order[position] = order[position-1]
			position--
		}
		order[position] = value
	}
}

func validateMixGain(value float64, name string) *VideoError {
	if !isFinite(value) || value < MixMinGainDB || value > MixMaxGainDB {
		return &VideoError{
			Code:    ErrorCodeVideoAudioSyncGainInvalid,
			Message: fmt.Sprintf("%s must be between %.0f and %.0f dB", name, MixMinGainDB, MixMaxGainDB),
		}
	}

	return nil
}

func effectiveMixBitrate(bitrate int) int {
	if bitrate <= 0 {
		return MixDefaultBitrateKbps
	}

	return bitrate
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// mixSegments resolves the actual placements used by the graph. Replace mode
// derives the single segment from the offset: a positive offset becomes an
// adelay, a negative offset becomes a head trim.
func mixSegments(req MixRequest) []MixSegment {
	if normalizeMixMode(req.Mode) == MixModeReplace {
		delay := math.Max(req.OffsetSeconds, 0)
		skip := math.Max(-req.OffsetSeconds, 0)
		return []MixSegment{{
			VideoStart:  delay,
			VideoEnd:    req.DurationSeconds,
			SourceStart: req.SourceStartSeconds + skip,
			SourcePath:  strings.TrimSpace(req.SourcePath),
			GainDB:      req.SourceGainDB,
		}}
	}

	segments := make([]MixSegment, 0, len(req.Segments))
	for _, segment := range req.Segments {
		segment.SourcePath = strings.TrimSpace(segment.SourcePath)
		segments = append(segments, segment)
	}

	return segments
}

// mixSourcePaths returns every distinct source file in first-use order. One
// input per file is what lets several segments share a single decode through
// asplit.
func mixSourcePaths(req MixRequest) []string {
	seen := make(map[string]struct{})
	paths := make([]string, 0, len(req.Segments)+1)

	add := func(path string) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			return
		}
		key := fsx.NormalizePathKey(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, trimmed)
	}

	if normalizeMixMode(req.Mode) == MixModeReplace {
		add(req.SourcePath)
		return paths
	}
	for _, segment := range mixSegments(req) {
		add(segment.SourcePath)
	}

	return paths
}

// mixSegmentNodes builds the per-segment filter chain and returns the labels
// that feed amix. Shared source files are split once with asplit=N.
func mixSegmentNodes(req MixRequest) ([]string, []string) {
	segments := mixSegments(req)
	sourcePaths := mixSourcePaths(req)
	sourceIndex := make(map[string]int, len(sourcePaths))
	useCount := make(map[string]int, len(sourcePaths))
	for index, path := range sourcePaths {
		sourceIndex[fsx.NormalizePathKey(path)] = index + 1
	}
	for _, segment := range segments {
		useCount[fsx.NormalizePathKey(segment.SourcePath)]++
	}

	parts := make([]string, 0, len(segments)*3+len(sourcePaths))
	branchLabels := make(map[string][]string, len(sourcePaths))
	branchUse := make(map[string]int, len(sourcePaths))
	for _, path := range sourcePaths {
		key := fsx.NormalizePathKey(path)
		inputIndex := sourceIndex[key]
		count := useCount[key]
		if count <= 1 {
			branchLabels[key] = []string{fmt.Sprintf("[%d:a]", inputIndex)}
			continue
		}

		outputs := make([]string, count)
		for branch := 0; branch < count; branch++ {
			outputs[branch] = fmt.Sprintf("[sp%d_%d]", inputIndex, branch)
		}
		parts = append(parts, fmt.Sprintf("[%d:a]asplit=%d%s", inputIndex, count, strings.Join(outputs, "")))
		branchLabels[key] = outputs
	}

	labels := make([]string, 0, len(segments))
	for index, segment := range segments {
		key := fsx.NormalizePathKey(segment.SourcePath)
		branch := branchUse[key]
		branchUse[key] = branch + 1
		inputLabel := branchLabels[key][branch]

		duration := segment.VideoEnd - segment.VideoStart
		sourceStart := math.Max(segment.SourceStart, 0)
		sourceEnd := sourceStart + math.Max(duration, 0)
		chain := []string{
			fmt.Sprintf("atrim=start=%s:end=%s", formatMixNumber(sourceStart), formatMixNumber(sourceEnd)),
			"asetpts=PTS-STARTPTS",
			fmt.Sprintf("aresample=%d", mixSampleRate),
			// Force one channel layout before amix. Without it amix
			// negotiates down to mono when a mono recorder meets a stereo
			// camera track, which would silently discard a channel.
			"aformat=channel_layouts=stereo",
			fmt.Sprintf("volume=%sdB", formatMixNumber(segment.GainDB)),
		}
		if fade := mixFadeSeconds(segment, duration); fade > 0 {
			chain = append(chain,
				fmt.Sprintf("afade=t=in:st=0:d=%s", formatMixNumber(fade)),
				fmt.Sprintf("afade=t=out:st=%s:d=%s", formatMixNumber(math.Max(duration-fade, 0)), formatMixNumber(fade)),
			)
		}

		gainLabel := fmt.Sprintf("[g%d]", index)
		parts = append(parts, fmt.Sprintf("%s%s%s", inputLabel, strings.Join(chain, ","), gainLabel))

		delayLabel := gainLabel
		if delayMilliseconds := int(math.Round(segment.VideoStart * 1000)); delayMilliseconds > 0 {
			delayLabel = fmt.Sprintf("[d%d]", index)
			parts = append(parts, fmt.Sprintf("%sadelay=%d|%d%s", gainLabel, delayMilliseconds, delayMilliseconds, delayLabel))
		}
		labels = append(labels, delayLabel)
	}

	return parts, labels
}

// mixFadeSeconds clamps the requested fade to half the segment so the in and
// out fades can never cross. A zero or negative request disables the fades.
func mixFadeSeconds(segment MixSegment, duration float64) float64 {
	if segment.FadeSeconds <= 0 || duration <= 0 {
		return 0
	}
	fade := segment.FadeSeconds
	if half := duration / 2; fade > half {
		fade = half
	}

	return fade
}

// buildMixFilterGraph assembles the full graph. loudnormFilter, when set, is
// inserted between the mix and the tail padding so both loudnorm passes
// measure and process exactly the same signal.
func buildMixFilterGraph(req MixRequest, loudnormFilter string) string {
	parts, audioLabels := mixSegmentNodes(req)

	originalAudio := normalizeMixOriginalAudio(req.OriginalAudio)
	if originalAudio == MixOriginalKeep || originalAudio == MixOriginalDuck {
		gain := req.OriginalGainDB
		if originalAudio == MixOriginalKeep {
			gain = 0
		}
		parts = append(parts, fmt.Sprintf("[0:a]aresample=%d,aformat=channel_layouts=stereo,volume=%sdB[orig]", mixSampleRate, formatMixNumber(gain)))
		audioLabels = append([]string{"[orig]"}, audioLabels...)
	}

	parts = append(parts, fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=longest[mixed]", strings.Join(audioLabels, ""), len(audioLabels)))

	tail := "[mixed]"
	if strings.TrimSpace(loudnormFilter) != "" {
		tail += strings.TrimSpace(loudnormFilter) + ","
	}
	tail += fmt.Sprintf("apad,atrim=0:%s,aresample=async=1:first_pts=0[a]", formatMixNumber(req.DurationSeconds))
	parts = append(parts, tail)

	return strings.Join(parts, ";")
}

// buildMixPreviewFilterGraph trims the same mix to the requested span and
// re-encodes the video at preview size. Video and audio are cut from the same
// timeline so they stay aligned.
func buildMixPreviewFilterGraph(req MixRequest, startSeconds, endSeconds float64) string {
	parts, audioLabels := mixSegmentNodes(req)

	originalAudio := normalizeMixOriginalAudio(req.OriginalAudio)
	if originalAudio == MixOriginalKeep || originalAudio == MixOriginalDuck {
		gain := req.OriginalGainDB
		if originalAudio == MixOriginalKeep {
			gain = 0
		}
		parts = append(parts, fmt.Sprintf("[0:a]aresample=%d,aformat=channel_layouts=stereo,volume=%sdB[orig]", mixSampleRate, formatMixNumber(gain)))
		audioLabels = append([]string{"[orig]"}, audioLabels...)
	}

	parts = append(parts, fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=longest[mixed]", strings.Join(audioLabels, ""), len(audioLabels)))
	parts = append(parts, fmt.Sprintf(
		"[mixed]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=async=1:first_pts=0[pa]",
		formatMixNumber(startSeconds), formatMixNumber(endSeconds),
	))
	parts = append(parts, fmt.Sprintf(
		"[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS,scale=%d:-2[pv]",
		formatMixNumber(startSeconds), formatMixNumber(endSeconds), mixPreviewVideoWidth,
	))

	return strings.Join(parts, ";")
}

// formatMixNumber renders a float without trailing zeros so the filter graph is
// stable and readable: 2, 0.015, -18.5.
func formatMixNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// BuildMixArgs builds the single-pass ffmpeg arguments for the base graph.
// Normalization is a two-pass operation layered by ExecuteMix; this function
// never includes loudnorm so its output stays a pure description of the plan.
func BuildMixArgs(req MixRequest) ([]string, *VideoError) {
	return buildMixArgsForPass(req, "")
}

func buildMixArgsForPass(req MixRequest, loudnormFilter string) ([]string, *VideoError) {
	if validationErr := ValidateMix(req); validationErr != nil {
		return nil, validationErr
	}

	args := withFFmpegGlobalArgs([]string{"-y", "-i", strings.TrimSpace(req.VideoPath)})
	for _, sourcePath := range mixSourcePaths(req) {
		args = append(args, "-i", sourcePath)
	}

	args = append(args,
		"-filter_complex", buildMixFilterGraph(req, loudnormFilter),
		"-map", "0:v",
		"-map", "[a]",
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", effectiveMixBitrate(req.AudioBitrateKbps)),
		"-ar", fmt.Sprintf("%d", mixSampleRate),
		"-fps_mode", "passthrough",
		"-movflags", "+faststart",
		strings.TrimSpace(req.OutputPath),
	)

	return args, nil
}

// buildMixMeasureArgs builds the first loudnorm pass: the same graph with the
// measurement filter, mapped to the null muxer. The log level must stay at info
// because loudnorm prints its JSON summary there.
func buildMixMeasureArgs(req MixRequest) []string {
	args := []string{
		"-hide_banner",
		"-nostats",
		"-nostdin",
		"-loglevel", "info",
		"-y",
		"-i", strings.TrimSpace(req.VideoPath),
	}
	for _, sourcePath := range mixSourcePaths(req) {
		args = append(args, "-i", sourcePath)
	}

	args = append(args,
		"-filter_complex", buildMixFilterGraph(req, mixLoudnormMeasureFilter()),
		"-map", "[a]",
		"-f", "null",
		"-",
	)

	return args
}

func mixLoudnormMeasureFilter() string {
	return fmt.Sprintf("loudnorm=I=%.1f:TP=%.1f:LRA=11:print_format=json", mixLoudnormTarget, mixLoudnormTruePeak)
}

func mixLoudnormApplyFilter(report mixLoudnormReport) string {
	return fmt.Sprintf(
		"loudnorm=I=%.1f:TP=%.1f:LRA=11:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true:print_format=json",
		mixLoudnormTarget,
		mixLoudnormTruePeak,
		mixLoudnormValue(report.InputI, "-70"),
		mixLoudnormValue(report.InputTP, "0"),
		mixLoudnormValue(report.InputLRA, "0"),
		mixLoudnormValue(report.InputThresh, "-70"),
		mixLoudnormValue(report.TargetOffset, "0"),
	)
}

// ExecuteMix renders the plan to a temp sibling and publishes it atomically.
// The video stream is copied; only audio is rebuilt. Normalization runs the
// same two-pass EBU R128 path the audio domain uses, locally so the video
// package does not import the audio engine.
func ExecuteMix(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req MixRequest, onProgress func(percent float64, message string)) *VideoError {
	if probe == nil {
		probe = NewFFmpegRuntimeProbe()
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		var vErr *VideoError
		if errors.As(runtimeErr, &vErr) {
			if vErr.Code == ErrorCodeRuntimeFFmpegNotFound || vErr.Code == ErrorCodeRuntimeFFprobeNotFound {
				return &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable", Cause: vErr}
			}
			return vErr
		}
		return &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: ErrFFmpegNotFound.Error(), Cause: runtimeErr}
	}

	if validationErr := ValidateMix(req); validationErr != nil {
		return validationErr
	}

	tempPath, tempErr := tempOutputPath(req.OutputPath)
	if tempErr != nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputWriteFailed, Message: "audio sync output could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			removeTempOutput(tempPath)
		}
	}()

	tempRequest := req
	tempRequest.OutputPath = tempPath

	if req.Normalize {
		if normalizeErr := executeNormalizedMix(ctx, runner, tempRequest, onProgress); normalizeErr != nil {
			return normalizeErr
		}
	} else {
		args, argsErr := BuildMixArgs(tempRequest)
		if argsErr != nil {
			return argsErr
		}
		if runErr := runFFmpegWithProgress(ctx, runner, args, req.DurationSeconds, onProgress); runErr != nil {
			return mapAudioSyncExecutionError(runErr)
		}
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return mapAudioSyncExecutionError(ctxErr)
	}
	if publishErr := publishOutput(tempPath, req.OutputPath); publishErr != nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputWriteFailed, Message: "audio sync output could not be published", Cause: publishErr}
	}
	published = true

	if onProgress != nil {
		onProgress(100, "Audio sync completed")
	}

	return nil
}

// executeNormalizedMix runs the local two-pass loudnorm path. The measurement
// pass decodes the whole mix; the apply pass reuses the measured values with
// linear mode so the dynamics are preserved where possible.
func executeNormalizedMix(ctx context.Context, runner CommandRunner, req MixRequest, onProgress func(percent float64, message string)) *VideoError {
	capture, ok := runner.(CaptureCommandRunner)
	if !ok {
		return &VideoError{Code: ErrorCodeVideoAudioSyncLoudnormFailed, Message: "the ffmpeg runner does not support loudness capture"}
	}

	measureProgress := mapMixProgress(onProgress, 0, 50, "Measuring loudness…")
	_, measureStderr, measureErr := capture.RunCapture(ctx, "ffmpeg", buildMixMeasureArgs(req), req.DurationSeconds, measureProgress)
	if measureErr != nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncLoudnormFailed, Message: "ffmpeg could not measure the mixed audio loudness", Cause: measureErr}
	}
	report, parsed := parseMixLoudnormReport(measureStderr)
	if !parsed {
		return &VideoError{Code: ErrorCodeVideoAudioSyncLoudnormFailed, Message: "ffmpeg did not return a loudness measurement"}
	}

	args, argsErr := buildMixArgsForPass(req, mixLoudnormApplyFilter(report))
	if argsErr != nil {
		return argsErr
	}
	applyProgress := mapMixProgress(onProgress, 50, 100, "Applying loudness normalization…")
	if _, _, applyErr := capture.RunCapture(ctx, "ffmpeg", args, req.DurationSeconds, applyProgress); applyErr != nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncLoudnormFailed, Message: "ffmpeg could not apply loudness normalization", Cause: applyErr}
	}

	return nil
}

// RenderPreview re-encodes a short span of the plan around the playhead at
// preview size. It publishes to outputPath and is meant for the media endpoint
// the editor streams from.
func RenderPreview(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req MixRequest, startSeconds, spanSeconds float64, outputPath string) *VideoError {
	if probe == nil {
		probe = NewFFmpegRuntimeProbe()
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}
	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		var vErr *VideoError
		if errors.As(runtimeErr, &vErr) {
			return vErr
		}
		return &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: ErrFFmpegNotFound.Error(), Cause: runtimeErr}
	}

	if !isFinite(startSeconds) || startSeconds < 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: "startSeconds must be >= 0"}
	}
	if !isFinite(spanSeconds) || spanSeconds <= 0 {
		return &VideoError{Code: ErrorCodeVideoAudioSyncValidation, Message: "spanSeconds must be a positive number"}
	}

	endSeconds := startSeconds + spanSeconds
	if req.DurationSeconds > 0 && endSeconds > req.DurationSeconds {
		endSeconds = req.DurationSeconds
	}
	if endSeconds <= startSeconds {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOffsetInvalid, Message: "the preview span starts after the video ends"}
	}

	previewRequest := req
	previewRequest.OutputPath = strings.TrimSpace(outputPath)
	previewRequest.Normalize = false
	if validationErr := ValidateMix(previewRequest); validationErr != nil {
		return validationErr
	}

	tempPath, tempErr := tempOutputPath(previewRequest.OutputPath)
	if tempErr != nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputWriteFailed, Message: "audio sync preview could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			removeTempOutput(tempPath)
		}
	}()

	args := withFFmpegGlobalArgs([]string{"-y", "-i", strings.TrimSpace(previewRequest.VideoPath)})
	for _, sourcePath := range mixSourcePaths(previewRequest) {
		args = append(args, "-i", sourcePath)
	}
	args = append(args,
		"-filter_complex", buildMixPreviewFilterGraph(previewRequest, startSeconds, endSeconds),
		"-map", "[pv]",
		"-map", "[pa]",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-crf", strconv.Itoa(mixPreviewCRF),
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", mixPreviewAudioKbps),
		"-ar", fmt.Sprintf("%d", mixSampleRate),
		"-movflags", "+faststart",
		tempPath,
	)

	if runErr := runFFmpegWithProgress(ctx, runner, args, spanSeconds, nil); runErr != nil {
		return mapAudioSyncExecutionError(runErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return mapAudioSyncExecutionError(ctxErr)
	}
	if publishErr := publishOutput(tempPath, previewRequest.OutputPath); publishErr != nil {
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputWriteFailed, Message: "audio sync preview could not be published", Cause: publishErr}
	}
	published = true

	return nil
}

// mapMixProgress rescales one pass's 0..100 into a slice of the overall bar.
func mapMixProgress(onProgress func(float64, string), start, end float64, message string) func(float64, string) {
	if onProgress == nil {
		return nil
	}

	return func(percent float64, _ string) {
		scaled := start + (end-start)*clampPercent(percent)/100
		onProgress(scaled, message)
	}
}

func mapAudioSyncExecutionError(err error) *VideoError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return &VideoError{Code: ErrorCodeVideoAudioSyncCancelled, Message: "audio sync canceled", Cause: err}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "not found"):
		return &VideoError{Code: ErrorCodeVideoAudioSyncInputOpenFailed, Message: "ffmpeg could not read one of the input files", Cause: err}
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "read-only file system") || strings.Contains(lower, "no space left"):
		return &VideoError{Code: ErrorCodeVideoAudioSyncOutputWriteFailed, Message: "ffmpeg could not write the output file", Cause: err}
	case strings.Contains(lower, "unknown encoder") || strings.Contains(lower, "encoder"):
		return &VideoError{Code: ErrorCodeVideoAudioSyncCodecUnavailable, Message: "required audio codec is unavailable in the current ffmpeg build", Cause: err}
	default:
		return &VideoError{Code: ErrorCodeVideoAudioSyncExecutionFailed, Message: "ffmpeg audio sync execution failed", Cause: err}
	}
}

// MixOutputExtension returns the container that can hold the probed video
// codec: mp4 for the mainstream codecs, mkv for everything else.
func MixOutputExtension(videoCodec string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(videoCodec)) {
	case "h264", "h265", "hevc", "av1", "vp9":
		return "mp4", true
	default:
		return "mkv", false
	}
}

// mixLoudnormReport mirrors the JSON summary printed by the loudnorm filter.
type mixLoudnormReport struct {
	InputI       string `json:"input_i"`
	InputTP      string `json:"input_tp"`
	InputLRA     string `json:"input_lra"`
	InputThresh  string `json:"input_thresh"`
	TargetOffset string `json:"target_offset"`
}

// parseMixLoudnormReport extracts the last JSON object from a loudnorm log. It
// is a small local copy of the audio domain's parser so the video engine does
// not import another domain package.
func parseMixLoudnormReport(output string) (mixLoudnormReport, bool) {
	start := strings.LastIndex(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return mixLoudnormReport{}, false
	}

	var report mixLoudnormReport
	if err := json.Unmarshal([]byte(output[start:end+1]), &report); err != nil {
		return mixLoudnormReport{}, false
	}
	if strings.TrimSpace(report.InputI) == "" {
		return mixLoudnormReport{}, false
	}

	return report, true
}

func mixLoudnormValue(raw, fallback string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}

	return trimmed
}
