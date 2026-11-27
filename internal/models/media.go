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

// MediaPeaksRequest asks for the min/max waveform of one visible window. The
// editor requests the window it shows, never a whole multi-hour file.
type MediaPeaksRequest struct {
	Path         string  `json:"path"`
	StartSeconds float64 `json:"startSeconds"`
	SpanSeconds  float64 `json:"spanSeconds"`
	Buckets      int     `json:"buckets"`
}

// MediaPeaksResponse is one window of waveform data. Min and Max are always
// initialised so the waveform lanes never receive null; HasAudio is false when
// the file has no audio stream, in which case both are flat zero.
type MediaPeaksResponse struct {
	Success         bool      `json:"success"`
	Message         string    `json:"message"`
	DurationSeconds float64   `json:"durationSeconds"`
	StartSeconds    float64   `json:"startSeconds"`
	SpanSeconds     float64   `json:"spanSeconds"`
	HasAudio        bool      `json:"hasAudio"`
	Min             []float32 `json:"min"`
	Max             []float32 `json:"max"`
}

// MediaProbeResponse is the container and stream metadata the sync editor needs
// before it can place lanes or snap to frames.
type MediaProbeResponse struct {
	Success         bool    `json:"success"`
	Message         string  `json:"message"`
	Path            string  `json:"path"`
	DurationSeconds float64 `json:"durationSeconds"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	VideoCodec      string  `json:"videoCodec"`
	AudioCodec      string  `json:"audioCodec"`
	FrameRate       float64 `json:"frameRate"`
	HasVideo        bool    `json:"hasVideo"`
	HasAudio        bool    `json:"hasAudio"`
}

// MediaSyncRequest measures the offset between the reference (the video's own
// audio) and the source recording. Zero MaxOffsetSeconds searches the full
// representable range; zero WindowSeconds uses the analyzer default.
type MediaSyncRequest struct {
	ReferencePath    string  `json:"referencePath"`
	SourcePath       string  `json:"sourcePath"`
	MaxOffsetSeconds float64 `json:"maxOffsetSeconds"`
	WindowSeconds    float64 `json:"windowSeconds"`
}

// MediaSyncWindow is one verification window of the sync detection. Label is
// "head", "middle" or "tail"; ResidualMs is the window offset minus the
// reported global offset.
type MediaSyncWindow struct {
	Label         string  `json:"label"`
	AnchorSeconds float64 `json:"anchorSeconds"`
	OffsetSeconds float64 `json:"offsetSeconds"`
	Confidence    float64 `json:"confidence"`
	ResidualMs    float64 `json:"residualMs"`
}

// MediaSyncResponse is the detection result. Low confidence is a successful
// measurement with a warning, not a failure: the editor renders it as "could
// not measure" and leaves the decision to the operator. DriftPPM is nil when
// fewer than two windows could be measured.
type MediaSyncResponse struct {
	Success                  bool              `json:"success"`
	Message                  string            `json:"message"`
	OffsetSeconds            float64           `json:"offsetSeconds"`
	Confidence               float64           `json:"confidence"`
	DriftPPM                 *float64          `json:"driftPpm"`
	Windows                  []MediaSyncWindow `json:"windows"`
	Notes                    []string          `json:"notes"`
	ReferenceDurationSeconds float64           `json:"referenceDurationSeconds"`
	SourceDurationSeconds    float64           `json:"sourceDurationSeconds"`
}

// MediaSyncSegment is one placed block of a source recording on the video
// timeline. FadeMs is converted to seconds before it reaches the engine.
type MediaSyncSegment struct {
	VideoStart  float64 `json:"videoStart"`
	VideoEnd    float64 `json:"videoEnd"`
	SourcePath  string  `json:"sourcePath"`
	SourceStart float64 `json:"sourceStart"`
	GainDB      float64 `json:"gainDb"`
	FadeMs      float64 `json:"fadeMs"`
}

// MediaSyncPreviewRequest describes a short preview render of the sync plan.
// The field set mirrors the tool options plus the render span, so the editor
// can preview exactly what it would export.
type MediaSyncPreviewRequest struct {
	VideoPath          string             `json:"videoPath"`
	StartSeconds       float64            `json:"startSeconds"`
	SpanSeconds        float64            `json:"spanSeconds"`
	Mode               string             `json:"mode"`
	SourcePath         string             `json:"sourcePath"`
	OffsetSeconds      float64            `json:"offsetSeconds"`
	SourceStartSeconds float64            `json:"sourceStartSeconds"`
	OriginalAudio      string             `json:"originalAudio"`
	OriginalGainDB     float64            `json:"originalGainDb"`
	SourceGainDB       float64            `json:"sourceGainDb"`
	Segments           []MediaSyncSegment `json:"segments"`
	AudioBitrateKbps   int                `json:"audioBitrateKbps"`
	DurationSeconds    float64            `json:"durationSeconds"`
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
