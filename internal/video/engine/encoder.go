package engine

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// Shared encoder tuning. Every ffmpeg path that re-encodes video (convert,
// merge, trim, burn) must use these values so one path can never fall back to
// the old single-threaded VP9 crawl or the slow x264 preset.
const (
	// libvpx-vp9 cpu-used: lower is slower and better. Benchmarks on a real
	// 720x1280 30 fps clip with realtime deadline and row-mt showed ~4.5x
	// (cpu-used 5), ~6x (7) and ~8x (8) speedups over the old defaults with
	// SSIM within 0.2% of the source. The high tier stays conservative.
	vp9CPUUsedHigh   = 5
	vp9CPUUsedMedium = 7
	vp9CPUUsedLow    = 8

	// VP9 row-mt stops scaling past the available cores; cap the request so
	// ffmpeg does not spawn idle worker threads.
	vp9MaxThreads = 16

	// x264 presets: "medium" was the old default everywhere. veryfast halves
	// the encode time at the same CRF with a negligible visual difference.
	x264PresetHigh   = "veryfast"
	x264PresetMedium = "veryfast"
	x264PresetLow    = "superfast"

	audioBitrateAAC  = "160k"
	audioBitrateOpus = "128k"
)

// videoEncoding is the shared encoder configuration for every ffmpeg path that
// re-encodes video.
type videoEncoding struct {
	videoCodec   string
	videoBitrate string
	videoCRF     string
	x264Preset   string
	audioCodec   string
	audioBitrate string
	faststart    bool
	vp9CPUUsed   int
}

// resolveVideoEncoding maps a target format and quality preset to the encoder
// settings shared by convert, merge and trim. An empty CRF means bitrate-only
// mode.
func resolveVideoEncoding(targetFormat, qualityPreset string) (videoEncoding, error) {
	targetFormat = strings.ToLower(strings.TrimSpace(targetFormat))
	qualityPreset = strings.ToLower(strings.TrimSpace(qualityPreset))

	encoding := videoEncoding{
		videoCodec:   "libx264",
		videoBitrate: "2500k",
		videoCRF:     "23",
		x264Preset:   x264PresetMedium,
		audioCodec:   "aac",
		audioBitrate: audioBitrateAAC,
		faststart:    true,
	}

	switch targetFormat {
	case TargetFormatMP4:
	case TargetFormatWebM:
		encoding = videoEncoding{
			videoCodec:   "libvpx-vp9",
			videoBitrate: "1800k",
			videoCRF:     "32",
			audioCodec:   "libopus",
			audioBitrate: audioBitrateOpus,
			vp9CPUUsed:   vp9CPUUsedMedium,
		}
	default:
		return videoEncoding{}, fmt.Errorf("targetFormat must be mp4 or webm")
	}

	switch qualityPreset {
	case QualityPresetHigh:
		if targetFormat == TargetFormatWebM {
			encoding.videoCRF = "28"
			encoding.videoBitrate = "2600k"
			encoding.vp9CPUUsed = vp9CPUUsedHigh
		} else {
			encoding.videoCRF = "20"
			encoding.videoBitrate = "4500k"
			encoding.x264Preset = x264PresetHigh
		}
	case QualityPresetMedium:
		// defaults
	case QualityPresetLow:
		if targetFormat == TargetFormatWebM {
			encoding.videoCRF = "36"
			encoding.videoBitrate = "1200k"
			encoding.vp9CPUUsed = vp9CPUUsedLow
		} else {
			encoding.videoCRF = "28"
			encoding.videoBitrate = "1400k"
			encoding.x264Preset = x264PresetLow
		}
	default:
		return videoEncoding{}, fmt.Errorf("qualityPreset must be high, medium, or low")
	}

	return encoding, nil
}

// withBitrateOnly switches an encoding to a pure bitrate target, dropping CRF.
func (e videoEncoding) withBitrateOnly(bitrateKbps int) videoEncoding {
	e.videoBitrate = fmt.Sprintf("%dk", bitrateKbps)
	e.videoCRF = ""
	return e
}

// appendVideoEncodingArgs appends the codec, tuning and audio arguments shared
// by every re-encode path.
func appendVideoEncodingArgs(args []string, encoding videoEncoding, removeAudio bool) []string {
	args = append(args, "-c:v", encoding.videoCodec, "-b:v", encoding.videoBitrate)
	if encoding.videoCRF != "" {
		args = append(args, "-crf", encoding.videoCRF)
	}

	if encoding.videoCodec == "libvpx-vp9" {
		args = append(args,
			"-row-mt", "1",
			"-cpu-used", strconv.Itoa(encoding.vp9CPUUsed),
			"-deadline", "realtime",
			"-threads", strconv.Itoa(vp9ThreadCount()),
			"-pix_fmt", "yuv420p",
		)
	} else {
		args = append(args, "-preset", encoding.x264Preset, "-pix_fmt", "yuv420p")
	}

	if removeAudio {
		return append(args, "-an")
	}

	args = append(args, "-c:a", encoding.audioCodec, "-b:a", encoding.audioBitrate)
	if encoding.faststart {
		args = append(args, "-movflags", "+faststart")
	}

	return args
}

// vp9ThreadCount returns the thread budget for libvpx-vp9. Row-based
// multithreading already keeps every core busy, so more threads than cores only
// adds overhead.
func vp9ThreadCount() int {
	threads := runtime.NumCPU()
	if threads > vp9MaxThreads {
		threads = vp9MaxThreads
	}
	if threads < 1 {
		threads = 1
	}

	return threads
}
