// Package engine runs yt-dlp for the URL downloader tool. It probes a link for
// the formats it really offers and downloads the requested quality with a
// machine-readable progress stream. It owns no Wails service and registers no
// tool: the tool layer consumes ProbeLink and Download.
package engine

const (
	// KindVideo downloads a video stream merged with the best audio into MP4.
	KindVideo = "video"
	// KindAudio extracts audio as MP3.
	KindAudio = "audio"
)

const (
	toolBinary = "yt-dlp"

	// downloadProgressPrefix tags every progress line emitted by yt-dlp. The
	// template prints exactly the four values parseDownloadProgress reads.
	downloadProgressPrefix = "ff-dl"

	downloadProgressTemplate = "download:" + downloadProgressPrefix + " %(progress.downloaded_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s"

	// outputTemplate keeps names readable while capping the title at 120 bytes
	// so the filesystem limit is not hit by long uploads. The id keeps it
	// unique across a queue.
	outputTemplate = "%(title).120B [%(id)s].%(ext)s"
)
