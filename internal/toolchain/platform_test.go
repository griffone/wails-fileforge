package toolchain

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
			micromambaURL: "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/micromamba-linux-64",
			micromambaBin: "micromamba",
			whisperURL:    "https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-ubuntu-x64.tar.gz",
			whisperKind:   ArchiveKindTarGz,
		},
		{
			name:          "linux arm64",
			goos:          "linux",
			goarch:        "arm64",
			micromambaURL: "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/micromamba-linux-aarch64",
			micromambaBin: "micromamba",
			whisperURL:    "https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-ubuntu-arm64.tar.gz",
			whisperKind:   ArchiveKindTarGz,
		},
		{
			name:          "windows amd64",
			goos:          "windows",
			goarch:        "amd64",
			micromambaURL: "https://github.com/mamba-org/micromamba-releases/releases/download/2.10.0-0/micromamba-win-64.exe",
			micromambaBin: "micromamba.exe",
			whisperURL:    "https://github.com/ggml-org/whisper.cpp/releases/download/b5454/whisper-bin-x64.zip",
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
	const modelBaseURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/"

	tests := map[string]string{
		"small":    modelBaseURL + "ggml-small.bin",
		"medium":   modelBaseURL + "ggml-medium.bin",
		"large-v3": modelBaseURL + "ggml-large-v3.bin",
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
	const tessdataBaseURL = "https://github.com/tesseract-ocr/tessdata_fast/raw/87416418657359cb625c412a48b6e1d6d41c29bd/"

	got, err := TessdataURL("eng")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tessdataBaseURL+"eng.traineddata" {
		t.Fatalf("unexpected eng URL: %q", got)
	}

	got, err = TessdataURL("spa")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tessdataBaseURL+"spa.traineddata" {
		t.Fatalf("unexpected spa URL: %q", got)
	}

	if _, err := TessdataURL(""); err == nil {
		t.Fatal("expected an error for an empty language")
	}
	if _, err := TessdataURL("fra"); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}

func TestPinnedArtifactURLsAreImmutable(t *testing.T) {
	artifacts := make([]Artifact, 0, 10)
	for _, target := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"windows", "amd64"}} {
		platform := PlatformFor(target[0], target[1])
		if !platform.Supported {
			t.Fatalf("%s/%s must be supported", target[0], target[1])
		}
		artifacts = append(artifacts, platform.MicromambaArtifact(), platform.WhisperArtifact())
	}
	for _, model := range SupportedModels() {
		artifact, err := ModelArtifact(model)
		if err != nil {
			t.Fatalf("model artifact %q: %v", model, err)
		}
		artifacts = append(artifacts, artifact)
	}
	for _, language := range SupportedOCRLanguages() {
		artifact, err := TessdataArtifact(language)
		if err != nil {
			t.Fatalf("tessdata artifact %q: %v", language, err)
		}
		artifacts = append(artifacts, artifact)
	}
	artifacts = append(artifacts, RembgModelArtifact())

	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.URL) == "" {
			t.Fatal("every pinned artifact needs a URL")
		}
		if len(artifact.SHA256) != 64 {
			t.Fatalf("%s: SHA-256 %q is not a 64-character digest", artifact.URL, artifact.SHA256)
		}
		if artifact.SizeBytes <= 0 {
			t.Fatalf("%s: size %d is not positive", artifact.URL, artifact.SizeBytes)
		}
		if artifact.FileName == "" {
			t.Fatalf("%s: missing file name", artifact.URL)
		}
		for _, segment := range strings.Split(artifact.URL, "/") {
			if segment == "latest" || segment == "main" {
				t.Fatalf("%s: mutable %q path segment in a pinned artifact URL", artifact.URL, segment)
			}
		}
	}
}

func TestPlatformArtifactMetadata(t *testing.T) {
	linux := PlatformFor("linux", "amd64")
	micromamba := linux.MicromambaArtifact()
	if micromamba.URL != linux.MicromambaURL || micromamba.FileName != "micromamba-linux-64" {
		t.Fatalf("unexpected micromamba artifact: %+v", micromamba)
	}
	if micromamba.SizeBytes != 19028608 || len(micromamba.SHA256) != 64 {
		t.Fatalf("unexpected micromamba metadata: %+v", micromamba)
	}

	whisper := linux.WhisperArtifact()
	if whisper.URL != linux.WhisperURL || whisper.FileName != "whisper-bin-ubuntu-x64.tar.gz" {
		t.Fatalf("unexpected whisper artifact: %+v", whisper)
	}
	if whisper.SizeBytes != 10364195 || len(whisper.SHA256) != 64 {
		t.Fatalf("unexpected whisper metadata: %+v", whisper)
	}

	model, err := ModelArtifact("")
	if err != nil {
		t.Fatalf("default model artifact: %v", err)
	}
	if model.FileName != ModelFileName(DefaultModel) {
		t.Fatalf("default model file = %q", model.FileName)
	}
	if model.SizeBytes != 1533763059 {
		t.Fatalf("default model size = %d", model.SizeBytes)
	}

	unsupported := PlatformFor("darwin", "arm64")
	if artifact := unsupported.MicromambaArtifact(); artifact.URL != "" || artifact.SHA256 != "" {
		t.Fatalf("unsupported platforms must not carry artifact metadata: %+v", artifact)
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
	if filepath.Base(models[0]) != "ggml-medium.bin" || filepath.Base(models[2]) != "ggml-large-v3.bin" {
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

func TestRembgModelMetadata(t *testing.T) {
	spec := defaultRembgModelSpec()
	if got := DefaultRembgModelSpec(); got != spec {
		t.Fatalf("exported model spec = %+v, want %+v", got, spec)
	}
	if spec.Model != "isnet-general-use" {
		t.Fatalf("default model = %q, want isnet-general-use", spec.Model)
	}
	if spec.FileName != "isnet-general-use.onnx" {
		t.Fatalf("default model file = %q, want isnet-general-use.onnx", spec.FileName)
	}
	if spec.URL != "https://github.com/danielgatis/rembg/releases/download/v0.0.0/isnet-general-use.onnx" {
		t.Fatalf("unexpected model URL: %q", spec.URL)
	}
	if spec.SizeBytes != 178648008 {
		t.Fatalf("default model size = %d, want 178648008", spec.SizeBytes)
	}
	if spec.SHA256 != "60920e99c45464f2ba57bee2ad08c919a52bbf852739e96947fbb4358c0d964a" {
		t.Fatalf("default model checksum = %q", spec.SHA256)
	}

	if got := RembgModelFileName("isnet-general-use"); got != "isnet-general-use.onnx" {
		t.Fatalf("model file name = %q", got)
	}
	url, err := RembgModelURL(" ISNET-GENERAL-USE ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != spec.URL {
		t.Fatalf("model URL = %q, want %q", url, spec.URL)
	}
	if _, err := RembgModelURL("bria-rmbg"); err == nil {
		t.Fatal("unvalidated models must be rejected")
	}
	if _, err := RembgModelURL(""); err == nil {
		t.Fatal("an empty model must be rejected")
	}
}

func TestRembgLayoutPaths(t *testing.T) {
	root := filepath.Join("tmp", "fileforge", ToolchainDirName)

	if got := onnxVenvDir(root); got != filepath.Join(root, "onnx-venv") {
		t.Fatalf("shared ONNX venv dir = %q", got)
	}
	if got := onnxVenvPythonPath(root, "linux"); got != filepath.Join(root, "onnx-venv", "bin", "python") {
		t.Fatalf("unix shared ONNX interpreter = %q", got)
	}
	if got := onnxVenvPythonPath(root, "windows"); got != filepath.Join(root, "onnx-venv", "Scripts", "python.exe") {
		t.Fatalf("windows shared ONNX interpreter = %q", got)
	}
	if got := rembgHomeDir(root); got != filepath.Join(root, "rembg") {
		t.Fatalf("rembg home = %q", got)
	}
	wantModel := filepath.Join(root, "rembg", "models", "isnet-general-use", "isnet-general-use.onnx")
	if got := rembgModelPath(root, "isnet-general-use"); got != wantModel {
		t.Fatalf("rembg model path = %q, want %q", got, wantModel)
	}
	if got := onnxReadyFilePath(root); got != filepath.Join(root, "onnx-venv.ready") {
		t.Fatalf("shared ONNX ready marker = %q", got)
	}
}

func TestManagedRembgPathsOnDisk(t *testing.T) {
	dataDir := t.TempDir()
	root := ToolchainDir(dataDir)
	python := onnxVenvPythonPath(root, runtime.GOOS)
	model := rembgModelPath(root, DefaultRembgModel)

	if _, found := ManagedRembgPythonPath(dataDir); found {
		t.Fatal("the managed interpreter must not be reported before the venv exists")
	}
	if _, found := ManagedRembgHome(dataDir); found {
		t.Fatal("the managed home must not be reported before it exists")
	}
	if _, found := ManagedRembgModelPath(dataDir, DefaultRembgModel); found {
		t.Fatal("the managed model must not be reported before it is downloaded")
	}

	writeLayoutFixture(t, python)
	writeLayoutFixture(t, model)
	if err := os.MkdirAll(rembgHomeDir(root), 0o755); err != nil {
		t.Fatalf("create rembg home: %v", err)
	}

	if got, found := ManagedRembgPythonPath(dataDir); !found || got != python {
		t.Fatalf("managed rembg interpreter = %q, %t", got, found)
	}
	if got, found := ManagedRembgHome(dataDir); !found || got != rembgHomeDir(root) {
		t.Fatalf("managed rembg home = %q, %t", got, found)
	}
	if got, found := ManagedRembgModelPath(dataDir, DefaultRembgModel); !found || got != model {
		t.Fatalf("managed rembg model = %q, %t", got, found)
	}
}

func TestRembgRuntimeStateWindowsLayout(t *testing.T) {
	root := t.TempDir()
	venv := filepath.Join(root, "onnx-venv")
	writeLayoutFixture(t, filepath.Join(venv, "Scripts", "python.exe"))
	sitePackages := filepath.Join(venv, "Lib", "site-packages")
	writeLayoutFixture(t, filepath.Join(sitePackages, "rembg", "__init__.py"))
	for _, receipt := range []string{"onnxruntime-1.32.0.dist-info", "numpy-2.3.5.dist-info", "pillow-12.3.0.dist-info"} {
		if err := os.MkdirAll(filepath.Join(sitePackages, receipt), 0o755); err != nil {
			t.Fatalf("create the %s directory: %v", receipt, err)
		}
	}

	if ready, detail := rembgRuntimeState(root, "windows"); ready {
		t.Fatalf("a venv without the marker must not be ready: %s", detail)
	} else if !strings.Contains(detail, "not verified") {
		t.Fatalf("expected a not-verified detail, got %q", detail)
	}
	if ready, _ := rembgRuntimeState(root, "linux"); ready {
		t.Fatal("the Unix layout must not accept the Windows tree")
	}

	writeLayoutFixture(t, onnxReadyFilePath(root))
	if ready, detail := rembgRuntimeState(root, "windows"); !ready {
		t.Fatalf("expected the Windows-shaped engine to be ready, got %q", detail)
	}
	if got, found := onnxSitePackagesDir(root, "windows"); !found || got != sitePackages {
		t.Fatalf("windows site-packages = %q, %t, want %q", got, found, sitePackages)
	}
	if ready, _ := rembgRuntimeState(root, "linux"); ready {
		t.Fatal("the Unix layout must not accept the Windows tree even with a marker")
	}
}

func TestManagedRembgRuntimeState(t *testing.T) {
	dataDir := t.TempDir()
	root := ToolchainDir(dataDir)
	venv := filepath.Join(root, "onnx-venv")
	python := onnxVenvPythonPath(root, runtime.GOOS)
	sitePackages := filepath.Join(venv, "Lib", "site-packages")
	if runtime.GOOS != "windows" {
		sitePackages = filepath.Join(venv, "lib", "python3.11", "site-packages")
	}

	ready, reason := ManagedRembgRuntimeState(dataDir)
	if ready {
		t.Fatal("a missing engine must not be ready")
	}
	if !strings.Contains(reason, "not installed") || !strings.Contains(reason, "Install / repair") {
		t.Fatalf("expected an installed/Settings reason, got %q", reason)
	}

	writeLayoutFixture(t, python)
	ready, reason = ManagedRembgRuntimeState(dataDir)
	if ready {
		t.Fatal("a venv without packages must not be ready")
	}
	if !strings.Contains(reason, "incomplete") || !strings.Contains(reason, "Install / repair") {
		t.Fatalf("expected an incomplete/Settings reason, got %q", reason)
	}

	writeLayoutFixture(t, filepath.Join(sitePackages, "rembg", "__init__.py"))
	for _, receipt := range []string{"onnxruntime-1.32.0.dist-info", "numpy-2.3.5.dist-info", "pillow-12.3.0.dist-info"} {
		if err := os.MkdirAll(filepath.Join(sitePackages, receipt), 0o755); err != nil {
			t.Fatalf("create the %s directory: %v", receipt, err)
		}
	}
	ready, reason = ManagedRembgRuntimeState(dataDir)
	if ready {
		t.Fatal("an unverified venv must not be ready")
	}
	if !strings.Contains(reason, "not verified") || !strings.Contains(reason, "Install / repair") {
		t.Fatalf("expected a not-verified/repair reason, got %q", reason)
	}

	writeLayoutFixture(t, onnxReadyFilePath(root))
	ready, reason = ManagedRembgRuntimeState(dataDir)
	if !ready {
		t.Fatalf("expected the verified engine to be ready, got %q", reason)
	}
	if reason != "Local background removal engine" {
		t.Fatalf("unexpected ready reason %q", reason)
	}
}

func TestRembgModelStateSizeBranches(t *testing.T) {
	root := t.TempDir()
	expectedSize := int64(16)
	original := rembgModelSpec
	rembgModelSpec.SizeBytes = expectedSize
	t.Cleanup(func() {
		rembgModelSpec = original
	})

	path := rembgModelPath(root, DefaultRembgModel)
	if ready, detail := rembgModelState(root); ready {
		t.Fatalf("a missing model must not be ready: %s", detail)
	} else if !strings.Contains(detail, "not installed") {
		t.Fatalf("expected a not-installed detail, got %q", detail)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the model folder: %v", err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(expectedSize)), 0o644); err != nil {
		t.Fatalf("write the exact-size model: %v", err)
	}
	if ready, detail := rembgModelState(root); !ready {
		t.Fatalf("expected an exact-size model to be ready, got %q", detail)
	}

	if err := os.WriteFile(path, []byte("short"), 0o644); err != nil {
		t.Fatalf("write the wrong-size model: %v", err)
	}
	if ready, detail := rembgModelState(root); ready {
		t.Fatalf("a wrong-size model must not be ready: %s", detail)
	} else if !strings.Contains(detail, "incomplete") {
		t.Fatalf("expected an incomplete detail, got %q", detail)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove the model: %v", err)
	}
	if ready, detail := rembgModelState(root); ready || !strings.Contains(detail, "not installed") {
		t.Fatalf("expected a missing-model detail, got %q (ready=%t)", detail, ready)
	}
}
