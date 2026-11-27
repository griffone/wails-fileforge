package app

import (
	"context"
	"errors"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/web/engine"
)

// MediaService exposes read-only media metadata to the UI. Today it answers one
// question — what formats does this link really offer — so the downloader page
// can build its quality picker from real choices instead of fixed presets.
type MediaService struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
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
	return &MediaService{probe: probe, runner: runner}
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
