package inspect

import (
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	kindImage    = "image"
	kindPDF      = "pdf"
	kindVideo    = "video"
	kindDocument = "document"
	kindOther    = "other"
)

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
		if pages, pageErr := api.PageCountFile(info.Path); pageErr == nil {
			info.PageCount = pages
		}
	}

	return info
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
	case "jpg", "jpeg", "png", "gif":
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
	case "md", "markdown", "docx":
		return kindDocument
	default:
		return kindOther
	}
}

func mimeTypeFor(extension, kind string) string {
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
