package outputplan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUniqueFilePathReturnsNaturalName(t *testing.T) {
	dir := t.TempDir()

	got, adjusted := UniqueFilePath(dir, "photo", "webp", map[string]struct{}{})
	want := filepath.Join(dir, "photo.webp")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if adjusted {
		t.Fatal("expected no adjustment for a free name")
	}
}

func TestUniqueFilePathSuffixesExistingFile(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "photo.webp")
	if err := os.WriteFile(existing, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	got, adjusted := UniqueFilePath(dir, "photo", "webp", map[string]struct{}{})
	want := filepath.Join(dir, "photo-2.webp")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if !adjusted {
		t.Fatal("expected adjustment when the natural name exists")
	}

	second, adjusted := UniqueFilePath(dir, "photo", "webp", map[string]struct{}{})
	if second != want {
		t.Fatalf("expected stat-only planning to be deterministic, got %q", second)
	}
	if !adjusted {
		t.Fatal("expected adjustment on the second call")
	}
}

func TestUniqueFilePathSkipsUsedNames(t *testing.T) {
	dir := t.TempDir()
	used := map[string]struct{}{}
	MarkUsed(used, filepath.Join(dir, "photo.webp"))
	MarkUsed(used, filepath.Join(dir, "photo-2.webp"))

	got, adjusted := UniqueFilePath(dir, "photo", "webp", used)
	want := filepath.Join(dir, "photo-3.webp")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if !adjusted {
		t.Fatal("expected adjustment when names are reserved")
	}
}

func TestUniqueFilePathNormalizesExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scan.tiff"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	withDot, _ := UniqueFilePath(dir, "scan", ".TIFF", map[string]struct{}{})
	withoutDot, _ := UniqueFilePath(dir, "scan", "tiff", map[string]struct{}{})

	want := filepath.Join(dir, "scan-2.tiff")
	if withDot != want || withoutDot != want {
		t.Fatalf("expected %q from both spellings, got %q and %q", want, withDot, withoutDot)
	}
}

func TestUniqueDirPathReturnsNaturalName(t *testing.T) {
	dir := t.TempDir()

	got, adjusted := UniqueDirPath(dir, "pages", map[string]struct{}{})
	want := filepath.Join(dir, "pages")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if adjusted {
		t.Fatal("expected no adjustment for a free directory name")
	}
}

func TestUniqueDirPathSuffixesExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "pages"), 0o755); err != nil {
		t.Fatalf("create existing directory: %v", err)
	}

	got, adjusted := UniqueDirPath(dir, "pages", map[string]struct{}{})
	want := filepath.Join(dir, "pages-2")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if !adjusted {
		t.Fatal("expected adjustment when the directory exists")
	}
}

func TestUniqueDirPathSkipsUsedNames(t *testing.T) {
	dir := t.TempDir()
	used := map[string]struct{}{}
	MarkUsed(used, filepath.Join(dir, "pages"))

	got, adjusted := UniqueDirPath(dir, "pages", used)
	want := filepath.Join(dir, "pages-2")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if !adjusted {
		t.Fatal("expected adjustment when the name is reserved")
	}
}
