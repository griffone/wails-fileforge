package inspect

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fileforge-desktop/internal/models"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	_ "golang.org/x/image/webp"
)

const (
	kindImage    = "image"
	kindPDF      = "pdf"
	kindVideo    = "video"
	kindDocument = "document"
	kindArchive  = "archive"
	kindOther    = "other"

	videoDurationTimeout = 2 * time.Second
)

// videoDurationCommand runs ffprobe for a single path. It is a package
// variable so tests can simulate a missing or failing ffprobe.
var videoDurationCommand = func(ctx context.Context, path string) ([]byte, error) {
	return exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
}

// videoMetadataCommand reads duration, stream codecs and video dimensions in a
// single ffprobe JSON call. It is a package variable so tests can mock it.
var videoMetadataCommand = func(ctx context.Context, path string) ([]byte, error) {
	return exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height:format=duration",
		"-of", "json",
		path,
	).Output()
}

// VideoMetadata is the subset of ffprobe output the workbench shows for video
// files. Zero values mean the value could not be read.
type VideoMetadata struct {
	DurationSeconds float64
	Width           int
	Height          int
	VideoCodec      string
	AudioCodec      string
}

// videoProbeStream, videoProbeFormat and videoProbeOutput mirror the ffprobe
// JSON shape produced by videoMetadataCommand.
type videoProbeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type videoProbeFormat struct {
	Duration string `json:"duration"`
}

type videoProbeOutput struct {
	Streams []videoProbeStream `json:"streams"`
	Format  videoProbeFormat   `json:"format"`
}

// InspectFile describes a single path for UI file cards. It never returns an
// error: missing or unreadable files are reported through Exists and Error.
func InspectFile(path string) models.FileInfoV1 {
	info := models.FileInfoV1{
		Path: strings.TrimSpace(path),
		Kind: kindOther,
	}
	if info.Path == "" {
		info.Error = "path is empty"
		return info
	}

	info.Name = filepath.Base(info.Path)
	info.Directory = filepath.Dir(info.Path)
	info.Extension = strings.ToLower(strings.TrimPrefix(filepath.Ext(info.Path), "."))
	info.Kind = kindFromExtension(info.Extension)
	info.MimeType = mimeTypeFor(info.Extension, info.Kind)

	stat, err := os.Stat(info.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			info.Error = "file not found"
		} else {
			info.Error = "file cannot be accessed"
		}
		return info
	}

	info.Exists = true
	info.ModifiedAt = stat.ModTime().UnixMilli()

	if stat.IsDir() {
		info.Kind = kindOther
		info.Error = "path is a directory"
		return info
	}

	info.SizeBytes = stat.Size()

	if info.Kind == kindImage && supportsImageConfig(info.Extension) {
		if width, height, decodeErr := decodeImageConfig(info.Path); decodeErr == nil {
			info.Width = width
			info.Height = height
		}
	}

	if info.Kind == kindPDF {
		// One validated read gives both the page count and the dimensions.
		// PageCountFile plus PageDimsFile parsed the same document twice.
		if pdfCtx, ctxErr := api.ReadContextFile(info.Path); ctxErr == nil {
			info.PageCount = pdfCtx.PageCount
			if dims, dimsErr := pdfCtx.PageDims(); dimsErr == nil && len(dims) > 0 {
				info.PageWidthPoints = dims[0].Width
				info.PageHeightPoints = dims[0].Height
			}
		}
	}

	if info.Kind == kindVideo {
		if metadata, ok := ProbeVideoMetadata(context.Background(), info.Path); ok {
			info.DurationSeconds = metadata.DurationSeconds
			info.Width = metadata.Width
			info.Height = metadata.Height
			info.VideoCodec = metadata.VideoCodec
			info.AudioCodec = metadata.AudioCodec
		}
	}

	return info
}

// ProbeVideoDuration reports the container duration in seconds. It never fails
// the caller: a missing ffprobe, an unreadable file, or a missing duration all
// return ok=false so file inspection can stay tolerant.
func ProbeVideoDuration(ctx context.Context, path string) (float64, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return 0, false
	}

	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, videoDurationTimeout)
	defer cancel()

	output, err := videoDurationCommand(probeCtx, trimmed)
	if err != nil {
		return 0, false
	}

	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return 0, false
	}

	return duration, true
}

// ProbeVideoMetadata reads duration, video dimensions and stream codecs in one
// ffprobe call. It never fails the caller: a missing ffprobe, unreadable file
// or malformed JSON return ok=false so inspection can stay tolerant. The first
// video stream supplies the dimensions and video codec; the first audio stream
// supplies the audio codec.
func ProbeVideoMetadata(ctx context.Context, path string) (VideoMetadata, bool) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return VideoMetadata{}, false
	}

	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, videoDurationTimeout)
	defer cancel()

	output, err := videoMetadataCommand(probeCtx, trimmed)
	if err != nil {
		return VideoMetadata{}, false
	}

	var parsed videoProbeOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return VideoMetadata{}, false
	}

	metadata := VideoMetadata{}
	if duration, durationErr := strconv.ParseFloat(strings.TrimSpace(parsed.Format.Duration), 64); durationErr == nil &&
		!math.IsNaN(duration) && !math.IsInf(duration, 0) && duration > 0 {
		metadata.DurationSeconds = duration
	}

	hasVideoStream := false
	for _, stream := range parsed.Streams {
		switch strings.ToLower(strings.TrimSpace(stream.CodecType)) {
		case "video":
			if hasVideoStream {
				continue
			}
			hasVideoStream = true
			metadata.Width = stream.Width
			metadata.Height = stream.Height
			metadata.VideoCodec = strings.ToLower(strings.TrimSpace(stream.CodecName))
		case "audio":
			if metadata.AudioCodec != "" {
				continue
			}
			metadata.AudioCodec = strings.ToLower(strings.TrimSpace(stream.CodecName))
		}
	}

	if metadata.DurationSeconds <= 0 && !hasVideoStream {
		return VideoMetadata{}, false
	}

	return metadata, true
}

// InspectFiles describes every path in order. One bad file never fails the batch.
func InspectFiles(paths []string) []models.FileInfoV1 {
	files := make([]models.FileInfoV1, 0, len(paths))
	for _, path := range paths {
		files = append(files, InspectFile(path))
	}
	return files
}

func decodeImageConfig(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = file.Close()
	}()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}

	return config.Width, config.Height, nil
}

func supportsImageConfig(extension string) bool {
	switch extension {
	case "jpg", "jpeg", "png", "gif", "webp":
		return true
	default:
		return false
	}
}

func kindFromExtension(extension string) string {
	switch extension {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif":
		return kindImage
	case "pdf":
		return kindPDF
	case "mp4", "mov", "mkv", "webm", "avi":
		return kindVideo
	case "md", "markdown", "docx", "html", "htm", "txt", "srt", "vtt":
		return kindDocument
	case "zip":
		return kindArchive
	default:
		return kindOther
	}
}

func mimeTypeFor(extension, kind string) string {
	// Pin the bundle types the recipe verification depends on instead of the
	// platform MIME database, which may append a charset or differ per OS.
	switch extension {
	case "zip":
		return "application/zip"
	case "html", "htm":
		return "text/html"
	case "txt":
		return "text/plain"
	case "srt":
		return "application/x-subrip"
	case "vtt":
		return "text/vtt"
	}

	if extension != "" {
		if detected := mime.TypeByExtension("." + extension); detected != "" {
			return detected
		}
	}

	switch kind {
	case kindImage:
		return "image/" + extension
	case kindPDF:
		return "application/pdf"
	case kindVideo:
		return "video/" + extension
	case kindDocument:
		switch extension {
		case "md", "markdown":
			return "text/markdown"
		case "docx":
			return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		}
	}

	return "application/octet-stream"
}
