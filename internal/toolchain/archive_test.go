package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type archiveTestEntry struct {
	name    string
	content string
	mode    os.FileMode
	dir     bool
	link    string
}

func writeTarGzFixture(t *testing.T, path string, entries []archiveTestEntry) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create tar.gz: %v", err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)

	for _, entry := range entries {
		header := &tar.Header{
			Name: entry.name,
			Mode: int64(entry.mode.Perm()),
			Size: int64(len(entry.content)),
		}
		switch {
		case entry.dir:
			header.Typeflag = tar.TypeDir
			header.Size = 0
		case entry.link != "":
			header.Typeflag = tar.TypeSymlink
			header.Linkname = entry.link
			header.Size = 0
		default:
			header.Typeflag = tar.TypeReg
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header %s: %v", entry.name, err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := tarWriter.Write([]byte(entry.content)); err != nil {
				t.Fatalf("write tar content %s: %v", entry.name, err)
			}
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func writeZipFixture(t *testing.T, path string, entries []archiveTestEntry) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)

	for _, entry := range entries {
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(mode)
		if entry.dir {
			header.SetMode(mode | os.ModeDir)
		}

		writerHandle, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", entry.name, err)
		}
		if !entry.dir {
			if _, err := writerHandle.Write([]byte(entry.content)); err != nil {
				t.Fatalf("write zip entry %s: %v", entry.name, err)
			}
		}
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(payload)
}

func TestExtractTarGz(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "whisper.tar.gz")
	writeTarGzFixture(t, archivePath, []archiveTestEntry{
		{name: "whisper-bin-ubuntu-x64/", dir: true, mode: 0o755},
		{name: "whisper-bin-ubuntu-x64/whisper-cli", content: "binary", mode: 0o755},
		{name: "whisper-bin-ubuntu-x64/lib/libwhisper.so.1.9.5", content: "library", mode: 0o644},
		{name: "whisper-bin-ubuntu-x64/lib/libwhisper.so.1", link: "libwhisper.so.1.9.5", mode: 0o777},
	})

	destDir := filepath.Join(t.TempDir(), "extract")
	if err := ExtractTarGz(archivePath, destDir); err != nil {
		t.Fatalf("extract tar.gz: %v", err)
	}

	cliPath := filepath.Join(destDir, "whisper-bin-ubuntu-x64", "whisper-cli")
	if got := readFileString(t, cliPath); got != "binary" {
		t.Fatalf("whisper-cli content = %q", got)
	}
	info, err := os.Stat(cliPath)
	if err != nil {
		t.Fatalf("stat whisper-cli: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected the executable bit, got %v", info.Mode().Perm())
	}

	libPath := filepath.Join(destDir, "whisper-bin-ubuntu-x64", "lib", "libwhisper.so.1")
	linkTarget, err := os.Readlink(libPath)
	if err != nil {
		t.Fatalf("expected the soname symlink to be preserved: %v", err)
	}
	if linkTarget != "libwhisper.so.1.9.5" {
		t.Fatalf("symlink target = %q", linkTarget)
	}
	if got := readFileString(t, libPath); got != "library" {
		t.Fatalf("library content through symlink = %q", got)
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	parent := t.TempDir()
	destDir := filepath.Join(parent, "extract")
	archivePath := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeTarGzFixture(t, archivePath, []archiveTestEntry{
		{name: "../escape.txt", content: "escaped", mode: 0o644},
	})

	err := ExtractTarGz(archivePath, destDir)
	if err == nil {
		t.Fatal("expected a traversal error")
	}
	if !strings.Contains(err.Error(), "escapes the destination") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("the archive wrote outside the destination: %v", statErr)
	}
}

func TestExtractZip(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "whisper.zip")
	writeZipFixture(t, archivePath, []archiveTestEntry{
		{name: "whisper-bin-x64/", dir: true, mode: 0o755},
		{name: "whisper-bin-x64/whisper-cli.exe", content: "binary", mode: 0o755},
		{name: "whisper-bin-x64/whisper.dll", content: "library", mode: 0o644},
	})

	destDir := filepath.Join(t.TempDir(), "extract")
	if err := ExtractZip(archivePath, destDir); err != nil {
		t.Fatalf("extract zip: %v", err)
	}

	if got := readFileString(t, filepath.Join(destDir, "whisper-bin-x64", "whisper-cli.exe")); got != "binary" {
		t.Fatalf("whisper-cli.exe content = %q", got)
	}
	if got := readFileString(t, filepath.Join(destDir, "whisper-bin-x64", "whisper.dll")); got != "library" {
		t.Fatalf("whisper.dll content = %q", got)
	}
}

func TestExtractZipRejectsTraversal(t *testing.T) {
	parent := t.TempDir()
	destDir := filepath.Join(parent, "extract")
	archivePath := filepath.Join(t.TempDir(), "evil.zip")
	writeZipFixture(t, archivePath, []archiveTestEntry{
		{name: "../../escape.txt", content: "escaped", mode: 0o644},
	})

	err := ExtractZip(archivePath, destDir)
	if err == nil {
		t.Fatal("expected a traversal error")
	}
	if _, statErr := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("the archive wrote outside the destination: %v", statErr)
	}
}

func TestExtractZipRejectsWindowsAbsolutePath(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "evil.zip")
	writeZipFixture(t, archivePath, []archiveTestEntry{
		{name: `C:\Windows\evil.txt`, content: "escaped", mode: 0o644},
	})

	if err := ExtractZip(archivePath, t.TempDir()); err == nil {
		t.Fatal("expected an absolute path error")
	}
}

func TestFlattenSingleRoot(t *testing.T) {
	staging := t.TempDir()
	source := filepath.Join(staging, "whisper-bin-ubuntu-x64")
	if err := os.MkdirAll(filepath.Join(source, "lib"), 0o755); err != nil {
		t.Fatalf("create source: %v", err)
	}
	writeArchiveTestFile(t, filepath.Join(source, "whisper-cli"), "binary")
	writeArchiveTestFile(t, filepath.Join(source, "lib", "libwhisper.so.1"), "library")

	dest := filepath.Join(t.TempDir(), "whisper")
	if err := FlattenSingleRoot(staging, dest); err != nil {
		t.Fatalf("flatten: %v", err)
	}

	if got := readFileString(t, filepath.Join(dest, "whisper-cli")); got != "binary" {
		t.Fatalf("whisper-cli content = %q", got)
	}
	if got := readFileString(t, filepath.Join(dest, "lib", "libwhisper.so.1")); got != "library" {
		t.Fatalf("library content = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "whisper-bin-ubuntu-x64")); !os.IsNotExist(err) {
		t.Fatalf("expected the archive root to be flattened, got %v", err)
	}
}

func TestFlattenSingleRootMultipleEntries(t *testing.T) {
	staging := t.TempDir()
	writeArchiveTestFile(t, filepath.Join(staging, "whisper-cli"), "binary")
	writeArchiveTestFile(t, filepath.Join(staging, "lib", "libwhisper.so.1"), "library")

	dest := filepath.Join(t.TempDir(), "whisper")
	if err := FlattenSingleRoot(staging, dest); err != nil {
		t.Fatalf("flatten: %v", err)
	}

	if got := readFileString(t, filepath.Join(dest, "whisper-cli")); got != "binary" {
		t.Fatalf("whisper-cli content = %q", got)
	}
	if got := readFileString(t, filepath.Join(dest, "lib", "libwhisper.so.1")); got != "library" {
		t.Fatalf("library content = %q", got)
	}
}

func TestExtractArchiveUnsupportedKind(t *testing.T) {
	err := ExtractArchive("archive.rar", t.TempDir(), ArchiveKind("rar"))
	if err == nil || !strings.Contains(err.Error(), "unsupported archive kind") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSafeArchiveTarget(t *testing.T) {
	destDir := filepath.Join(t.TempDir(), "dest")

	valid, err := safeArchiveTarget(destDir, "dir/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != filepath.Join(destDir, "dir", "file.txt") {
		t.Fatalf("unexpected target: %q", valid)
	}

	for _, invalid := range []string{"../escape.txt", "/etc/passwd", `C:\Windows\evil`, `\\server\share\file`, "..\\escape.txt"} {
		if _, err := safeArchiveTarget(destDir, invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func writeArchiveTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestExtractTarGzRejectsUnsafeSymlink(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeTarGzFixture(t, archivePath, []archiveTestEntry{
		{name: "root/link", link: "../../etc/passwd", mode: 0o777},
	})

	err := ExtractTarGz(archivePath, filepath.Join(t.TempDir(), "extract"))
	if err == nil {
		t.Fatal("expected an unsafe symlink error")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
}
