package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/media"
	"fileforge-desktop/internal/media/analysis"
	"fileforge-desktop/internal/models"
	videoEngine "fileforge-desktop/internal/video/engine"
	"fileforge-desktop/internal/web/engine"
)

const (
	// mediaPreviewDirPattern names the per-process directory that holds the one
	// sync preview the editor plays. A single stable directory keeps previews
	// from piling up in the OS temp tree.
	mediaPreviewDirPattern = "fileforge-sync-preview-*"
	// mediaPreviewFilename is reused for every render so a new preview replaces
	// the old one instead of accumulating files.
	mediaPreviewFilename = "sync-preview.mp4"
)

// mediaAnalyzer is the seam over internal/media/analysis. It exists so the
// service tests can exercise peaks and sync detection without ffmpeg.
type mediaAnalyzer interface {
	Peaks(ctx context.Context, req analysis.PeaksRequest) (*analysis.PeaksResult, *analysis.Error)
	Analyze(ctx context.Context, req analysis.Request) (*analysis.Result, *analysis.Error)
}

// execMediaAnalyzer runs the real analyzer through the process-backed runner.
type execMediaAnalyzer struct {
	runner analysis.Runner
}

func (a execMediaAnalyzer) Peaks(ctx context.Context, req analysis.PeaksRequest) (*analysis.PeaksResult, *analysis.Error) {
	return analysis.Peaks(ctx, a.runner, req)
}

func (a execMediaAnalyzer) Analyze(ctx context.Context, req analysis.Request) (*analysis.Result, *analysis.Error) {
	return analysis.Analyze(ctx, a.runner, req)
}

// mediaMetadataProbe reads container and stream metadata. It is a function so
// tests can answer without ffprobe.
type mediaMetadataProbe func(ctx context.Context, path string) (inspect.VideoMetadata, bool)

// mediaPreviewRenderer renders a short span of a mix plan to outputPath.
type mediaPreviewRenderer func(ctx context.Context, req videoEngine.MixRequest, startSeconds, spanSeconds float64, outputPath string) *videoEngine.VideoError

// mediaServiceDeps collects every process boundary the media endpoints touch.
// Nil fields fall back to the production implementation.
type mediaServiceDeps struct {
	probe         engine.RuntimeProbe
	runner        engine.CommandRunner
	analyzer      mediaAnalyzer
	probeMetadata mediaMetadataProbe
	renderPreview mediaPreviewRenderer
}

// MediaService exposes read-only media metadata and preview rendering to the
// UI: link formats for the downloader, waveform peaks, stream metadata, sync
// detection and short audio-sync previews for the sync editor.
type MediaService struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner

	analyzer      mediaAnalyzer
	probeMetadata mediaMetadataProbe
	renderPreview mediaPreviewRenderer

	// previewMu serialises renders so the previous preview is fully removed
	// before the next one is written to the same path.
	previewMu  sync.Mutex
	previewDir string
}

// NewMediaService builds the service with the production runtime probe and the
// process-backed command runner.
func NewMediaService() *MediaService {
	return NewMediaServiceWithDeps(nil, nil)
}

// NewMediaServiceWithDeps lets tests inject a fake probe and runner. A nil
// dependency falls back to the production one, so callers may override only the
// half they need.
func NewMediaServiceWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *MediaService {
	return newMediaService(mediaServiceDeps{probe: probe, runner: runner})
}

// newMediaService builds the service from overridable deps. The analysis,
// metadata and preview seams default to the real ffmpeg-backed implementations,
// which keeps the service usable in production while staying testable.
func newMediaService(deps mediaServiceDeps) *MediaService {
	if deps.analyzer == nil {
		deps.analyzer = execMediaAnalyzer{runner: analysis.NewExecRunner()}
	}
	if deps.probeMetadata == nil {
		deps.probeMetadata = inspect.ProbeVideoMetadata
	}
	if deps.renderPreview == nil {
		deps.renderPreview = renderMediaPreview
	}

	return &MediaService{
		probe:         deps.probe,
		runner:        deps.runner,
		analyzer:      deps.analyzer,
		probeMetadata: deps.probeMetadata,
		renderPreview: deps.renderPreview,
	}
}

// ProbeLink reads the real formats of one link and maps them onto the bindable
// DTO. It never panics and never returns an error: every failure — missing
// runtime, unsupported site, network error — becomes Success false plus the
// engine message, which the page renders as the row error and offers to retry.
func (s *MediaService) ProbeLink(rawURL string) models.DownloadLinkInfoResponse {
	link := strings.TrimSpace(rawURL)
	response := models.DownloadLinkInfoResponse{
		URL:     link,
		Entries: []models.DownloadLinkEntry{},
		Video:   []models.DownloadVideoOption{},
		Audio:   []models.DownloadAudioOption{},
	}
	if link == "" {
		response.Message = "Enter a link to probe."
		return response
	}

	probe := s.probe
	if probe == nil {
		probe = engine.NewToolRuntimeProbe()
	}
	if err := probe.Check(context.Background()); err != nil {
		response.Message = mediaFailureMessage(err, "The downloader runtime is unavailable.")
		return response
	}

	runner := s.runner
	if runner == nil {
		runner = &engine.ExecCommandRunner{}
	}

	info, probeErr := engine.ProbeLink(context.Background(), runner, link)
	if probeErr != nil {
		response.Message = mediaFailureMessage(probeErr, "The link could not be read.")
		return response
	}

	return downloadLinkInfo(info)
}

// AnalyzePeaks reduces one visible window to per-bucket min/max values. A file
// without audio is a successful response with HasAudio false, not an error.
func (s *MediaService) AnalyzePeaks(req models.MediaPeaksRequest) models.MediaPeaksResponse {
	response := models.MediaPeaksResponse{
		StartSeconds: req.StartSeconds,
		SpanSeconds:  req.SpanSeconds,
		Min:          []float32{},
		Max:          []float32{},
	}

	result, analysisErr := s.analyzer.Peaks(context.Background(), analysis.PeaksRequest{
		Path:         req.Path,
		StartSeconds: req.StartSeconds,
		SpanSeconds:  req.SpanSeconds,
		Buckets:      req.Buckets,
	})
	if analysisErr != nil {
		response.Message = analysisFailureMessage(analysisErr, "The waveform could not be read.")
		return response
	}

	response.Success = true
	response.Message = "Waveform loaded."
	response.DurationSeconds = result.DurationSeconds
	response.StartSeconds = result.StartSeconds
	response.SpanSeconds = result.SpanSeconds
	response.HasAudio = result.HasAudio
	response.Min = nonNilFloat32(result.Min)
	response.Max = nonNilFloat32(result.Max)

	return response
}

// ProbeMedia reports duration, dimensions, codecs and frame rate for a file.
// Files without a video or audio stream report HasVideo/HasAudio false instead
// of failing.
func (s *MediaService) ProbeMedia(path string) models.MediaProbeResponse {
	trimmed := strings.TrimSpace(path)
	response := models.MediaProbeResponse{Path: trimmed}
	if trimmed == "" {
		response.Message = "Choose a file to probe."
		return response
	}

	metadata, ok := s.probeMetadata(context.Background(), trimmed)
	if !ok {
		response.Message = "The media metadata could not be read."
		return response
	}

	response.Success = true
	response.Message = "Media metadata loaded."
	response.DurationSeconds = metadata.DurationSeconds
	response.Width = metadata.Width
	response.Height = metadata.Height
	response.VideoCodec = metadata.VideoCodec
	response.AudioCodec = metadata.AudioCodec
	response.FrameRate = metadata.FrameRate
	response.HasVideo = metadata.VideoCodec != "" || metadata.Width > 0 || metadata.Height > 0
	response.HasAudio = metadata.AudioCodec != ""

	return response
}

// DetectSync measures where the source recording's t = 0 sits on the reference
// timeline. Low confidence is a successful result with notes; only an
// undecodable or silent file is a failure.
func (s *MediaService) DetectSync(req models.MediaSyncRequest) models.MediaSyncResponse {
	response := models.MediaSyncResponse{
		Windows: []models.MediaSyncWindow{},
		Notes:   []string{},
	}

	result, analysisErr := s.analyzer.Analyze(context.Background(), analysis.Request{
		ReferencePath:    req.ReferencePath,
		SourcePath:       req.SourcePath,
		MaxOffsetSeconds: req.MaxOffsetSeconds,
		WindowSeconds:    req.WindowSeconds,
	})
	if analysisErr != nil {
		response.Message = analysisFailureMessage(analysisErr, "The files could not be analyzed.")
		return response
	}

	response.Success = true
	response.Message = "Sync measured."
	response.OffsetSeconds = result.OffsetSeconds
	response.Confidence = result.Confidence
	response.DriftPPM = result.DriftPPM
	response.ReferenceDurationSeconds = result.ReferenceDurationSeconds
	response.SourceDurationSeconds = result.SourceDurationSeconds

	for _, window := range result.Windows {
		response.Windows = append(response.Windows, models.MediaSyncWindow{
			Label:         window.Label,
			AnchorSeconds: window.AnchorSeconds,
			OffsetSeconds: window.OffsetSeconds,
			Confidence:    window.Confidence,
			ResidualMs:    window.ResidualMs,
		})
	}
	if result.Notes != nil {
		response.Notes = append(response.Notes, result.Notes...)
	}

	return response
}

// RenderSyncPreview renders a short span of the sync plan into the process
// preview directory and registers it for token-scoped playback. The previous
// preview is removed first so previews never pile up; a render failure or a
// cancellation is reported as Success false with the engine message.
func (s *MediaService) RenderSyncPreview(req models.MediaSyncPreviewRequest) models.MediaSourceResponse {
	if strings.TrimSpace(req.VideoPath) == "" {
		return models.MediaSourceResponse{Success: false, Message: "Choose a video to preview."}
	}

	s.previewMu.Lock()
	defer s.previewMu.Unlock()

	previewDir, dirErr := s.ensurePreviewDirLocked()
	if dirErr != nil {
		return models.MediaSourceResponse{Success: false, Message: "A temporary preview directory could not be created."}
	}

	outputPath := filepath.Join(previewDir, mediaPreviewFilename)
	// Remove the previous preview before rendering the new one. A file still
	// open in the webview is not an error here: the render reports whether the
	// path could actually be written.
	_ = os.Remove(outputPath)

	mixRequest := mediaMixRequest(req, outputPath)
	if renderErr := s.renderPreview(context.Background(), mixRequest, req.StartSeconds, req.SpanSeconds, outputPath); renderErr != nil {
		return models.MediaSourceResponse{Success: false, Message: mediaPreviewFailureMessage(renderErr)}
	}

	token, registerErr := media.Register(outputPath)
	if registerErr != nil {
		return models.MediaSourceResponse{Success: false, Message: "The preview could not be registered for playback."}
	}

	response := models.MediaSourceResponse{
		Success: true,
		URL:     "/media/" + token,
		Message: "ok",
	}
	if baseURL, serverErr := media.EnsureServer(); serverErr == nil && baseURL != "" {
		response.FallbackURL = baseURL + "/media/" + token
	}

	return response
}

// ensurePreviewDirLocked returns the process preview directory, creating it on
// first use. Callers must hold previewMu.
func (s *MediaService) ensurePreviewDirLocked() (string, error) {
	if s.previewDir != "" {
		if info, err := os.Stat(s.previewDir); err == nil && info.IsDir() {
			return s.previewDir, nil
		}
	}

	dir, err := os.MkdirTemp("", mediaPreviewDirPattern)
	if err != nil {
		return "", err
	}
	s.previewDir = dir

	return dir, nil
}

// mediaMixRequest copies the preview DTO onto the engine request. FadeMs is
// converted to the seconds the engine expects; every other time is already in
// seconds.
func mediaMixRequest(req models.MediaSyncPreviewRequest, outputPath string) videoEngine.MixRequest {
	segments := make([]videoEngine.MixSegment, 0, len(req.Segments))
	for _, segment := range req.Segments {
		segments = append(segments, videoEngine.MixSegment{
			VideoStart:  segment.VideoStart,
			VideoEnd:    segment.VideoEnd,
			SourceStart: segment.SourceStart,
			SourcePath:  segment.SourcePath,
			GainDB:      segment.GainDB,
			FadeSeconds: segment.FadeMs / 1000,
		})
	}

	return videoEngine.MixRequest{
		VideoPath:          req.VideoPath,
		OutputPath:         outputPath,
		Mode:               req.Mode,
		SourcePath:         req.SourcePath,
		OffsetSeconds:      req.OffsetSeconds,
		SourceStartSeconds: req.SourceStartSeconds,
		OriginalAudio:      req.OriginalAudio,
		OriginalGainDB:     req.OriginalGainDB,
		SourceGainDB:       req.SourceGainDB,
		Segments:           segments,
		AudioBitrateKbps:   req.AudioBitrateKbps,
		DurationSeconds:    req.DurationSeconds,
	}
}

// renderMediaPreview is the production preview renderer: the video engine's
// short re-encode through the real ffmpeg runtime.
func renderMediaPreview(ctx context.Context, req videoEngine.MixRequest, startSeconds, spanSeconds float64, outputPath string) *videoEngine.VideoError {
	return videoEngine.RenderPreview(ctx, videoEngine.NewFFmpegRuntimeProbe(), &videoEngine.ExecCommandRunner{}, req, startSeconds, spanSeconds, outputPath)
}

// nonNilFloat32 returns an initialised slice so the generated binding never
// sees null for a waveform array.
func nonNilFloat32(values []float32) []float32 {
	if values == nil {
		return []float32{}
	}

	return values
}

// analysisFailureMessage prefers the analyzer's user-facing message over the
// code-prefixed error string.
func analysisFailureMessage(err *analysis.Error, fallback string) string {
	if err != nil {
		if message := strings.TrimSpace(err.Message); message != "" {
			return message
		}
		if rendered := strings.TrimSpace(err.Error()); rendered != "" {
			return rendered
		}
	}

	return fallback
}

// mediaPreviewFailureMessage prefers the engine's user-facing message, which
// already says whether the runtime is missing, the plan is invalid or ffmpeg
// failed.
func mediaPreviewFailureMessage(err *videoEngine.VideoError) string {
	if err != nil {
		if message := strings.TrimSpace(err.Message); message != "" {
			return message
		}
		if rendered := strings.TrimSpace(err.Error()); rendered != "" {
			return rendered
		}
	}

	return "The preview could not be rendered."
}

// downloadLinkInfo copies the engine result onto the wire DTO. Slices are
// always initialised so the generated binding never sees null for a list the
// UI iterates.
func downloadLinkInfo(info *engine.LinkInfo) models.DownloadLinkInfoResponse {
	response := models.DownloadLinkInfoResponse{
		Success:         true,
		Message:         "Link formats loaded.",
		URL:             info.URL,
		ID:              info.ID,
		Title:           info.Title,
		Uploader:        info.Uploader,
		ThumbnailURL:    info.ThumbnailURL,
		DurationSeconds: info.DurationSeconds,
		IsPlaylist:      info.IsPlaylist,
		Entries:         make([]models.DownloadLinkEntry, 0, len(info.Entries)),
		Video:           make([]models.DownloadVideoOption, 0, len(info.Video)),
		Audio:           make([]models.DownloadAudioOption, 0, len(info.Audio)),
	}

	for _, entry := range info.Entries {
		response.Entries = append(response.Entries, models.DownloadLinkEntry{
			URL:             entry.URL,
			Title:           entry.Title,
			DurationSeconds: entry.DurationSeconds,
		})
	}

	for _, option := range info.Video {
		response.Video = append(response.Video, models.DownloadVideoOption{
			Height:    option.Height,
			Ext:       option.Ext,
			VCodec:    option.VCodec,
			FPS:       option.FPS,
			SizeBytes: option.SizeBytes,
			HDR:       option.HDR,
		})
	}

	for _, option := range info.Audio {
		response.Audio = append(response.Audio, models.DownloadAudioOption{
			Ext:         option.Ext,
			ACodec:      option.ACodec,
			BitrateKbps: option.BitrateKbps,
			SizeBytes:   option.SizeBytes,
		})
	}

	return response
}

// mediaFailureMessage prefers the typed engine message, which is written for
// the user, over a wrapped Go error string.
func mediaFailureMessage(err error, fallback string) string {
	var webErr *engine.WebError
	if errors.As(err, &webErr) && webErr != nil {
		if message := strings.TrimSpace(webErr.Message); message != "" {
			return message
		}
	}
	if err != nil {
		if message := strings.TrimSpace(err.Error()); message != "" {
			return message
		}
	}

	return fallback
}
