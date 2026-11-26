package engine

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Supported output formats. Each format names the ffmpeg encoder and the codec
// the verification layer expects to find with ffprobe.
const (
	FormatMP3  = "mp3"
	FormatM4A  = "m4a"
	FormatOpus = "opus"
	FormatWAV  = "wav"
	FormatFLAC = "flac"
)

// Format describes one target audio format.
type Format struct {
	Name string
	// Encoder is the ffmpeg encoder name required in `ffmpeg -encoders`.
	Encoder string
	// Codec is the codec name ffprobe reports for a valid output.
	Codec string
	// Lossless formats ignore a requested bitrate.
	Lossless bool
	// DefaultBitrateKbps applies when the request omits bitrateKbps.
	DefaultBitrateKbps int
}

var supportedFormats = []Format{
	{Name: FormatMP3, Encoder: "libmp3lame", Codec: "mp3", DefaultBitrateKbps: 192},
	{Name: FormatM4A, Encoder: "aac", Codec: "aac", DefaultBitrateKbps: 192},
	{Name: FormatOpus, Encoder: "libopus", Codec: "opus", DefaultBitrateKbps: 128},
	{Name: FormatWAV, Encoder: "pcm_s16le", Codec: "pcm_s16le", Lossless: true},
	{Name: FormatFLAC, Encoder: "flac", Codec: "flac", Lossless: true},
}

// SupportedFormatNames returns the stable format names in display order.
func SupportedFormatNames() []string {
	names := make([]string, 0, len(supportedFormats))
	for _, format := range supportedFormats {
		names = append(names, format.Name)
	}

	return names
}

// AllEncoderNames returns every encoder the audio domain may need, in display
// order. RuntimeState uses it to report an incomplete ffmpeg build.
func AllEncoderNames() []string {
	names := make([]string, 0, len(supportedFormats))
	for _, format := range supportedFormats {
		names = append(names, format.Encoder)
	}

	return names
}

// FormatByName resolves a supported format. An empty name resolves to mp3 so a
// request that omits the option keeps the documented default.
func FormatByName(raw string) (Format, *AudioError) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		name = FormatMP3
	}
	for _, format := range supportedFormats {
		if format.Name == name {
			return format, nil
		}
	}

	return Format{}, &AudioError{
		Code:    ErrorCodeFormatUnsupported,
		Message: fmt.Sprintf("format must be one of %s", strings.Join(SupportedFormatNames(), ", ")),
	}
}

// FormatForExtension resolves the format a file extension already uses. The
// boolean is false for extensions outside the supported set.
func FormatForExtension(rawExtension string) (Format, bool) {
	extension := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(rawExtension)), ".")
	for _, format := range supportedFormats {
		if format.Name == extension {
			return format, true
		}
	}

	return Format{}, false
}

// TargetFormatForInput preserves the input container when it is one of the
// supported formats. Ogg and raw AAC map to their closest supported container;
// anything else falls back to mp3.
func TargetFormatForInput(path string) Format {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(path))), ".")
	if format, ok := FormatForExtension(extension); ok {
		return format
	}

	switch extension {
	case "ogg":
		return mustFormat(FormatOpus)
	case "aac":
		return mustFormat(FormatM4A)
	default:
		return mustFormat(FormatMP3)
	}
}

func mustFormat(name string) Format {
	format, err := FormatByName(name)
	if err != nil {
		panic(err)
	}

	return format
}
