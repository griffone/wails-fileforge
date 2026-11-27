package toolchain

import (
	"fmt"
	"strings"
)

// This file is the single registry of pinned download metadata. Every URL points
// at an immutable release tag, commit, or asset, and every entry carries the
// exact SHA-256 and byte size checked while installing. Values were captured on
// 2026-11-19 from the upstream release metadata:
//
//   - micromamba: GitHub release asset digests for tag 2.10.0-0.
//   - whisper.cpp: GitHub release asset digests for tag b5454.
//   - Whisper models: Hugging Face LFS oids for commit 5359861.
//   - tessdata_fast: SHA-256 computed from the raw files at commit 8741641.
//   - rembg: SHA-256 computed from the v0.0.0 release asset (the release
//     predates GitHub asset digests); the legacy MD5 still matches
//     fc16ebd8b0c10d971d3513d564d01e29.
//   - upscale: SHA-256 computed on 2026-11-27 from the Qualcomm AI Hub Models
//     v0.67.0 ONNX float archive and its extracted files.

const (
	// MicromambaReleaseTag pins the micromamba release. The previous code used
	// the "latest" alias, which made installs depend on the current upstream
	// release instead of a verified one.
	MicromambaReleaseTag = "2.10.0-0"

	// whisperModelCommit pins the Hugging Face revision of the ggml models.
	whisperModelCommit = "5359861c739e955e79d9a303bcbc70fb988958b1"

	// tessdataFastCommit pins the tessdata_fast revision of the language data.
	tessdataFastCommit = "87416418657359cb625c412a48b6e1d6d41c29bd"
)

const (
	micromambaReleaseBaseURL = "https://github.com/mamba-org/micromamba-releases/releases/download/" + MicromambaReleaseTag + "/"
	whisperReleaseBaseURL    = "https://github.com/ggml-org/whisper.cpp/releases/download/" + WhisperReleaseTag + "/"
	whisperModelBaseURL      = "https://huggingface.co/ggerganov/whisper.cpp/resolve/" + whisperModelCommit + "/"
	tessdataFastBaseURL      = "https://github.com/tesseract-ocr/tessdata_fast/raw/" + tessdataFastCommit + "/"
	rembgModelReleaseBaseURL = "https://github.com/danielgatis/rembg/releases/download/v0.0.0/"
)

// platformArtifact is the pinned metadata of one per-platform binary or archive.
type platformArtifact struct {
	FileName  string
	SHA256    string
	SizeBytes int64
}

// fileArtifact is the pinned metadata of one file shared by every platform.
type fileArtifact struct {
	SHA256    string
	SizeBytes int64
}

// micromambaArtifacts pins the release assets keyed by GOOS/GOARCH.
var micromambaArtifacts = map[string]platformArtifact{
	"linux/amd64": {
		FileName:  "micromamba-linux-64",
		SHA256:    "701f39c06f8e6a670a0e186c8479b9e5c0502a2f0cda6cef65d83bdd2875e8bd",
		SizeBytes: 19028608,
	},
	"linux/arm64": {
		FileName:  "micromamba-linux-aarch64",
		SHA256:    "3bb56786e21b9b24024f599fe9185b4a59dc3f9f59937a42311ee583adbbb23c",
		SizeBytes: 23067984,
	},
	"windows/amd64": {
		FileName:  "micromamba-win-64.exe",
		SHA256:    "f8b4284bd4eb1134122eaf93b3ea6c7be567e596de5829e1a6f478e34e41974a",
		SizeBytes: 11723264,
	},
}

// whisperArtifacts pins the prebuilt whisper.cpp runtime archives.
var whisperArtifacts = map[string]platformArtifact{
	"linux/amd64": {
		FileName:  "whisper-bin-ubuntu-x64.tar.gz",
		SHA256:    "a72becf15d7917f990f6313867a52638b82b7f9ef237fb0c980dac56a135781c",
		SizeBytes: 10364195,
	},
	"linux/arm64": {
		FileName:  "whisper-bin-ubuntu-arm64.tar.gz",
		SHA256:    "6b95ebfc60447df48e70ef00a73bdc3f41679ed2d01ef465827206a2ff325149",
		SizeBytes: 4608377,
	},
	"windows/amd64": {
		FileName:  "whisper-bin-x64.zip",
		SHA256:    "6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb",
		SizeBytes: 8928640,
	},
}

// whisperModelArtifacts pins the ggml model files. The sizes and checksums are
// the Hugging Face LFS metadata, so they match the bytes the CDN serves.
var whisperModelArtifacts = map[string]fileArtifact{
	ModelSmall: {
		SHA256:    "1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b",
		SizeBytes: 487601967,
	},
	ModelMedium: {
		SHA256:    "6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208",
		SizeBytes: 1533763059,
	},
	ModelLargeV3: {
		SHA256:    "64d182b440b98d5203c4f9bd541544d84c605196c4f7b845dfa11fb23594d1e2",
		SizeBytes: 3095033483,
	},
}

// tessdataArtifacts pins the Tesseract language data files.
var tessdataArtifacts = map[string]fileArtifact{
	OCRLanguageEnglish: {
		SHA256:    "7d4322bd2a7749724879683fc3912cb542f19906c83bcc1a52132556427170b2",
		SizeBytes: 4113088,
	},
	OCRLanguageSpanish: {
		SHA256:    "6f2e04d02774a18f01bed44b1111f2cd7f3ba7ac9dc4373cd3f898a40ea6b464",
		SizeBytes: 2294433,
	},
}

// platformKey normalizes an OS/arch pair for the metadata maps.
func platformKey(goos, goarch string) string {
	return strings.ToLower(strings.TrimSpace(goos)) + "/" + strings.ToLower(strings.TrimSpace(goarch))
}

// MicromambaArtifact returns the pinned micromamba artifact for the platform.
// An unsupported platform yields an artifact with the zero metadata, which the
// installer never uses because Start rejects unsupported platforms first.
func (p Platform) MicromambaArtifact() Artifact {
	spec := micromambaArtifacts[platformKey(p.OS, p.Arch)]

	return Artifact{
		URL:       p.MicromambaURL,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  spec.FileName,
	}
}

// WhisperArtifact returns the pinned whisper.cpp runtime archive for the
// platform.
func (p Platform) WhisperArtifact() Artifact {
	spec := whisperArtifacts[platformKey(p.OS, p.Arch)]

	return Artifact{
		URL:       p.WhisperURL,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  spec.FileName,
	}
}

// ModelArtifact returns the pinned Whisper model artifact.
func ModelArtifact(model string) (Artifact, error) {
	normalized, err := NormalizeModel(model)
	if err != nil {
		return Artifact{}, err
	}

	spec := whisperModelArtifacts[normalized]
	fileName := ModelFileName(normalized)

	return Artifact{
		URL:       whisperModelBaseURL + fileName,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  fileName,
	}, nil
}

// TessdataArtifact returns the pinned Tesseract language data artifact.
func TessdataArtifact(language string) (Artifact, error) {
	trimmed := strings.ToLower(strings.TrimSpace(language))
	spec, supported := tessdataArtifacts[trimmed]
	if !supported {
		return Artifact{}, fmt.Errorf("unsupported OCR language %q: use eng or spa", language)
	}

	fileName := TessdataFileName(trimmed)

	return Artifact{
		URL:       tessdataFastBaseURL + fileName,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  fileName,
	}, nil
}

// RembgModelArtifact returns the pinned background removal model artifact. It
// reads rembgModelSpec so tests can substitute a small fake payload.
func RembgModelArtifact() Artifact {
	spec := rembgModelSpec

	return Artifact{
		URL:       spec.URL,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  spec.FileName,
	}
}

// UpscaleModelArtifact returns the pinned upscale model archive. It reads
// upscaleModelSpec so tests can substitute a small fake archive.
func UpscaleModelArtifact() Artifact {
	spec := upscaleModelSpec

	return Artifact{
		URL:       spec.URL,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  spec.ArchiveName,
	}
}

// UpscaleAnimeModelArtifact returns the pinned illustration model file. It
// reads upscaleAnimeModelSpec so tests can substitute a small fake payload.
func UpscaleAnimeModelArtifact() Artifact {
	spec := upscaleAnimeModelSpec

	return Artifact{
		URL:       spec.URL,
		SHA256:    spec.SHA256,
		SizeBytes: spec.SizeBytes,
		FileName:  spec.FileName,
	}
}
