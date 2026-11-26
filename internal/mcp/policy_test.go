package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestStoreAllowDenyListNormalizesAndPersists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	roots, err := store.Roots()
	if err != nil {
		t.Fatalf("read empty roots: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("expected an empty allowlist, got %v", roots)
	}

	exports := filepath.Join(dir, "exports")
	if err := os.MkdirAll(exports, 0o755); err != nil {
		t.Fatalf("create exports: %v", err)
	}

	normalized, err := store.Allow(exports + string(os.PathSeparator) + ".")
	if err != nil {
		t.Fatalf("allow exports: %v", err)
	}
	if normalized != exports {
		t.Fatalf("expected %q, got %q", exports, normalized)
	}

	// A second grant of the same folder, including through a symlink, must not
	// duplicate the stored root.
	if _, err := store.Allow(exports); err != nil {
		t.Fatalf("re-allow exports: %v", err)
	}
	alias := filepath.Join(dir, "exports-alias")
	if err := os.Symlink(exports, alias); err == nil {
		if _, err := store.Allow(alias); err != nil {
			t.Fatalf("allow through alias: %v", err)
		}
	}

	roots, err = store.Roots()
	if err != nil {
		t.Fatalf("read roots: %v", err)
	}
	if len(roots) != 1 || roots[0] != exports {
		t.Fatalf("expected one normalized root %q, got %v", exports, roots)
	}

	var stored Config
	payload, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		t.Fatalf("read mcp.json: %v", err)
	}
	if err := json.Unmarshal(payload, &stored); err != nil {
		t.Fatalf("decode mcp.json: %v", err)
	}
	if stored.Version != ConfigVersion {
		t.Fatalf("expected version %d, got %d", ConfigVersion, stored.Version)
	}
	if len(stored.AllowedRoots) != 1 || stored.AllowedRoots[0] != exports {
		t.Fatalf("unexpected stored roots: %v", stored.AllowedRoots)
	}

	removed, err := store.Deny(alias)
	if err != nil {
		t.Fatalf("deny through alias: %v", err)
	}
	if len(removed) != 1 || removed[0] != exports {
		t.Fatalf("expected the resolved exports root to be removed, got %v", removed)
	}

	roots, err = store.Roots()
	if err != nil {
		t.Fatalf("read roots after deny: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("expected an empty allowlist after deny, got %v", roots)
	}

	// Denying an unknown folder is a no-op.
	removed, err = store.Deny(filepath.Join(dir, "not-allowed"))
	if err != nil {
		t.Fatalf("deny unknown: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removed roots, got %v", removed)
	}
}

func TestStoreAllowRequiresAnExistingDirectory(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Allow(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected a missing folder to fail")
	}

	file := filepath.Join(t.TempDir(), "file.txt")
	writeTestFile(t, file)
	if _, err := store.Allow(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected a not-a-directory error, got %v", err)
	}
}

func TestStoreRejectsUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(ConfigPath(dir), []byte(`{"version":99,"allowedRoots":[]}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store := NewStore(dir)
	if _, err := store.Load(); err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("expected an unsupported version error, got %v", err)
	}
}

func TestPolicyDeniesEverythingWithAnEmptyAllowlist(t *testing.T) {
	policy := NewPolicy(nil)
	err := policy.Check(filepath.Join(t.TempDir(), "file.pdf"))
	if err == nil {
		t.Fatal("expected an empty allowlist to deny the path")
	}

	var denied *DeniedPathError
	if !errors.As(err, &denied) {
		t.Fatalf("expected a DeniedPathError, got %T", err)
	}
	for _, want := range []string{"no allowed roots are configured", "fileforge-cli mcp allow <dir>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q must contain %q", err.Error(), want)
		}
	}
}

func TestPolicyAllowsPathsInsideAGrantedRoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "nested", "input.pdf")
	writeTestFile(t, file)

	policy := NewPolicy([]string{root})
	for _, path := range []string{
		root,
		file,
		filepath.Join(root, "not-created-yet", "output.pdf"),
		filepath.Join(root, "new", "output", "dir"),
	} {
		if err := policy.Check(path); err != nil {
			t.Fatalf("expected %q to be allowed: %v", path, err)
		}
	}
}

func TestPolicyDeniesPathsOutsideAGrantedRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	outsideFile := filepath.Join(outside, "input.pdf")
	writeTestFile(t, outsideFile)

	policy := NewPolicy([]string{root})
	err := policy.Check(outsideFile)
	if err == nil {
		t.Fatal("expected a path outside the grant to be denied")
	}
	for _, want := range []string{root, "outside the allowed roots", "fileforge-cli mcp allow <dir>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("message %q must contain %q", err.Error(), want)
		}
	}
}

func TestPolicyDeniesDotDotEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "allowed")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	outsideFile := filepath.Join(base, "outside", "input.pdf")
	writeTestFile(t, outsideFile)

	// Build the request path with a literal ".." so filepath.Join cannot clean
	// it before the policy sees it.
	escape := root + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "outside" + string(os.PathSeparator) + "input.pdf"

	policy := NewPolicy([]string{root})
	if err := policy.Check(escape); err == nil {
		t.Fatal("expected a .. escape to be denied")
	}
}

func TestPolicyDeniesSymlinkPointingOutside(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	outsideFile := filepath.Join(outside, "input.pdf")
	writeTestFile(t, outsideFile)

	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	policy := NewPolicy([]string{root})
	err := policy.Check(filepath.Join(link, "input.pdf"))
	if err == nil {
		t.Fatal("expected a symlink escape to be denied")
	}
	if !strings.Contains(err.Error(), "outside the allowed roots") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestPolicyAllowsSymlinkTargetInsideTheRoot(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	file := filepath.Join(realDir, "input.pdf")
	writeTestFile(t, file)

	link := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	policy := NewPolicy([]string{root})
	if err := policy.Check(filepath.Join(link, "input.pdf")); err != nil {
		t.Fatalf("expected an inside symlink to be allowed: %v", err)
	}
}

func TestPolicyDeniesDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "missing-target"), link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	policy := NewPolicy([]string{root})
	if err := policy.Check(filepath.Join(link, "output.pdf")); err == nil {
		t.Fatal("expected a dangling symlink to be denied")
	}
}

func TestPolicyDeniesEmptyAndNULPaths(t *testing.T) {
	root := t.TempDir()
	policy := NewPolicy([]string{root})

	if err := policy.Check("   "); err == nil {
		t.Fatal("expected an empty path to be denied")
	}
	if err := policy.Check(root + "/bad\x00name"); err == nil {
		t.Fatal("expected a NUL byte path to be denied")
	}
}
