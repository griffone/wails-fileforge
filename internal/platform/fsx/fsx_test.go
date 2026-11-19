package fsx

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestWriteAtomicReplacesContentAndCreatesParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "state.json")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("prepare destination directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("prepare destination: %v", err)
	}

	if err := WriteAtomic(path, []byte("new payload"), 0o644); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(data) != "new payload" {
		t.Fatalf("destination content = %q, want %q", data, "new payload")
	}
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestWriteAtomicCreatesMissingParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "payload.bin")

	if err := WriteAtomic(path, []byte{0x01, 0x02}, 0o600); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if len(data) != 2 || data[0] != 0x01 || data[1] != 0x02 {
		t.Fatalf("destination content = %v, want [1 2]", data)
	}
}

func TestWriteAtomicAppliesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}

	path := filepath.Join(t.TempDir(), "mode.bin")
	if err := WriteAtomic(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("destination mode = %o, want 600", got)
	}
}

func TestWriteAtomicCleansTempFileWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "target")
	if err := os.Mkdir(destination, 0o755); err != nil {
		t.Fatalf("prepare directory destination: %v", err)
	}

	err := WriteAtomic(destination, []byte("data"), 0o644)
	if err == nil {
		t.Fatal("WriteAtomic over a directory returned nil error")
	}
	assertNoTempFiles(t, dir)
}

func TestWriteAtomicRejectsEmptyPath(t *testing.T) {
	if err := WriteAtomic("  ", []byte("data"), 0o644); err == nil {
		t.Fatal("WriteAtomic with an empty path returned nil error")
	}
}

func TestReplaceFileMovesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.tmp")
	dst := filepath.Join(dir, "destination.bin")
	if err := os.WriteFile(src, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(dst, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	if err := ReplaceFile(src, dst); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}

	if Exists(src) {
		t.Fatal("source still exists after ReplaceFile")
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(data) != "fresh" {
		t.Fatalf("destination content = %q, want %q", data, "fresh")
	}
}

func TestReplaceFileMissingSourceReturnsError(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "destination.bin")
	if err := os.WriteFile(dst, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write destination: %v", err)
	}

	if err := ReplaceFile(filepath.Join(dir, "missing.tmp"), dst); err == nil {
		t.Fatal("ReplaceFile with a missing source returned nil error")
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(data) != "stale" {
		t.Fatalf("destination content = %q, want the original %q", data, "stale")
	}
}

func TestEnsureDirCreatesNestedDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "one", "two", "three")
	if err := EnsureDir(dir, 0o755); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if !IsDir(dir) {
		t.Fatalf("%s is not a directory", dir)
	}
	if err := EnsureDir(dir, 0o755); err != nil {
		t.Fatalf("EnsureDir is not idempotent: %v", err)
	}
	if err := EnsureDir(" ", 0o755); err == nil {
		t.Fatal("EnsureDir with an empty path returned nil error")
	}
}

func TestUniqueFilePath(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "report.pdf")

	got, err := UniqueFilePath(original)
	if err != nil {
		t.Fatalf("UniqueFilePath on a free path: %v", err)
	}
	if got != original {
		t.Fatalf("UniqueFilePath = %q, want the original %q", got, original)
	}

	writeFile(t, original)
	got, err = UniqueFilePath(original)
	if err != nil {
		t.Fatalf("UniqueFilePath with one collision: %v", err)
	}
	if want := filepath.Join(dir, "report-1.pdf"); got != want {
		t.Fatalf("UniqueFilePath = %q, want %q", got, want)
	}

	writeFile(t, got)
	got, err = UniqueFilePath(original)
	if err != nil {
		t.Fatalf("UniqueFilePath with two collisions: %v", err)
	}
	if want := filepath.Join(dir, "report-2.pdf"); got != want {
		t.Fatalf("UniqueFilePath = %q, want %q", got, want)
	}
}

func TestUniqueFilePathExhaustsAttempts(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "report.pdf")
	writeFile(t, original)
	for suffix := 1; suffix <= maxUniqueAttempts; suffix++ {
		writeFile(t, filepath.Join(dir, "report-"+strconv.Itoa(suffix)+".pdf"))
	}

	if _, err := UniqueFilePath(original); err == nil {
		t.Fatal("UniqueFilePath returned nil error after exhausting every candidate")
	}
}

func TestUniqueFilePathRejectsEmptyPath(t *testing.T) {
	if _, err := UniqueFilePath(" "); err == nil {
		t.Fatal("UniqueFilePath with an empty path returned nil error")
	}
}

func TestSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "same.bin")
	writeFile(t, path)

	relative, err := filepath.Rel(mustGetwd(t), path)
	if err != nil {
		t.Fatalf("relative path: %v", err)
	}
	if !SameFile(path, relative) {
		t.Fatalf("SameFile(%q, %q) = false, want true", path, relative)
	}
	if SameFile(path, filepath.Join(dir, "other.bin")) {
		t.Fatal("SameFile reported two distinct missing paths as equal")
	}
	if SameFile("", path) {
		t.Fatal("SameFile with an empty path returned true")
	}
}

func TestSameFileFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.bin")
	link := filepath.Join(dir, "link.bin")
	writeFile(t, target)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are not supported: %v", err)
	}

	if !SameFile(link, target) {
		t.Fatal("SameFile did not resolve a symlink to its target")
	}
}

func TestSameFileDetectsHardLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.bin")
	link := filepath.Join(dir, "hardlink.bin")
	writeFile(t, target)
	if err := os.Link(target, link); err != nil {
		t.Skipf("hard links are not supported: %v", err)
	}

	if !SameFile(link, target) {
		t.Fatal("SameFile did not detect a hard link")
	}
}

func TestNormalizePathKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "..", "file.bin")

	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("absolute path: %v", err)
	}
	want := filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		want = strings.ToLower(want)
	}

	if got := NormalizePathKey("  " + path + "  "); got != want {
		t.Fatalf("NormalizePathKey = %q, want %q", got, want)
	}
	if got := NormalizePathKey(""); got != "" {
		t.Fatalf("NormalizePathKey(\"\") = %q, want empty", got)
	}
}

func TestNormalizePathKeyCaseFoldingIsPlatformSpecific(t *testing.T) {
	key := NormalizePathKey("/tmp/FileForge/Report.PDF")
	lowered := strings.ToLower(key)
	if runtime.GOOS == "windows" {
		if key != lowered {
			t.Fatalf("Windows path key = %q, want lower-case %q", key, lowered)
		}
		return
	}
	if key == lowered {
		t.Fatalf("non-Windows path key %q was case-folded", key)
	}
}

func TestExistsAndIsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.bin")
	writeFile(t, file)

	if !Exists(dir) || !Exists(file) {
		t.Fatal("Exists returned false for existing entries")
	}
	if Exists(filepath.Join(dir, "missing")) {
		t.Fatal("Exists returned true for a missing path")
	}
	if !IsDir(dir) {
		t.Fatal("IsDir returned false for a directory")
	}
	if IsDir(file) {
		t.Fatal("IsDir returned true for a regular file")
	}
}

func TestWriteProbe(t *testing.T) {
	dir := t.TempDir()
	if err := WriteProbe(dir); err != nil {
		t.Fatalf("WriteProbe: %v", err)
	}
	assertNoTempFiles(t, dir)

	if err := WriteProbe(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("WriteProbe on a missing directory returned nil error")
	}
	if err := WriteProbe(""); err == nil {
		t.Fatal("WriteProbe with an empty directory returned nil error")
	}
}

func TestWriteProbeFailsOnReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce directory permissions the same way")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(dir, 0o555); err != nil {
		t.Fatalf("create read-only directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := WriteProbe(dir); err == nil {
		t.Fatal("WriteProbe on a read-only directory returned nil error")
	}
}

func TestWriteAtomicWithPatternUsesCustomPattern(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	if err := WriteAtomicWithPattern(path, []byte("{}"), 0o644, ".custom-*.tmp"); err != nil {
		t.Fatalf("WriteAtomicWithPattern: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("destination missing: %v", err)
	}
	assertNoTempFiles(t, dir)
}

func TestWriteAtomicWithInvalidPatternReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	err := WriteAtomicWithPattern(path, []byte("{}"), 0o644, "bad/pattern-*.tmp")
	if err == nil {
		t.Fatal("WriteAtomicWithPattern with a path separator in the pattern returned nil error")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("error = %v, want a pattern validation error", err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read directory %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".fileforge-") || strings.HasSuffix(name, ".tmp") {
			t.Fatalf("temporary file left behind: %s", name)
		}
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	return dir
}
