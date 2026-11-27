package models

// DownloadLinkEntry is one item of an explicit playlist. URL is the item link
// the downloader queues; DurationSeconds is the item length when the site
// reports it.
type DownloadLinkEntry struct {
	URL             string  `json:"url"`
	Title           string  `json:"title"`
	DurationSeconds float64 `json:"durationSeconds"`
}

// DownloadVideoOption is one real video height the link offers. FPS is zero
// when the site does not report it and SizeBytes is zero when the size is
// unknown; the UI hides both instead of inventing a value.
type DownloadVideoOption struct {
	Height    int     `json:"height"`
	Ext       string  `json:"ext"`
	VCodec    string  `json:"vcodec"`
	FPS       float64 `json:"fps"`
	SizeBytes int64   `json:"sizeBytes"`
	HDR       bool    `json:"hdr"`
}

// DownloadAudioOption is one real audio-only format the link offers. BitrateKbps
// is the source bitrate the probe measured, not the MP3 target the user picks.
type DownloadAudioOption struct {
	Ext         string `json:"ext"`
	ACodec      string `json:"acodec"`
	BitrateKbps int    `json:"bitrateKbps"`
	SizeBytes   int64  `json:"sizeBytes"`
}

// DownloadLinkInfoResponse is the probe result the downloader page renders.
// Success is false when the runtime is unavailable, the site is unsupported or
// the network failed; Message then carries the reason and the page shows it
// verbatim. Entries are empty for a single video and Video/Audio are empty for
// a playlist.
type DownloadLinkInfoResponse struct {
	Success         bool                  `json:"success"`
	Message         string                `json:"message"`
	URL             string                `json:"url"`
	ID              string                `json:"id"`
	Title           string                `json:"title"`
	Uploader        string                `json:"uploader"`
	ThumbnailURL    string                `json:"thumbnailUrl"`
	DurationSeconds float64               `json:"durationSeconds"`
	IsPlaylist      bool                  `json:"isPlaylist"`
	Entries         []DownloadLinkEntry   `json:"entries"`
	Video           []DownloadVideoOption `json:"video"`
	Audio           []DownloadAudioOption `json:"audio"`
}
