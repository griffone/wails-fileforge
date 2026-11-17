package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlatformForSupportedTargets(t *testing.T) {
	tests := []struct {
		name          string
		goos          string
		goarch        string
		micromambaURL string
		micromambaBin string
		whisperURL    string
		whisperKind   ArchiveKind
	}{
		{
			name:          "linux amd64",
			goos:          "linux",
			goarch:        "amd64",
			micromambaURL: "https://github.com/mamba-org/micromamba-releases/releases/latest/download/micromamba-linux-64",
			micromambaBin: "micromamba",
			whisperURL:    "https://github.com/ggerganov/whisper.cpp/releases/download/b5454/whisper-bin-ubuntu-x64.tar.gz",
			whisperKind:   ArchiveKindTarGz,
		},
		{
			name:          "linux arm64",
			goos:          "linux",
			goarch:        "arm64",
			micromambaURL: "https://github.com/mamba-org/micromamba-releases/releases/latest/download/micromamba-linux-aarch64",
			micromambaBin: "micromamba",
			whisperURL:    "https://github.com/ggerganov/whisper.cpp/releases/download/b5454/whisper-bin-ubuntu-arm64.tar.gz",
			whisperKind:   ArchiveKindTarGz,
		},
		{
			name:          "windows amd64",
			goos:          "windows",
			goarch:        "amd64",
			micromambaURL: "https://github.com/mamba-org/micromamba-releases/releases/latest/download/micromamba-win-64.exe",
			micromambaBin: "micromamba.exe",
			whisperURL:    "https://github.com/ggerganov/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip",
			whisperKind:   ArchiveKindZip,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			platform := PlatformFor(test.goos, test.goarch)
			if !platform.Supported {
				t.Fatalf("expected %s/%s to be supported, got %q", test.goos, test.goarch, platform.Reason)
			}
			if platform.OS != test.goos || platform.Arch != test.goarch {
				t.Fatalf("unexpected platform identity: %+v", platform)
			}
			if platform.MicromambaURL != test.micromambaURL {
				t.Fatalf("micromamba URL = %q, want %q", platform.MicromambaURL, test.micromambaURL)
			}
			if platform.MicromambaBinary != test.micromambaBin {
				t.Fatalf("micromamba binary = %q, want %q", platform.MicromambaBinary, test.micromambaBin)
			}
			if platform.WhisperURL != test.whisperURL {
				t.Fatalf("whisper URL = %q, want %q", platform.WhisperURL, test.whisperURL)
			}
			if platform.WhisperArchive != test.whisperKind {
				t.Fatalf("whisper archive = %q, want %q", platform.WhisperArchive, test.whisperKind)
			}
		})
	}
}

func TestPlatformForUnsupportedTargets(t *testing.T) {
	tests := []struct {
		name        string
		goos        string
		goarch      string
		reasonMatch string
	}{
		{name: "windows arm64", goos: "windows", goarch: "arm64", reasonMatch: "Windows on ARM"},
		{name: "darwin amd64", goos: "darwin", goarch: "amd64", reasonMatch: "macOS"},
		{name: "darwin arm64", goos: "darwin", goarch: "arm64", reasonMatch: "macOS"},
		{name: "linux 386", goos: "linux", goarch: "386", reasonMatch: "linux/386"},
		{name: "freebsd amd64", goos: "freebsd", goarch: "amd64", reasonMatch: "freebsd/amd64"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			platform := PlatformFor(test.goos, test.goarch)
			if platform.Supported {
				t.Fatalf("expected %s/%s to be unsupported", test.goos, test.goarch)
			}
			if platform.Reason == "" {
				t.Fatal("expected a clear unsupported reason")
			}
			if !strings.Contains(platform.Reason, test.reasonMatch) {
				t.Fatalf("reason %q does not mention %q", platform.Reason, test.reasonMatch)
			}
			if platform.MicromambaURL != "" || platform.WhisperURL != "" {
				t.Fatalf("unsupported platforms must not carry URLs: %+v", platform)
			}
		})
	}
}

func TestPlatformNames(t *testing.T) {
	linux := PlatformFor("linux", "amd64")
	if linux.ExeSuffix() != "" || linux.WhisperCLIName() != "whisper-cli" {
		t.Fatalf("unexpected linux names: %q %q", linux.ExeSuffix(), linux.WhisperCLIName())
	}

	windows := PlatformFor("windows", "amd64")
	if windows.ExeSuffix() != ".exe" || windows.WhisperCLIName() != "whisper-cli.exe" {
		t.Fatalf("unexpected windows names: %q %q", windows.ExeSuffix(), windows.WhisperCLIName())
	}
	if windows.MicromambaName() != "micromamba.exe" {
		t.Fatalf("unexpected micromamba name: %q", windows.MicromambaName())
	}
}

func TestDataDirFor(t *testing.T) {
	envWith := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	home := func(path string) func() (string, error) {
		return func() (string, error) { return path, nil }
	}
	cache := func(path string) func() (string, error) {
		return func() (string, error) { return path, nil }
	}

	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		homeDir func() (string, error)
		cache   func() (string, error)
		want    string
	}{
		{
			name:    "linux xdg data home",
			goos:    "linux",
			env:     map[string]string{"XDG_DATA_HOME": "/data/xdg"},
			homeDir: home("/home/ivan"),
			want:    filepath.Join("/data/xdg", "fileforge"),
		},
		{
			name:    "linux default",
			goos:    "linux",
			env:     map[string]string{},
			homeDir: home("/home/ivan"),
			want:    filepath.Join("/home/ivan", ".local", "share", "fileforge"),
		},
		{
			name:    "windows local app data",
			goos:    "windows",
			env:     map[string]string{"LOCALAPPDATA": `C:\Users\ivan\AppData\Local`},
			homeDir: home(`C:\Users\ivan`),
			want:    filepath.Join(`C:\Users\ivan\AppData\Local`, "FileForge"),
		},
		{
			name:    "windows cache fallback",
			goos:    "windows",
			env:     map[string]string{},
			homeDir: home(`C:\Users\ivan`),
			cache:   cache(`C:\Users\ivan\AppData\Local`),
			want:    filepath.Join(`C:\Users\ivan\AppData\Local`, "FileForge"),
		},
		{
			name:    "macOS application support",
			goos:    "darwin",
			env:     map[string]string{},
			homeDir: home("/Users/ivan"),
			want:    filepath.Join("/Users/ivan", "Library", "Application Support", "FileForge"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cacheDir := test.cache
			if cacheDir == nil {
				cacheDir = cache("/unused/cache")
			}
			got, err := dataDirFor(test.goos, envWith(test.env), test.homeDir, cacheDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.want {
				t.Fatalf("data dir = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDataDirForHomeErrors(t *testing.T) {
	homeError := func() (string, error) { return "", errors.New("no home") }
	cacheError := func() (string, error) { return "", errors.New("no cache") }

	for _, goos := range []string{"linux", "darwin"} {
		if _, err := dataDirFor(goos, func(string) string { return "" }, homeError, cacheError); err == nil {
			t.Fatalf("%s: expected a home directory error", goos)
		}
	}

	if _, err := dataDirFor("windows", func(string) string { return "" }, homeError, cacheError); err == nil {
		t.Fatal("windows: expected a cache directory error")
	}
}

func TestNormalizeModel(t *testing.T) {
	for _, model := range SupportedModels() {
		normalized, err := NormalizeModel(model)
		if err != nil || normalized != model {
			t.Fatalf("model %q: got %q, %v", model, normalized, err)
		}
	}

	if normalized, err := NormalizeModel(""); err != nil || normalized != DefaultModel {
		t.Fatalf("empty model should default to %q, got %q, %v", DefaultModel, normalized, err)
	}
	if _, err := NormalizeModel("tiny"); err == nil {
		t.Fatal("expected an error for an unsupported model")
	}
}

func TestModelURLs(t *testing.T) {
	tests := map[string]string{
		"small":    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
		"medium":   "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.bin",
		"large-v3": "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3.bin",
	}

	for model, want := range tests {
		got, err := ModelURL(model)
		if err != nil {
			t.Fatalf("model %q: unexpected error: %v", model, err)
		}
		if got != want {
			t.Fatalf("model %q URL = %q, want %q", model, got, want)
		}
	}

	if ModelFileName("small") != "ggml-small.bin" {
		t.Fatalf("unexpected model file name: %q", ModelFileName("small"))
	}
}

func TestNormalizeOCRLanguages(t *testing.T) {
	languages, err := NormalizeOCRLanguages(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(languages) != 2 || languages[0] != "eng" || languages[1] != "spa" {
		t.Fatalf("unexpected default languages: %v", languages)
	}

	languages, err = NormalizeOCRLanguages([]string{"SPA", "eng", "spa"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(languages) != 2 || languages[0] != "spa" || languages[1] != "eng" {
		t.Fatalf("expected deduped normalized languages, got %v", languages)
	}

	if _, err := NormalizeOCRLanguages([]string{"fra"}); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}

func TestTessdataURLs(t *testing.T) {
	got, err := TessdataURL("eng")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://github.com/tesseract-ocr/tessdata_fast/raw/main/eng.traineddata" {
		t.Fatalf("unexpected eng URL: %q", got)
	}

	got, err = TessdataURL("spa")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://github.com/tesseract-ocr/tessdata_fast/raw/main/spa.traineddata" {
		t.Fatalf("unexpected spa URL: %q", got)
	}

	if _, err := TessdataURL(""); err == nil {
		t.Fatal("expected an error for an empty language")
	}
	if _, err := TessdataURL("fra"); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}

func TestManagedPaths(t *testing.T) {
	dataDir := filepath.Join("tmp", "fileforge")
	root := filepath.Join(dataDir, ToolchainDirName)

	if got := ToolchainDir(dataDir); got != root {
		t.Fatalf("toolchain dir = %q, want %q", got, root)
	}
	if got := ManagedWhisperCLIPath(dataDir); !strings.HasSuffix(got, filepath.Join("whisper", "whisper-cli")) {
		t.Fatalf("unexpected managed whisper path: %q", got)
	}

	models := ManagedModelPaths(dataDir)
	if len(models) != 3 {
		t.Fatalf("expected three managed model paths, got %d", len(models))
	}
	if filepath.Base(models[0]) != "ggml-small.bin" || filepath.Base(models[2]) != "ggml-large-v3.bin" {
		t.Fatalf("unexpected managed model order: %v", models)
	}
}

func writeLayoutFixture(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestManagedBinaryPathWindowsLayout(t *testing.T) {
	root := t.TempDir()
	ffmpeg := filepath.Join(root, "env", "Library", "bin", "ffmpeg.exe")
	tesseract := filepath.Join(root, "env", "Scripts", "tesseract.exe")
	ghostscript := filepath.Join(root, "env", "Library", "bin", "gswin64c.exe")
	writeLayoutFixture(t, ffmpeg)
	writeLayoutFixture(t, tesseract)
	writeLayoutFixture(t, ghostscript)

	tests := []struct {
		names []string
		want  string
	}{
		{names: []string{"ffmpeg"}, want: ffmpeg},
		{names: []string{"tesseract"}, want: tesseract},
		{names: []string{"gs", "gswin64c", "gswin32c"}, want: ghostscript},
	}

	for _, test := range tests {
		path, found := managedBinaryPath(root, "windows", test.names)
		if !found {
			t.Fatalf("expected %v to be found", test.names)
		}
		if path != test.want {
			t.Fatalf("managed binary for %v = %q, want %q", test.names, path, test.want)
		}
	}

	if _, found := managedBinaryPath(root, "windows", []string{"pdftoppm"}); found {
		t.Fatal("expected pdftoppm to be missing")
	}
}

func TestManagedBinaryPathUnixLayout(t *testing.T) {
	root := t.TempDir()
	ffmpeg := filepath.Join(root, "env", "bin", "ffmpeg")
	writeLayoutFixture(t, ffmpeg)

	path, found := managedBinaryPath(root, "linux", []string{"ffmpeg"})
	if !found || path != ffmpeg {
		t.Fatalf("managed ffmpeg = %q, %t", path, found)
	}

	windowsPath := filepath.Join(root, "env", "Library", "bin", "ffmpeg")
	writeLayoutFixture(t, windowsPath)
	if path, found := managedBinaryPath(root, "linux", []string{"ffmpeg"}); !found || path != ffmpeg {
		t.Fatalf("linux must not use the Windows layout: %q, %t", path, found)
	}
}
