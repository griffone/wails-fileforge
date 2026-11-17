package toolchain

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Managed toolchain layout inside <dataDir>/toolchain:
//
//	micromamba(.exe)
//	env/                  conda prefix (bin/, Library/bin, Scripts on Windows)
//	whisper/              extracted prebuilt whisper.cpp release
//	models/ggml-<model>.bin
//	tessdata/eng.traineddata, spa.traineddata
//	manifest.json
//
// The helpers below are the single source of truth for those paths.

// ToolchainDir returns the managed folder inside the application data
// directory.
func ToolchainDir(dataDir string) string {
	return filepath.Join(dataDir, ToolchainDirName)
}

// ManagedWhisperCLIPath returns the managed whisper.cpp binary path for the
// running OS. It is exported so the transcription engine can prefer the managed
// runtime without importing the installer internals.
func ManagedWhisperCLIPath(dataDir string) string {
	return filepath.Join(ToolchainDir(dataDir), "whisper", "whisper-cli"+PlatformFor(runtime.GOOS, runtime.GOARCH).ExeSuffix())
}

// ManagedModelPaths returns the managed Whisper model paths in default-first
// order (the selected default model, then the other supported models).
func ManagedModelPaths(dataDir string) []string {
	modelsDir := filepath.Join(ToolchainDir(dataDir), "models")
	ordered := make([]string, 0, len(SupportedModels()))
	ordered = append(ordered, DefaultModel)
	for _, model := range SupportedModels() {
		if model != DefaultModel {
			ordered = append(ordered, model)
		}
	}

	paths := make([]string, 0, len(ordered))
	for _, model := range ordered {
		paths = append(paths, filepath.Join(modelsDir, ModelFileName(model)))
	}

	return paths
}

// ManagedTessdataDir returns the managed Tesseract language data folder.
func ManagedTessdataDir(dataDir string) string {
	return filepath.Join(ToolchainDir(dataDir), "tessdata")
}

// tessdataPDFAssets lists the Tesseract data files required by the searchable
// PDF output. The languages are downloaded separately; without these files
// tesseract exits successfully but writes no PDF, which later surfaces as a
// misleading "failed to merge the recognized pages" error.
var tessdataPDFAssets = []string{
	"pdf.ttf",
	filepath.Join("configs", "pdf"),
	filepath.Join("configs", "txt"),
}

// tessdataAssetsSourceDir returns the conda prefix folder that ships the
// Tesseract PDF assets. Windows environments keep share under Library.
func tessdataAssetsSourceDir(root, goos string) string {
	if goos == "windows" {
		return filepath.Join(envDir(root), "Library", "share", "tessdata")
	}

	return filepath.Join(envDir(root), "share", "tessdata")
}

// tessdataPDFAssetsState reports whether every PDF asset is present and gives
// the user-facing detail shown by the toolchain status.
func tessdataPDFAssetsState(dir string) (bool, string) {
	for _, relative := range tessdataPDFAssets {
		if !fileWithContent(filepath.Join(dir, relative)) {
			return false, "Tesseract PDF output data is incomplete (missing config or font). Run Install / repair."
		}
	}

	return true, "Tesseract PDF output data"
}

// whisperRuntimeState verifies that the managed whisper-cli exists and, on
// Linux, that the soname symlinks required by the prebuilt binary are present.
// Dropping those symlinks during extraction leaves whisper-cli unable to load
// libwhisper.so.1, so the toolchain must not report the runtime as ready.
func whisperRuntimeState(root string, platform Platform) (bool, string) {
	return whisperRuntimeStateInDir(whisperDir(root), platform)
}

func whisperRuntimeStateInDir(dir string, platform Platform) (bool, string) {
	cli := filepath.Join(dir, platform.WhisperCLIName())
	if !fileWithContent(cli) {
		return false, "Whisper runtime is not installed."
	}

	if platform.OS == "linux" {
		for _, library := range []string{"libwhisper.so.1", "libggml.so.0", "libggml-base.so.0"} {
			if !fileWithContent(filepath.Join(dir, library)) {
				return false, "Whisper runtime is incomplete (missing shared libraries). Run Install / repair."
			}
		}
	}

	return true, "Local whisper.cpp transcription runtime"
}

func whisperRuntimeReadyInDir(dir string, platform Platform) bool {
	ready, _ := whisperRuntimeStateInDir(dir, platform)
	return ready
}

// managedBinDirs lists the conda prefix binary folders in PATH precedence
// order. Windows environments ship DLLs and executables under Library/bin and
// Scripts; Unix uses bin.
func managedBinDirs(root, goos string) []string {
	envDir := filepath.Join(root, "env")
	if goos == "windows" {
		return []string{
			filepath.Join(envDir, "Library", "bin"),
			filepath.Join(envDir, "Scripts"),
			filepath.Join(envDir, "bin"),
		}
	}

	return []string{filepath.Join(envDir, "bin")}
}

// managedPathDirs returns the directories prepended to PATH: the conda prefix
// binary folders plus the extracted whisper runtime.
func managedPathDirs(root, goos string) []string {
	dirs := managedBinDirs(root, goos)
	dirs = append(dirs, filepath.Join(root, "whisper"))

	return dirs
}

func micromambaPath(root string, platform Platform) string {
	return filepath.Join(root, platform.MicromambaName())
}

func envDir(root string) string {
	return filepath.Join(root, "env")
}

func whisperDir(root string) string {
	return filepath.Join(root, "whisper")
}

func whisperCLIPath(root string, platform Platform) string {
	return filepath.Join(whisperDir(root), platform.WhisperCLIName())
}

func modelsDir(root string) string {
	return filepath.Join(root, "models")
}

func modelFilePath(root, model string) string {
	return filepath.Join(modelsDir(root), ModelFileName(model))
}

func tessdataDir(root string) string {
	return filepath.Join(root, "tessdata")
}

func tessdataFilePath(root, language string) string {
	return filepath.Join(tessdataDir(root), TessdataFileName(language))
}

func manifestFilePath(root string) string {
	return filepath.Join(root, "manifest.json")
}

func stagingDir(root string) string {
	return filepath.Join(root, "staging")
}

func mambaRootDir(root string) string {
	return filepath.Join(root, "mamba-root")
}

// managedBinaryPath looks for a tool inside the conda prefix binary folders,
// trying the platform executable suffix first on Windows.
func managedBinaryPath(root, goos string, names []string) (string, bool) {
	for _, dir := range managedBinDirs(root, goos) {
		for _, name := range names {
			candidates := []string{name}
			if goos == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
				candidates = []string{name + ".exe", name}
			}
			for _, candidate := range candidates {
				path := filepath.Join(dir, candidate)
				if info, err := statFile(path); err == nil && !info.IsDir() {
					return path, true
				}
			}
		}
	}

	return "", false
}
