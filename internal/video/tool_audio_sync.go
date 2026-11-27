package video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const (
	ToolIDVideoAudioSync = "tool.video.audio_sync"

	videoAudioSyncOutputSuffix = "_synced"

	audioSyncDefaultOriginalGainDB = -18.0
	audioSyncDefaultSourceGainDB   = 0.0
)

// probeVideoMetadata is a package variable so plan tests can simulate a video
// and its source without ffprobe.
var probeVideoMetadata = inspect.ProbeVideoMetadata

type AudioSyncTool struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
}

func NewAudioSyncTool() *AudioSyncTool {
	return &AudioSyncTool{}
}

func NewAudioSyncToolWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *AudioSyncTool {
	return &AudioSyncTool{probe: probe, runner: runner}
}

func (t *AudioSyncTool) ID() string {
	return ToolIDVideoAudioSync
}

func (t *AudioSyncTool) Capability() string {
	return ToolIDVideoAudioSync
}

func (t *AudioSyncTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Audio in Video",
		Description:      "Replace or repair a video's audio with a separately recorded source while the video stream is copied untouched",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"mp4", "mov", "mkv", "webm", "avi", "mp3", "m4a", "wav", "aac", "flac", "ogg", "opus"},
		OutputExtensions: []string{"mp4", "mkv"},
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		ResourceClass:    "external",
		MaxWorkers:       1,
		Tags:             []string{"video", "audio", "sync", "align", "ffmpeg"},
	}
}

func (t *AudioSyncTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeState{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *AudioSyncTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves the output container and the exact audio-sync request without
// writing anything. It only runs ffprobe metadata reads: one for the video and
// one per distinct source file.
func (t *AudioSyncTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	if strings.TrimSpace(req.Mode) != "single" {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: "mode must be single"})
	}
	if len(req.InputPaths) != 1 {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: "exactly 1 input video is required"})
	}

	inputPath := strings.TrimSpace(req.InputPaths[0])
	if inputPath == "" {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: "inputPaths must not contain empty values"})
	}
	if _, err := os.Stat(inputPath); err != nil {
		if os.IsNotExist(err) {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("video does not exist: %s", inputPath)})
		}
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("video is not accessible: %s", inputPath)})
	}

	metadata, metadataOK := probeVideoMetadata(ctx, inputPath)
	if !metadataOK {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncDurationUnavailable, Message: "the video metadata could not be read"})
	}
	if metadata.VideoCodec == "" {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncInputNoVideo, Message: "the input does not contain a video stream"})
	}
	if metadata.DurationSeconds <= 0 {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncDurationUnavailable, Message: "the video duration could not be read"})
	}

	mixRequest, optionsErr := t.mixRequestFromOptions(ctx, req.Options, inputPath, metadata)
	if optionsErr != nil {
		return planFailure(plan, optionsErr)
	}

	container, mp4Compatible := engine.MixOutputExtension(metadata.VideoCodec)

	explicitOutput := optionString(req.Options, "outputPath")
	var planned models.PlannedOutput
	if explicitOutput != "" {
		planned = models.PlannedOutput{
			InputPath:  inputPath,
			OutputPath: filepath.Clean(strings.TrimSpace(explicitOutput)),
			OutputKind: videoOutputKindFile,
		}
		if _, err := os.Stat(planned.OutputPath); err == nil {
			planned.Exists = true
		}
	} else {
		used := reserveInputPaths(append([]string{inputPath}, mixRequestSourcePaths(mixRequest)...))
		planned = planAutoVideoOutput(inputPath, req.OutputDir, videoAudioSyncOutputSuffix, container, used)
	}

	mixRequest.OutputPath = planned.OutputPath
	if validationErr := engine.ValidateMix(mixRequest); validationErr != nil {
		plan.Outputs = []models.PlannedOutput{planned}
		return planFailure(plan, mapVideoError(validationErr))
	}

	plan.Success = true
	plan.Message = "Ready to sync the audio of 1 video."
	plan.Outputs = []models.PlannedOutput{planned}
	plan.Warnings = append(plan.Warnings, planned.Warnings...)
	if !mp4Compatible {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"%s video cannot be stored in MP4. Saving as Matroska (.mkv) instead.",
			metadata.VideoCodec,
		))
	}

	effectiveOptions := map[string]any{
		"mode":             mixRequest.Mode,
		"originalAudio":    mixRequest.OriginalAudio,
		"originalGainDb":   mixRequest.OriginalGainDB,
		"sourceGainDb":     mixRequest.SourceGainDB,
		"audioBitrateKbps": mixRequest.AudioBitrateKbps,
		"normalize":        mixRequest.Normalize,
		"durationSeconds":  mixRequest.DurationSeconds,
		"frameRate":        metadata.FrameRate,
		"container":        container,
	}
	if mixRequest.Mode == engine.MixModeSections {
		effectiveOptions["segments"] = audioSyncEffectiveSegments(mixRequest.Segments)
	} else {
		effectiveOptions["sourcePath"] = mixRequest.SourcePath
		effectiveOptions["offsetSeconds"] = mixRequest.OffsetSeconds
		effectiveOptions["sourceStartSeconds"] = mixRequest.SourceStartSeconds
	}
	plan.EffectiveOptions = effectiveOptions

	return plan, nil
}

// mixRequestFromOptions parses and validates every option, then probes each
// distinct source for an audio stream.
func (t *AudioSyncTool) mixRequestFromOptions(ctx context.Context, options map[string]any, inputPath string, metadata inspect.VideoMetadata) (engine.MixRequest, *models.JobError) {
	validationCode := engine.ErrorCodeVideoAudioSyncValidation

	request := engine.MixRequest{
		VideoPath:       inputPath,
		DurationSeconds: metadata.DurationSeconds,
	}

	mode := strings.ToLower(optionString(options, "mode"))
	if mode == "" {
		mode = engine.MixModeReplace
	}
	switch mode {
	case engine.MixModeReplace, engine.MixModeSections:
	default:
		return request, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncModeInvalid, Message: "options.mode must be replace or sections"}
	}
	request.Mode = mode

	originalAudio := strings.ToLower(optionString(options, "originalAudio"))
	if originalAudio == "" {
		originalAudio = engine.MixOriginalRemove
	}
	switch originalAudio {
	case engine.MixOriginalRemove, engine.MixOriginalKeep, engine.MixOriginalDuck:
	default:
		return request, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncOriginalAudioInvalid, Message: "options.originalAudio must be remove, keep, or duck"}
	}
	if originalAudio != engine.MixOriginalRemove && metadata.AudioCodec == "" {
		return request, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncOriginalAudioMissing, Message: "the video has no audio stream to keep or duck"}
	}
	request.OriginalAudio = originalAudio

	originalGainDB, originalGainSupplied, originalGainErr := videoOptionFloat(options, "originalGainDb", validationCode)
	if originalGainErr != nil {
		return request, originalGainErr
	}
	if originalGainSupplied {
		request.OriginalGainDB = originalGainDB
	} else {
		request.OriginalGainDB = audioSyncDefaultOriginalGainDB
	}

	sourceGainDB, sourceGainSupplied, sourceGainErr := videoOptionFloat(options, "sourceGainDb", validationCode)
	if sourceGainErr != nil {
		return request, sourceGainErr
	}
	if sourceGainSupplied {
		request.SourceGainDB = sourceGainDB
	} else {
		request.SourceGainDB = audioSyncDefaultSourceGainDB
	}

	bitrate, bitrateErr := videoOptionInt(options, "audioBitrateKbps", validationCode)
	if bitrateErr != nil {
		return request, bitrateErr
	}
	if bitrate <= 0 {
		bitrate = engine.MixDefaultBitrateKbps
	}
	request.AudioBitrateKbps = bitrate

	normalize, normalizeErr := videoOptionBool(options, "normalize", validationCode)
	if normalizeErr != nil {
		return request, normalizeErr
	}
	request.Normalize = normalize

	switch mode {
	case engine.MixModeReplace:
		sourcePath := strings.TrimSpace(optionString(options, "sourcePath"))
		if sourcePath == "" {
			return request, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: "options.sourcePath is required in replace mode"}
		}

		offsetSeconds, _, offsetErr := videoOptionFloat(options, "offsetSeconds", validationCode)
		if offsetErr != nil {
			return request, offsetErr
		}
		sourceStartSeconds, _, sourceStartErr := videoOptionFloat(options, "sourceStartSeconds", validationCode)
		if sourceStartErr != nil {
			return request, sourceStartErr
		}

		request.SourcePath = sourcePath
		request.OffsetSeconds = offsetSeconds
		request.SourceStartSeconds = sourceStartSeconds
	case engine.MixModeSections:
		segments, segmentsErr := parseAudioSyncSegments(options)
		if segmentsErr != nil {
			return request, segmentsErr
		}
		request.Segments = segments
	}

	sources := mixRequestSourcePaths(request)
	if sourcesErr := requireAudioSources(ctx, sources); sourcesErr != nil {
		return request, sourcesErr
	}

	return request, nil
}

// parseAudioSyncSegments parses the nested segment objects. Segments are
// sorted by videoStart before validation so the graph and the overlap check
// share one order.
func parseAudioSyncSegments(options map[string]any) ([]engine.MixSegment, *models.JobError) {
	raw := options["segments"]
	if raw == nil {
		return nil, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncSegmentsRequired, Message: "options.segments is required in sections mode"}
	}

	items, ok := raw.([]any)
	if !ok {
		if typed, typedOK := raw.([]map[string]any); typedOK {
			items = make([]any, len(typed))
			for index, item := range typed {
				items[index] = item
			}
		} else {
			return nil, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncSegmentsRequired, Message: "options.segments must be a list of segment objects"}
		}
	}
	if len(items) == 0 {
		return nil, &models.JobError{Code: engine.ErrorCodeVideoAudioSyncSegmentsRequired, Message: "options.segments must contain at least 1 segment"}
	}
	if len(items) > engine.MixMaxSegments {
		return nil, &models.JobError{
			Code:    engine.ErrorCodeVideoAudioSyncSegmentLimit,
			Message: fmt.Sprintf("options.segments supports at most %d segments", engine.MixMaxSegments),
		}
	}

	validationCode := engine.ErrorCodeVideoAudioSyncSegmentInvalid
	segments := make([]engine.MixSegment, 0, len(items))
	for index, item := range items {
		number := index + 1
		object, objectOK := item.(map[string]any)
		if !objectOK {
			return nil, &models.JobError{Code: validationCode, Message: fmt.Sprintf("segment %d must be an object", number)}
		}

		videoStart, startSupplied, startErr := videoOptionFloat(object, "videoStart", validationCode)
		if startErr != nil {
			return nil, startErr
		}
		if !startSupplied {
			return nil, &models.JobError{Code: validationCode, Message: fmt.Sprintf("segment %d requires videoStart", number)}
		}
		videoEnd, endSupplied, endErr := videoOptionFloat(object, "videoEnd", validationCode)
		if endErr != nil {
			return nil, endErr
		}
		if !endSupplied {
			return nil, &models.JobError{Code: validationCode, Message: fmt.Sprintf("segment %d requires videoEnd", number)}
		}

		sourcePath := strings.TrimSpace(optionString(object, "sourcePath"))
		if sourcePath == "" {
			return nil, &models.JobError{Code: validationCode, Message: fmt.Sprintf("segment %d requires sourcePath", number)}
		}

		sourceStart, _, sourceStartErr := videoOptionFloat(object, "sourceStart", validationCode)
		if sourceStartErr != nil {
			return nil, sourceStartErr
		}

		gainDB, gainSupplied, gainErr := videoOptionFloat(object, "gainDb", validationCode)
		if gainErr != nil {
			return nil, gainErr
		}
		if !gainSupplied {
			gainDB, _, gainErr = videoOptionFloat(object, "sourceGainDb", validationCode)
			if gainErr != nil {
				return nil, gainErr
			}
		}

		fadeMs := engine.MixDefaultFadeSeconds * 1000
		if suppliedFade, fadeSupplied, fadeErr := videoOptionFloat(object, "fadeMs", validationCode); fadeErr != nil {
			return nil, fadeErr
		} else if fadeSupplied {
			fadeMs = suppliedFade
		}
		if fadeMs < 0 {
			return nil, &models.JobError{Code: validationCode, Message: fmt.Sprintf("segment %d fadeMs must be >= 0", number)}
		}

		segments = append(segments, engine.MixSegment{
			VideoStart:  videoStart,
			VideoEnd:    videoEnd,
			SourceStart: sourceStart,
			SourcePath:  sourcePath,
			GainDB:      gainDB,
			FadeSeconds: fadeMs / 1000,
		})
	}

	sort.SliceStable(segments, func(left, right int) bool {
		return segments[left].VideoStart < segments[right].VideoStart
	})

	return segments, nil
}

// requireAudioSources probes each distinct source once and rejects anything
// without an audio stream.
func requireAudioSources(ctx context.Context, sources []string) *models.JobError {
	for _, sourcePath := range sources {
		if _, err := os.Stat(sourcePath); err != nil {
			if os.IsNotExist(err) {
				return &models.JobError{Code: engine.ErrorCodeVideoAudioSyncSourceNotFound, Message: fmt.Sprintf("source does not exist: %s", sourcePath)}
			}
			return &models.JobError{Code: engine.ErrorCodeVideoAudioSyncValidation, Message: fmt.Sprintf("source is not accessible: %s", sourcePath)}
		}

		metadata, ok := probeVideoMetadata(ctx, sourcePath)
		if !ok || metadata.AudioCodec == "" {
			return &models.JobError{Code: engine.ErrorCodeVideoAudioSyncSourceNoAudio, Message: fmt.Sprintf("source has no audio stream: %s", sourcePath)}
		}
	}

	return nil
}

// mixRequestSourcePaths returns the distinct source files of a parsed request.
func mixRequestSourcePaths(request engine.MixRequest) []string {
	seen := make(map[string]struct{})
	paths := make([]string, 0, len(request.Segments)+1)

	add := func(path string) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		paths = append(paths, trimmed)
	}

	if request.Mode == engine.MixModeSections {
		for _, segment := range request.Segments {
			add(segment.SourcePath)
		}
		return paths
	}
	add(request.SourcePath)

	return paths
}

func (t *AudioSyncTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *AudioSyncTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = &models.JobError{Code: "EXECUTION_ERROR", Message: "audio sync execution returned no result"}
	}

	return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), jobErr), jobErr
}

// ExecutePlan renders exactly the outputs resolved by Plan. The plan carries
// the fully resolved mix request, including the segments, the duration and the
// frame rate the editor uses to snap.
func (t *AudioSyncTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	if len(plan.Outputs) == 0 {
		jobErr := &models.JobError{Code: "EXECUTION_ERROR", Message: "audio sync execution returned no planned output"}
		return nil, jobErr
	}

	items := make([]models.JobResultItem, 0, len(plan.Outputs))
	var firstErr *models.JobError

	var mixProgress func(percent float64, message string)
	if onProgress != nil {
		mixProgress = func(percent float64, _ string) {
			current := jobProgressPercent(percent)
			message := fmt.Sprintf("Syncing audio… %d%%", current)
			if percent >= 100 {
				message = "Audio sync completed"
			}
			onProgress(models.JobProgress{
				Current: current,
				Total:   jobProgressTotal,
				Stage:   "video.audio_sync",
				Message: message,
			})
		}
	}

	for _, planned := range plan.Outputs {
		mixRequest := audioSyncRequestForOutput(planned, plan, req)

		select {
		case <-ctx.Done():
			jobErr := &models.JobError{Code: engine.ErrorCodeVideoAudioSyncCancelled, Message: "audio sync canceled"}
			items = append(items, videoPlanFailureItem(mixRequest.VideoPath, mixRequest.OutputPath, jobErr))
			if firstErr == nil {
				firstErr = jobErr
			}
			return items, firstErr
		default:
		}

		if mixErr := engine.ExecuteMix(ctx, t.probe, t.runner, mixRequest, mixProgress); mixErr != nil {
			jobErr := mapVideoError(mixErr)
			items = append(items, videoPlanFailureItem(mixRequest.VideoPath, mixRequest.OutputPath, jobErr))
			if firstErr == nil {
				firstErr = jobErr
			}
			continue
		}

		items = append(items, models.JobResultItem{
			InputPath:   mixRequest.VideoPath,
			OutputPath:  mixRequest.OutputPath,
			Outputs:     []string{mixRequest.OutputPath},
			OutputCount: 1,
			Success:     true,
			Message:     "Audio sync successful",
		})
		if onProgress != nil {
			onProgress(models.JobProgress{Current: jobProgressTotal, Total: jobProgressTotal, Stage: "video.audio_sync", Message: "Audio sync successful"})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

func audioSyncRequestForOutput(planned models.PlannedOutput, plan models.PlanJobResponse, req models.JobRequest) engine.MixRequest {
	videoPath := strings.TrimSpace(planned.InputPath)
	if videoPath == "" {
		videoPath = firstInputPath(req.InputPaths)
	}

	mixRequest := engine.MixRequest{
		VideoPath:        videoPath,
		OutputPath:       planned.OutputPath,
		Mode:             optionString(plan.EffectiveOptions, "mode"),
		OriginalAudio:    optionString(plan.EffectiveOptions, "originalAudio"),
		AudioBitrateKbps: videoIntOption(plan.EffectiveOptions, "audioBitrateKbps"),
	}
	if mixRequest.Mode == "" {
		mixRequest.Mode = engine.MixModeReplace
	}
	if mixRequest.OriginalAudio == "" {
		mixRequest.OriginalAudio = engine.MixOriginalRemove
	}
	mixRequest.OriginalGainDB = videoFloatOption(plan.EffectiveOptions, "originalGainDb")
	mixRequest.SourceGainDB = videoFloatOption(plan.EffectiveOptions, "sourceGainDb")
	mixRequest.DurationSeconds = videoFloatOption(plan.EffectiveOptions, "durationSeconds")
	mixRequest.Normalize, _ = plan.EffectiveOptions["normalize"].(bool)

	if segments, ok := plan.EffectiveOptions["segments"]; ok {
		mixRequest.Segments = audioSyncSegmentsFromEffective(segments)
	}
	mixRequest.SourcePath = optionString(plan.EffectiveOptions, "sourcePath")
	mixRequest.OffsetSeconds = videoFloatOption(plan.EffectiveOptions, "offsetSeconds")
	mixRequest.SourceStartSeconds = videoFloatOption(plan.EffectiveOptions, "sourceStartSeconds")

	return mixRequest
}

// audioSyncEffectiveSegments converts the parsed segments back to the JSON-safe
// option shape the plan exposes. It keeps the documented `fadeMs` key instead of
// leaking the engine's seconds-based field name.
func audioSyncEffectiveSegments(segments []engine.MixSegment) []map[string]any {
	result := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		result = append(result, map[string]any{
			"videoStart":  segment.VideoStart,
			"videoEnd":    segment.VideoEnd,
			"sourcePath":  segment.SourcePath,
			"sourceStart": segment.SourceStart,
			"gainDb":      segment.GainDB,
			"fadeMs":      segment.FadeSeconds * 1000,
		})
	}

	return result
}

// audioSyncSegmentsFromEffective reads the plan's segment list without
// revalidating it: Plan already did that. It accepts both the map shape a JSON
// round trip produces and the typed values an in-process plan still holds.
func audioSyncSegmentsFromEffective(raw any) []engine.MixSegment {
	switch value := raw.(type) {
	case []engine.MixSegment:
		return value
	case []map[string]any:
		return audioSyncSegmentsFromMaps(value)
	case []any:
		objects := make([]map[string]any, 0, len(value))
		for _, item := range value {
			object, ok := item.(map[string]any)
			if !ok {
				return nil
			}
			objects = append(objects, object)
		}
		return audioSyncSegmentsFromMaps(objects)
	default:
		return nil
	}
}

func audioSyncSegmentsFromMaps(objects []map[string]any) []engine.MixSegment {
	segments := make([]engine.MixSegment, 0, len(objects))
	for _, object := range objects {
		segments = append(segments, engine.MixSegment{
			VideoStart:  videoFloatOption(object, "videoStart"),
			VideoEnd:    videoFloatOption(object, "videoEnd"),
			SourceStart: videoFloatOption(object, "sourceStart"),
			SourcePath:  optionString(object, "sourcePath"),
			GainDB:      videoFloatOption(object, "gainDb"),
			FadeSeconds: videoFloatOption(object, "fadeMs") / 1000,
		})
	}

	return segments
}

func videoFloatOption(options map[string]any, key string) float64 {
	if options == nil {
		return 0
	}

	switch value := options[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func videoIntOption(options map[string]any, key string) int {
	if options == nil {
		return 0
	}

	switch value := options[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	default:
		return 0
	}
}
