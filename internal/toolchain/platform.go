// Package toolchain installs and verifies a portable local toolchain inside the
// application data directory: micromamba plus a conda-forge environment
// (ffmpeg, tesseract, poppler, ghostscript), a prebuilt whisper.cpp runtime with
// a Whisper model, and Tesseract language data. Nothing is installed
// system-wide, no administrator rights are required and no user path is
// hardcoded.
package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Supported Whisper models, default first. The empty option means the default.
const (
	ModelSmall   = "small"
	ModelMedium  = "medium"
	ModelLargeV3 = "large-v3"

	DefaultModel = ModelMedium
)

// Supported Tesseract language data. The empty option means both.
const (
	OCRLanguageEnglish = "eng"
	OCRLanguageSpanish = "spa"
)

// WhisperReleaseTag pins the prebuilt whisper.cpp release used by the
// installer, so installs are reproducible and the archive layout is verified.
const WhisperReleaseTag = "b5454"

const (
	micromambaReleaseBaseURL = "https://github.com/mamba-org/micromamba-releases/releases/latest/download/"
	whisperReleaseBaseURL    = "https://github.com/ggerganov/whisper.cpp/releases/download/" + WhisperReleaseTag + "/"
	whisperModelBaseURL      = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"
	tessdataFastBaseURL      = "https://github.com/tesseract-ocr/tessdata_fast/raw/main/"

	// ToolchainDirName is the managed folder inside the application data
	// directory.
	ToolchainDirName = "toolchain"
)

// ArchiveKind selects the extraction routine for a downloaded archive.
type ArchiveKind string

const (
	ArchiveKindTarGz ArchiveKind = "tar.gz"
	ArchiveKindZip   ArchiveKind = "zip"
)

// Platform describes the portable artifacts for one OS/arch pair. Supported is
// false when no verified prebuilt exists; Reason then explains why and is safe
// to surface in the UI.
type Platform struct {
	OS               string
	Arch             string
	MicromambaURL    string
	MicromambaBinary string
	WhisperURL       string
	WhisperArchive   ArchiveKind
	Supported        bool
	Reason           string
}

// PlatformFor resolves the portable artifacts for the given GOOS/GOARCH pair.
// It never fails: an unsupported pair returns Supported=false with a clear
// Reason instead of guessing URLs.
func PlatformFor(goos, goarch string) Platform {
	platform := Platform{OS: strings.TrimSpace(goos), Arch: strings.TrimSpace(goarch)}

	switch platform.OS {
	case "linux":
		platform.MicromambaBinary = "micromamba"
		platform.WhisperArchive = ArchiveKindTarGz
		switch platform.Arch {
		case "amd64":
			platform.MicromambaURL = micromambaReleaseBaseURL + "micromamba-linux-64"
			platform.WhisperURL = whisperReleaseBaseURL + "whisper-bin-ubuntu-x64.tar.gz"
		case "arm64":
			platform.MicromambaURL = micromambaReleaseBaseURL + "micromamba-linux-aarch64"
			platform.WhisperURL = whisperReleaseBaseURL + "whisper-bin-ubuntu-arm64.tar.gz"
		default:
			platform.Reason = fmt.Sprintf(
				"The managed toolchain is not available for linux/%s: no verified prebuilt binaries are pinned for this architecture.",
				platform.Arch,
			)
		}
	case "windows":
		platform.MicromambaBinary = "micromamba.exe"
		platform.WhisperArchive = ArchiveKindZip
		switch platform.Arch {
		case "amd64":
			platform.MicromambaURL = micromambaReleaseBaseURL + "micromamba-win-64.exe"
			platform.WhisperURL = whisperReleaseBaseURL + "whisper-bin-x64.zip"
		case "arm64":
			platform.Reason = "The managed toolchain is not available for Windows on ARM: micromamba only ships a win-64 binary and no ARM64 whisper.cpp prebuilt is verified."
		default:
			platform.Reason = fmt.Sprintf(
				"The managed toolchain is not available for windows/%s: no verified prebuilt binaries are pinned for this architecture.",
				platform.Arch,
			)
		}
	case "darwin":
		platform.Reason = "The managed toolchain is not available on macOS yet: no prebuilt whisper.cpp release and micromamba binary are verified for this slice."
	default:
		platform.Reason = fmt.Sprintf(
			"The managed toolchain is not available for %s/%s: no verified prebuilt binaries exist for this platform.",
			platform.OS,
			platform.Arch,
		)
	}

	platform.Supported = platform.Reason == ""

	return platform
}

// CurrentPlatform resolves the portable artifacts for the running machine.
func CurrentPlatform() Platform {
	return PlatformFor(runtime.GOOS, runtime.GOARCH)
}

// ExeSuffix returns the executable extension for the platform.
func (p Platform) ExeSuffix() string {
	if p.OS == "windows" {
		return ".exe"
	}

	return ""
}

// WhisperCLIName returns the whisper.cpp binary name for the platform.
func (p Platform) WhisperCLIName() string {
	return "whisper-cli" + p.ExeSuffix()
}

// MicromambaName returns the micromamba binary name for the platform.
func (p Platform) MicromambaName() string {
	if strings.TrimSpace(p.MicromambaBinary) != "" {
		return p.MicromambaBinary
	}

	return "micromamba" + p.ExeSuffix()
}

// DataDir resolves the per-user application data directory for the running OS:
// Windows %LOCALAPPDATA%\FileForge, macOS ~/Library/Application Support/FileForge
// and Linux $XDG_DATA_HOME/fileforge or ~/.local/share/fileforge.
func DataDir() (string, error) {
	return dataDirFor(runtime.GOOS, os.Getenv, os.UserHomeDir, os.UserCacheDir)
}

// dataDirFor is the injectable data directory resolver used by tests.
func dataDirFor(goos string, getenv func(string) string, homeDir func() (string, error), cacheDir func() (string, error)) (string, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	if cacheDir == nil {
		cacheDir = os.UserCacheDir
	}

	switch strings.TrimSpace(goos) {
	case "windows":
		if local := strings.TrimSpace(getenv("LOCALAPPDATA")); local != "" {
			return filepath.Clean(filepath.Join(local, "FileForge")), nil
		}

		cache, err := cacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve the local application data directory: %w", err)
		}
		if strings.TrimSpace(cache) == "" {
			return "", fmt.Errorf("resolve the local application data directory: the cache directory is empty")
		}

		return filepath.Clean(filepath.Join(cache, "FileForge")), nil
	case "darwin":
		home, err := homeDir()
		if err != nil {
			return "", fmt.Errorf("resolve the home directory: %w", err)
		}
		if strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve the home directory: the home directory is empty")
		}

		return filepath.Clean(filepath.Join(home, "Library", "Application Support", "FileForge")), nil
	default:
		if xdg := strings.TrimSpace(getenv("XDG_DATA_HOME")); xdg != "" {
			return filepath.Clean(filepath.Join(xdg, "fileforge")), nil
		}

		home, err := homeDir()
		if err != nil {
			return "", fmt.Errorf("resolve the home directory: %w", err)
		}
		if strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve the home directory: the home directory is empty")
		}

		return filepath.Clean(filepath.Join(home, ".local", "share", "fileforge")), nil
	}
}

// SupportedModels lists the Whisper models the installer can download, in
// default-first order.
func SupportedModels() []string {
	return []string{ModelSmall, ModelMedium, ModelLargeV3}
}

// NormalizeModel validates a model option. An empty value selects the default.
func NormalizeModel(model string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return DefaultModel, nil
	}
	for _, supported := range SupportedModels() {
		if normalized == supported {
			return normalized, nil
		}
	}

	return "", fmt.Errorf("model must be small, medium, or large-v3: %s", model)
}

// ModelFileName returns the ggml file name for a model.
func ModelFileName(model string) string {
	return "ggml-" + model + ".bin"
}

// ModelURL returns the pinned Hugging Face download URL for a model.
func ModelURL(model string) (string, error) {
	normalized, err := NormalizeModel(model)
	if err != nil {
		return "", err
	}

	return whisperModelBaseURL + ModelFileName(normalized), nil
}

// SupportedOCRLanguages lists the downloadable Tesseract language data.
func SupportedOCRLanguages() []string {
	return []string{OCRLanguageEnglish, OCRLanguageSpanish}
}

// NormalizeOCRLanguages validates the requested languages. An empty list means
// English and Spanish. Duplicates are removed and order is preserved.
func NormalizeOCRLanguages(languages []string) ([]string, error) {
	if len(languages) == 0 {
		return SupportedOCRLanguages(), nil
	}

	normalized := make([]string, 0, len(languages))
	seen := make(map[string]struct{}, len(languages))
	for _, language := range languages {
		trimmed := strings.ToLower(strings.TrimSpace(language))
		if trimmed == "" {
			continue
		}

		found := false
		for _, supported := range SupportedOCRLanguages() {
			if trimmed == supported {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unsupported OCR language %q: use eng or spa", language)
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}

		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return SupportedOCRLanguages(), nil
	}

	return normalized, nil
}

// TessdataFileName returns the traineddata file name for a language.
func TessdataFileName(language string) string {
	return language + ".traineddata"
}

// TessdataURL returns the tessdata_fast download URL for a language.
func TessdataURL(language string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(language))
	for _, supported := range SupportedOCRLanguages() {
		if trimmed == supported {
			return tessdataFastBaseURL + TessdataFileName(trimmed), nil
		}
	}

	return "", fmt.Errorf("unsupported OCR language %q: use eng or spa", language)
}
