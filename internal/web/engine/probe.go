package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
)

// LinkEntry is one item of an explicit playlist.
type LinkEntry struct {
	URL             string
	Title           string
	DurationSeconds float64
}

// VideoOption is one real video height offered by the link.
type VideoOption struct {
	Height    int
	Ext       string
	VCodec    string
	FPS       float64
	SizeBytes int64
	HDR       bool
}

// AudioOption is one real audio-only format offered by the link. BitrateKbps is
// the source bitrate, not the MP3 target the user picks later.
type AudioOption struct {
	Ext         string
	ACodec      string
	BitrateKbps int
	SizeBytes   int64
}

// LinkInfo is the probe result the downloader page renders. Video and Audio are
// empty for a playlist; Entries is empty for a single video.
type LinkInfo struct {
	URL             string
	ID              string
	Title           string
	Uploader        string
	ThumbnailURL    string
	DurationSeconds float64
	IsPlaylist      bool
	Entries         []LinkEntry
	Video           []VideoOption
	Audio           []AudioOption
}

// ProbeLink reads the real formats of one link. It runs the metadata command
// once; when the link turns out to be a playlist it re-runs with
// --flat-playlist so filling the entries never expands every item.
func ProbeLink(ctx context.Context, runner CommandRunner, rawURL string) (*LinkInfo, *WebError) {
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	trimmedURL := strings.TrimSpace(rawURL)
	info, probeErr := probeMetadata(ctx, runner, trimmedURL, false)
	if probeErr != nil {
		return nil, probeErr
	}

	if info.Type == "playlist" {
		info, probeErr = probeMetadata(ctx, runner, trimmedURL, true)
		if probeErr != nil {
			return nil, probeErr
		}
	}

	return buildLinkInfo(trimmedURL, info), nil
}

// probeMetadata runs `yt-dlp -J` and decodes the document. flat adds
// --flat-playlist for the second pass of a playlist.
func probeMetadata(ctx context.Context, runner CommandRunner, rawURL string, flat bool) (*ytInfo, *WebError) {
	args := []string{"-J", "--no-playlist", "--no-warnings"}
	if flat {
		args = append(args, "--flat-playlist")
	}
	args = append(args, rawURL)

	var buffered bytes.Buffer
	result, runErr := runner.Run(ctx, toolBinary, args, func(line string) {
		buffered.WriteString(line)
		buffered.WriteByte('\n')
	})
	if runErr != nil {
		return nil, classifyCommandError(ctx, runErr, result, CodeMetadataFailed, "yt-dlp could not read the link metadata")
	}

	var info ytInfo
	if err := json.Unmarshal(buffered.Bytes(), &info); err != nil {
		return nil, &WebError{
			Code:       CodeMetadataFailed,
			DetailCode: detailMetadataInvalidJSON,
			Message:    "yt-dlp returned metadata that could not be parsed",
			Details:    map[string]any{"error": err.Error()},
		}
	}

	return &info, nil
}

func buildLinkInfo(rawURL string, info *ytInfo) *LinkInfo {
	link := &LinkInfo{
		URL:             rawURL,
		ID:              info.ID,
		Title:           info.Title,
		Uploader:        info.Uploader,
		ThumbnailURL:    info.Thumbnail,
		DurationSeconds: info.Duration,
		IsPlaylist:      info.Type == "playlist",
	}

	if link.IsPlaylist {
		link.Entries = playlistEntries(info.Entries)
		return link
	}

	link.Video = buildVideoOptions(info.Formats)
	link.Audio = buildAudioOptions(info.Formats)

	return link
}

func playlistEntries(entries []ytEntry) []LinkEntry {
	links := make([]LinkEntry, 0, len(entries))
	for _, entry := range entries {
		entryURL := strings.TrimSpace(entry.URL)
		if entryURL == "" {
			entryURL = strings.TrimSpace(entry.WebpageURL)
		}
		if entryURL == "" {
			continue
		}

		links = append(links, LinkEntry{
			URL:             entryURL,
			Title:           entry.Title,
			DurationSeconds: entry.Duration,
		})
	}

	return links
}

// ytInfo and ytFormat mirror only the fields the downloader reads. Keeping the
// DTO small makes it obvious which metadata is trusted.
type ytInfo struct {
	Type      string     `json:"_type"`
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Uploader  string     `json:"uploader"`
	Thumbnail string     `json:"thumbnail"`
	Duration  float64    `json:"duration"`
	Formats   []ytFormat `json:"formats"`
	Entries   []ytEntry  `json:"entries"`
}

type ytEntry struct {
	URL        string  `json:"url"`
	WebpageURL string  `json:"webpage_url"`
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Duration   float64 `json:"duration"`
}

type ytFormat struct {
	FormatID       string  `json:"format_id"`
	FormatNote     string  `json:"format_note"`
	Format         string  `json:"format"`
	Ext            string  `json:"ext"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	Height         int     `json:"height"`
	FPS            float64 `json:"fps"`
	TBR            float64 `json:"tbr"`
	ABR            float64 `json:"abr"`
	Filesize       int64   `json:"filesize"`
	FilesizeApprox int64   `json:"filesize_approx"`
	Protocol       string  `json:"protocol"`
	DynamicRange   string  `json:"dynamic_range"`
}

// buildVideoOptions groups the formats per distinct height and keeps the best
// candidate at each one. A video-only winner has the best audio size merged in,
// because the download policy merges the best audio anyway.
func buildVideoOptions(formats []ytFormat) []VideoOption {
	byHeight := make(map[int]ytFormat)
	for _, format := range formats {
		if !isVideoCandidate(format) {
			continue
		}

		current, exists := byHeight[format.Height]
		if !exists || betterVideoFormat(format, current) {
			byHeight[format.Height] = format
		}
	}

	audioSize, hasAudioSize := bestAudioSize(formats)
	options := make([]VideoOption, 0, len(byHeight))
	for height, format := range byHeight {
		size := formatSize(format)
		if !hasEmbeddedAudio(format) {
			if size > 0 && hasAudioSize {
				size += audioSize
			} else {
				// A partial total would understate the download; report
				// unknown instead of a wrong number.
				size = 0
			}
		}

		options = append(options, VideoOption{
			Height:    height,
			Ext:       format.Ext,
			VCodec:    format.VCodec,
			FPS:       format.FPS,
			SizeBytes: size,
			HDR:       isHDR(format.DynamicRange),
		})
	}

	sort.Slice(options, func(i, j int) bool {
		return options[i].Height > options[j].Height
	})

	return options
}

// buildAudioOptions keeps the best audio-only format per ext/codec pair. DRC
// duplicates and lower-bitrate variants of the same pair collapse into the best
// one, so the picker shows real choices instead of noise.
func buildAudioOptions(formats []ytFormat) []AudioOption {
	type choice struct {
		format  ytFormat
		bitrate float64
	}

	byKey := make(map[string]choice)
	for _, format := range formats {
		if !isAudioCandidate(format) {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(format.Ext)) + "|" + strings.ToLower(strings.TrimSpace(format.ACodec))
		bitrate := audioBitrate(format)
		current, exists := byKey[key]
		if exists && bitrate <= current.bitrate {
			continue
		}
		byKey[key] = choice{format: format, bitrate: bitrate}
	}

	choices := make([]choice, 0, len(byKey))
	for _, picked := range byKey {
		choices = append(choices, picked)
	}
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].bitrate != choices[j].bitrate {
			return choices[i].bitrate > choices[j].bitrate
		}
		if choices[i].format.Ext != choices[j].format.Ext {
			return choices[i].format.Ext < choices[j].format.Ext
		}
		return choices[i].format.ACodec < choices[j].format.ACodec
	})

	options := make([]AudioOption, 0, len(choices))
	for _, picked := range choices {
		options = append(options, AudioOption{
			Ext:         picked.format.Ext,
			ACodec:      picked.format.ACodec,
			BitrateKbps: int(math.Round(picked.bitrate)),
			SizeBytes:   formatSize(picked.format),
		})
	}

	return options
}

// isVideoCandidate accepts a real video stream. Audio-only entries, storyboards
// (vcodec=none), and duplicates from the web_safari client are rejected.
func isVideoCandidate(format ytFormat) bool {
	if format.Height <= 0 {
		return false
	}
	if !hasVideoStream(format) {
		return false
	}

	return !isStoryboard(format) && !isWebSafariFormat(format)
}

func isAudioCandidate(format ytFormat) bool {
	if !hasAudioStream(format) || hasVideoStream(format) {
		return false
	}

	return !isStoryboard(format) && !isWebSafariFormat(format)
}

func hasVideoStream(format ytFormat) bool {
	codec := strings.ToLower(strings.TrimSpace(format.VCodec))
	return codec != "" && codec != "none"
}

func hasAudioStream(format ytFormat) bool {
	codec := strings.ToLower(strings.TrimSpace(format.ACodec))
	return codec != "" && codec != "none"
}

func hasEmbeddedAudio(format ytFormat) bool {
	return hasAudioStream(format)
}

func isStoryboard(format ytFormat) bool {
	if strings.EqualFold(strings.TrimSpace(format.Ext), "mhtml") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(format.FormatNote), "storyboard") {
		return true
	}

	return strings.Contains(strings.ToLower(format.Protocol), "mhtml")
}

// isWebSafariFormat drops the HLS duplicates that the web_safari client reports
// with inflated tbr values; they are not the direct formats the downloader
// wants and they carry no usable size.
func isWebSafariFormat(format ytFormat) bool {
	for _, value := range []string{format.FormatID, format.FormatNote, format.Format} {
		if strings.Contains(strings.ToLower(value), "web_safari") {
			return true
		}
	}

	return false
}

// betterVideoFormat prefers a direct download over a streaming variant, then
// the higher bitrate, then frame rate, then MP4, then format id for a stable
// result across probes.
func betterVideoFormat(candidate, current ytFormat) bool {
	if protocolRank(candidate) != protocolRank(current) {
		return protocolRank(candidate) < protocolRank(current)
	}
	if candidate.TBR != current.TBR {
		return candidate.TBR > current.TBR
	}
	if candidate.FPS != current.FPS {
		return candidate.FPS > current.FPS
	}
	if candidate.Ext != current.Ext {
		return extRank(candidate.Ext) < extRank(current.Ext)
	}

	return candidate.FormatID < current.FormatID
}

func protocolRank(format ytFormat) int {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(format.Protocol)), "http") {
		return 0
	}

	return 1
}

func extRank(ext string) int {
	if strings.EqualFold(strings.TrimSpace(ext), "mp4") {
		return 0
	}

	return 1
}

func formatSize(format ytFormat) int64 {
	if format.FilesizeApprox > 0 {
		return format.FilesizeApprox
	}
	if format.Filesize > 0 {
		return format.Filesize
	}

	return 0
}

func audioBitrate(format ytFormat) float64 {
	if format.ABR > 0 {
		return format.ABR
	}
	if format.TBR > 0 {
		return format.TBR
	}

	return 0
}

// bestAudioSize returns the size of the audio format the `ba` policy picks.
func bestAudioSize(formats []ytFormat) (int64, bool) {
	var best ytFormat
	found := false
	for _, format := range formats {
		if !isAudioCandidate(format) {
			continue
		}
		if !found || audioBitrate(format) > audioBitrate(best) {
			best = format
			found = true
		}
	}
	if !found {
		return 0, false
	}

	size := formatSize(best)
	return size, size > 0
}

func isHDR(dynamicRange string) bool {
	trimmed := strings.TrimSpace(dynamicRange)
	if trimmed == "" {
		return false
	}

	return !strings.EqualFold(trimmed, "SDR")
}
