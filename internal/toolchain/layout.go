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
//	pdf2docx-venv/        dedicated venv for the PDF to DOCX engine
//	rembg-venv/           dedicated venv for the background removal engine
//	rembg/                REMBG_HOME/U2NET_HOME with models/<model>/<model>.onnx
//	upscale-venv/         dedicated venv for the local upscale engine
//	upscale/              UPSCALE_HOME with models/general/<model>.onnx + .data
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

// ManagedPDF2DOCXPythonPath returns the interpreter of the managed PDF to DOCX
// virtual environment for the running OS. It is exported so the PDF to DOCX
// engine runs with the exact package set installed by the toolchain.
func ManagedPDF2DOCXPythonPath(dataDir string) (string, bool) {
	path := pdf2docxVenvPythonPath(ToolchainDir(dataDir), runtime.GOOS)

	return path, fileWithContent(path)
}

// ManagedPDF2DOCXPath returns the pdf2docx console script of the managed
// virtual environment for the running OS.
func ManagedPDF2DOCXPath(dataDir string) (string, bool) {
	path := pdf2docxVenvBinaryPath(ToolchainDir(dataDir), runtime.GOOS)

	return path, fileWithContent(path)
}

// ManagedRembgPythonPath returns the interpreter of the managed background
// removal virtual environment for the running OS. It is exported so the
// background removal engine runs with the exact package set installed by the
// toolchain.
func ManagedRembgPythonPath(dataDir string) (string, bool) {
	path := rembgVenvPythonPath(ToolchainDir(dataDir), runtime.GOOS)

	return path, fileWithContent(path)
}

// ManagedRembgHome returns the managed RemBG home folder, which is also passed
// as REMBG_HOME and U2NET_HOME to the child process.
func ManagedRembgHome(dataDir string) (string, bool) {
	path := rembgHomeDir(ToolchainDir(dataDir))

	return path, directoryExists(path)
}

// ManagedRembgModelPath returns the managed ONNX model file for the requested
// background removal model.
func ManagedRembgModelPath(dataDir, model string) (string, bool) {
	path := rembgModelPath(ToolchainDir(dataDir), model)

	return path, fileWithContent(path)
}

// ManagedUpscalePythonPath returns the interpreter of the managed upscale
// virtual environment for the running OS. The upscale engine runs with the
// exact pinned package set installed by the toolchain.
func ManagedUpscalePythonPath(dataDir string) (string, bool) {
	path := upscaleVenvPythonPath(ToolchainDir(dataDir), runtime.GOOS)

	return path, fileWithContent(path)
}

// ManagedUpscaleModelPath returns the ONNX file of the pinned general upscale
// model when both the graph and its external data file are present with the
// expected sizes.
func ManagedUpscaleModelPath(dataDir string) (string, bool) {
	root := ToolchainDir(dataDir)
	path := upscaleModelONNXPath(root)

	return path, upscaleModelFilesMatch(root)
}

// ManagedUpscaleRuntimeState reports whether the managed upscale engine is
// installed and verified: interpreter, pinned packages and the post-import
// marker. The reason is user-facing and points the user at Settings.
func ManagedUpscaleRuntimeState(dataDir string) (bool, string) {
	ready, reason := upscaleRuntimeState(ToolchainDir(dataDir), runtime.GOOS)
	if ready {
		return true, reason
	}

	return false, strings.Replace(reason, "Run Install / repair.", "Open Settings and run Install / repair.", 1)
}

// ManagedUpscaleModelState reports whether the pinned general upscale model is
// installed and complete. The reason is user-facing.
func ManagedUpscaleModelState(dataDir string) (bool, string) {
	return upscaleModelState(ToolchainDir(dataDir))
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

// pdf2docxVenvDir returns the dedicated virtual environment that hosts the
// pinned PDF to DOCX engine packages. Keeping it outside the conda prefix
// prevents those pins from changing the shared toolchain environment.
func pdf2docxVenvDir(root string) string {
	return filepath.Join(root, "pdf2docx-venv")
}

// managedBasePythonPath returns the managed conda interpreter used to create
// the dedicated virtual environments (PDF to DOCX and background removal).
func managedBasePythonPath(root, goos string) string {
	if goos == "windows" {
		return filepath.Join(envDir(root), "python.exe")
	}

	return filepath.Join(envDir(root), "bin", "python")
}

// pdf2docxVenvPythonPath returns the interpreter inside the PDF to DOCX virtual
// environment.
func pdf2docxVenvPythonPath(root, goos string) string {
	if goos == "windows" {
		return filepath.Join(pdf2docxVenvDir(root), "Scripts", "python.exe")
	}

	return filepath.Join(pdf2docxVenvDir(root), "bin", "python")
}

// pdf2docxVenvBinaryPath returns the pdf2docx console script inside the PDF to
// DOCX virtual environment.
func pdf2docxVenvBinaryPath(root, goos string) string {
	if goos == "windows" {
		return filepath.Join(pdf2docxVenvDir(root), "Scripts", "pdf2docx.exe")
	}

	return filepath.Join(pdf2docxVenvDir(root), "bin", "pdf2docx")
}

// rembgVenvDir returns the dedicated virtual environment that hosts the pinned
// background removal engine packages. It is separate from the conda prefix and
// from the PDF to DOCX environment so a repair of one never breaks the others.
func rembgVenvDir(root string) string {
	return filepath.Join(root, "rembg-venv")
}

// rembgVenvPythonPath returns the interpreter inside the background removal
// virtual environment.
func rembgVenvPythonPath(root, goos string) string {
	if goos == "windows" {
		return filepath.Join(rembgVenvDir(root), "Scripts", "python.exe")
	}

	return filepath.Join(rembgVenvDir(root), "bin", "python")
}

// rembgHomeDir returns the RemBG home folder. Both REMBG_HOME and U2NET_HOME
// point here, and the pinned model files live below it so the application
// never reads weights from the user home directory.
func rembgHomeDir(root string) string {
	return filepath.Join(root, "rembg")
}

// rembgModelPath returns the ONNX file for a background removal model.
func rembgModelPath(root, model string) string {
	return filepath.Join(rembgHomeDir(root), "models", model, RembgModelFileName(model))
}

// rembgReadyFileName marks a background removal virtual environment that was
// installed and imported successfully. It lives in the RemBG home, not inside
// the venv, so a partial venv can never carry the marker by accident.
const rembgReadyFileName = ".ready"

// rembgReadyFilePath returns the readiness marker path.
func rembgReadyFilePath(root string) string {
	return filepath.Join(rembgHomeDir(root), rembgReadyFileName)
}

// rembgSitePackagesDir returns the site-packages folder of the background
// removal virtual environment. Windows keeps it under Lib; Unix nests it below
// lib/pythonX.Y, so the Python version is discovered instead of hardcoded.
func rembgSitePackagesDir(root, goos string) (string, bool) {
	venv := rembgVenvDir(root)
	if goos == "windows" {
		dir := filepath.Join(venv, "Lib", "site-packages")

		return dir, directoryExists(dir)
	}

	matches, err := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages"))
	if err != nil {
		return "", false
	}
	for _, match := range matches {
		if directoryExists(match) {
			return match, true
		}
	}

	return "", false
}

// rembgPackagesInstalled reports whether the venv carries the rembg package and
// a pip receipt for onnxruntime. A missing dist-info directory means the
// install was interrupted, so the environment is never reported as ready.
func rembgPackagesInstalled(root, goos string) bool {
	sitePackages, found := rembgSitePackagesDir(root, goos)
	if !found {
		return false
	}
	if !fileWithContent(filepath.Join(sitePackages, "rembg", "__init__.py")) {
		return false
	}

	matches, err := filepath.Glob(filepath.Join(sitePackages, "onnxruntime-*.dist-info"))
	if err != nil {
		return false
	}
	for _, match := range matches {
		if directoryExists(match) {
			return true
		}
	}

	return false
}

// rembgRuntimeState verifies the background removal engine: the interpreter,
// the installed packages and the marker written after a successful import.
func rembgRuntimeState(root, goos string) (bool, string) {
	if !fileWithContent(rembgVenvPythonPath(root, goos)) {
		return false, "Background removal engine is not installed. Run Install / repair."
	}
	if !rembgPackagesInstalled(root, goos) {
		return false, "Background removal engine is incomplete. Run Install / repair."
	}
	if !fileWithContent(rembgReadyFilePath(root)) {
		return false, "Background removal engine is not verified. Run Install / repair."
	}

	return true, "Local background removal engine"
}

// ManagedRembgRuntimeState reports whether the managed background removal engine
// is installed and verified: interpreter, pinned packages and the post-import
// marker. The reason is user-facing and points the user at Settings.
func ManagedRembgRuntimeState(dataDir string) (bool, string) {
	ready, reason := rembgRuntimeState(ToolchainDir(dataDir), runtime.GOOS)
	if ready {
		return true, reason
	}

	return false, strings.Replace(reason, "Run Install / repair.", "Open Settings and run Install / repair.", 1)
}

// ManagedRembgModelState reports whether the pinned background removal model is
// installed and complete, reusing the exact-size check from the toolchain
// status. The reason is user-facing.
func ManagedRembgModelState(dataDir string) (bool, string) {
	return rembgModelState(ToolchainDir(dataDir))
}

// rembgModelState verifies the pinned model by its exact size. The checksum is
// only verified during install because hashing ~179 MB on every status poll
// would be wasteful.
func rembgModelState(root string) (bool, string) {
	spec := rembgModelSpec
	path := rembgModelPath(root, spec.Model)
	if fileHasExactSize(path, spec.SizeBytes) {
		return true, "Local background removal model"
	}
	if fileWithContent(path) {
		return false, "Background removal model is incomplete. Run Install / repair."
	}

	return false, "Background removal model is not installed. Run Install / repair."
}

// upscaleVenvDir returns the dedicated virtual environment that hosts the
// pinned local upscale engine packages. It is separate from the other
// environments so a repair of one never breaks the others.
func upscaleVenvDir(root string) string {
	return filepath.Join(root, "upscale-venv")
}

// upscaleVenvPythonPath returns the interpreter inside the upscale virtual
// environment.
func upscaleVenvPythonPath(root, goos string) string {
	if goos == "windows" {
		return filepath.Join(upscaleVenvDir(root), "Scripts", "python.exe")
	}

	return filepath.Join(upscaleVenvDir(root), "bin", "python")
}

// upscaleHomeDir returns the upscale home folder. The pinned model files live
// below it so the application never reads weights from the user home
// directory.
func upscaleHomeDir(root string) string {
	return filepath.Join(root, "upscale")
}

// upscaleModelDir returns the folder that holds the pinned general model.
func upscaleModelDir(root string) string {
	return filepath.Join(upscaleHomeDir(root), "models", DefaultUpscaleModel)
}

// upscaleModelONNXPath returns the ONNX graph of the pinned general model. The
// external weights file lives next to it.
func upscaleModelONNXPath(root string) string {
	return filepath.Join(upscaleModelDir(root), upscaleModelSpec.ONNXFileName)
}

// upscaleReadyFileName marks an upscale virtual environment that was installed
// and imported successfully. It lives in the upscale home, not inside the venv,
// so a partial venv can never carry the marker by accident.
const upscaleReadyFileName = ".ready"

// upscaleReadyFilePath returns the readiness marker path.
func upscaleReadyFilePath(root string) string {
	return filepath.Join(upscaleHomeDir(root), upscaleReadyFileName)
}

// upscaleSitePackagesDir returns the site-packages folder of the upscale
// virtual environment.
func upscaleSitePackagesDir(root, goos string) (string, bool) {
	venv := upscaleVenvDir(root)
	if goos == "windows" {
		dir := filepath.Join(venv, "Lib", "site-packages")

		return dir, directoryExists(dir)
	}

	matches, err := filepath.Glob(filepath.Join(venv, "lib", "python*", "site-packages"))
	if err != nil {
		return "", false
	}
	for _, match := range matches {
		if directoryExists(match) {
			return match, true
		}
	}

	return "", false
}

// upscalePackagesInstalled reports whether the venv carries a pip receipt for
// onnxruntime. A missing dist-info directory means the install was interrupted,
// so the environment is never reported as ready.
func upscalePackagesInstalled(root, goos string) bool {
	sitePackages, found := upscaleSitePackagesDir(root, goos)
	if !found {
		return false
	}

	matches, err := filepath.Glob(filepath.Join(sitePackages, "onnxruntime-*.dist-info"))
	if err != nil {
		return false
	}
	for _, match := range matches {
		if directoryExists(match) {
			return true
		}
	}

	return false
}

// upscaleRuntimeState verifies the upscale engine: the interpreter, the
// installed packages and the marker written after a successful import.
func upscaleRuntimeState(root, goos string) (bool, string) {
	if !fileWithContent(upscaleVenvPythonPath(root, goos)) {
		return false, "The upscale engine is not installed. Run Install / repair."
	}
	if !upscalePackagesInstalled(root, goos) {
		return false, "The upscale engine is incomplete. Run Install / repair."
	}
	if !fileWithContent(upscaleReadyFilePath(root)) {
		return false, "The upscale engine is not verified. Run Install / repair."
	}

	return true, "Local upscale engine"
}

// upscaleModelFilesMatch reports whether every pinned model file is present
// with its exact size. The checksum is only verified during install because
// hashing the weights on every status poll would be wasteful.
func upscaleModelFilesMatch(root string) bool {
	spec := upscaleModelSpec
	dir := upscaleModelDir(root)
	for _, file := range spec.Files {
		if !fileHasExactSize(filepath.Join(dir, file.Name), file.SizeBytes) {
			return false
		}
	}

	return len(spec.Files) > 0
}

// upscaleModelState verifies the pinned model by the exact sizes of the graph
// and its external data file.
func upscaleModelState(root string) (bool, string) {
	spec := upscaleModelSpec
	if upscaleModelFilesMatch(root) {
		return true, "Local upscale model"
	}
	for _, file := range spec.Files {
		if fileWithContent(filepath.Join(upscaleModelDir(root), file.Name)) {
			return false, "Upscale model is incomplete. Run Install / repair."
		}
	}

	return false, "Upscale model is not installed. Run Install / repair."
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
