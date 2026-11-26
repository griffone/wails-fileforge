package engine

import (
	"path/filepath"
	"strings"
)

// audioInputExtensions are the containers the audio tools decode. Extract also
// accepts video containers, because pulling the audio track out of a video is
// its whole purpose.
var audioInputExtensions = map[string]struct{}{
	"mp3":  {},
	"m4a":  {},
	"aac":  {},
	"opus": {},
	"ogg":  {},
	"wav":  {},
	"flac": {},
	"wma":  {},
	"aiff": {},
	"aif":  {},
}

var videoInputExtensions = map[string]struct{}{
	"mp4":  {},
	"m4v":  {},
	"mov":  {},
	"mkv":  {},
	"webm": {},
	"avi":  {},
	"wmv":  {},
	"flv":  {},
	"ts":   {},
	"mts":  {},
	"3gp":  {},
}

// ExtensionOf returns the lower-case extension without the leading dot.
func ExtensionOf(path string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(strings.TrimSpace(path))), ".")
}

// IsAudioInputExtension reports whether the extension is an audio container the
// tools decode.
func IsAudioInputExtension(extension string) bool {
	_, ok := audioInputExtensions[strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".")]

	return ok
}

// IsVideoInputExtension reports whether the extension is a video container the
// extract tool accepts.
func IsVideoInputExtension(extension string) bool {
	_, ok := videoInputExtensions[strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extension)), ".")]

	return ok
}

// AudioInputExtensions returns the accepted audio extensions in a stable order.
func AudioInputExtensions() []string {
	return []string{"mp3", "m4a", "aac", "opus", "ogg", "wav", "flac", "wma", "aiff"}
}

// ExtractInputExtensions returns the accepted audio and video extensions in a
// stable order.
func ExtractInputExtensions() []string {
	extensions := AudioInputExtensions()

	return append(extensions, "mp4", "m4v", "mov", "mkv", "webm", "avi", "wmv", "flv", "ts", "mts", "3gp")
}
