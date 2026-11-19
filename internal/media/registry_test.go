package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupRegistry resets package state before a test and after it finishes so
// tokens, roots, and TTL never leak between tests.
func setupRegistry(t *testing.T) {
	t.Helper()
	resetForTests()
	t.Cleanup(resetForTests)
}

func TestRegisterAndServe(t *testing.T) {
	setupRegistry(t)

	path := writeTempFile(t, "sample.mp4", "media body")

	token, err := Register(path)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(token) != tokenBytes*2 {
		t.Fatalf("token length = %d, want %d", len(token), tokenBytes*2)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/media/"+token, nil)
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "media body" {
		t.Fatalf("body = %q, want %q", got, "media body")
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want %q", got, "bytes")
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want %q", got, "private, no-store")
	}
}

func TestMiddlewareRangeRequest(t *testing.T) {
	setupRegistry(t)

	token, err := Register(writeTempFile(t, "clip.mp4", "0123456789"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/media/"+token, nil)
	req.Header.Set("Range", "bytes=2-5")
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusPartialContent)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Fatalf("body = %q, want %q", got, "2345")
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range = %q, want %q", got, "bytes 2-5/10")
	}
}

func TestMiddlewareHeadRequest(t *testing.T) {
	setupRegistry(t)

	const content = "0123456789"
	token, err := Register(writeTempFile(t, "head.mp4", content))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/media/"+token, nil)
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "" {
		t.Fatalf("body = %q, want empty body for HEAD", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "10" {
		t.Fatalf("Content-Length = %q, want %q", got, "10")
	}
}

func TestMiddlewareUnknownToken(t *testing.T) {
	setupRegistry(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/media/does-not-exist", nil)
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMiddlewareRejectsNonReadMethods(t *testing.T) {
	setupRegistry(t)

	token, err := Register(writeTempFile(t, "methods.mp4", "body"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/media/"+token, nil)
			Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
			}
			if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
				t.Fatalf("Allow = %q, want %q", got, "GET, HEAD")
			}
			if got := rec.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q, want %q", got, "private, no-store")
			}
		})
	}
}

func TestRegisterRejectsInvalidPaths(t *testing.T) {
	setupRegistry(t)

	if _, err := Register(""); err == nil {
		t.Fatal("Register(\"\") error = nil, want error")
	}
	if _, err := Register("   "); err == nil {
		t.Fatal("Register(blank) error = nil, want error")
	}

	dir := t.TempDir()
	if _, err := Register(dir); err == nil {
		t.Fatal("Register(directory) error = nil, want error")
	}

	missing := filepath.Join(dir, "missing.mp4")
	if _, err := Register(missing); err == nil {
		t.Fatal("Register(missing file) error = nil, want error")
	}
}

func TestRegisterEnforcesAllowedRoots(t *testing.T) {
	setupRegistry(t)

	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outsideDir := filepath.Join(parent, "outside")
	siblingDir := filepath.Join(parent, "root-other")

	insidePath := writeFileIn(t, root, "inside.mp4", "inside")
	nestedPath := writeFileIn(t, filepath.Join(root, "nested"), "nested.mp4", "nested")
	outsidePath := writeFileIn(t, outsideDir, "outside.mp4", "outside")
	siblingPath := writeFileIn(t, siblingDir, "sibling.mp4", "sibling")

	SetAllowedRoots([]string{root})

	for _, path := range []string{insidePath, nestedPath} {
		if _, err := Register(path); err != nil {
			t.Fatalf("Register(%q) error = %v, want allowed", path, err)
		}
	}

	for _, path := range []string{outsidePath, siblingPath} {
		if _, err := Register(path); err == nil {
			t.Fatalf("Register(%q) error = nil, want outside-root rejection", path)
		}
	}
}

func TestRegisterRejectsSymlinkEscapingAllowedRoot(t *testing.T) {
	setupRegistry(t)

	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	outsideDir := filepath.Join(parent, "outside")

	target := writeFileIn(t, outsideDir, "secret.mp4", "secret")
	link := filepath.Join(root, "escape.mp4")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	SetAllowedRoots([]string{root})
	if _, err := Register(link); err == nil {
		t.Fatal("Register(symlink outside root) error = nil, want rejection")
	}
}

func TestRegisterResolvesSymlinkInsideAllowedRoot(t *testing.T) {
	setupRegistry(t)

	root := t.TempDir()
	target := writeFileIn(t, root, "real.mp4", "real")
	link := filepath.Join(root, "link.mp4")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	SetAllowedRoots([]string{root})
	token, err := Register(link)
	if err != nil {
		t.Fatalf("Register(symlink inside root) error = %v", err)
	}

	path, ok := Lookup(token)
	if !ok {
		t.Fatal("token from symlink registration not found")
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if path != resolvedTarget {
		t.Fatalf("stored path = %q, want resolved target %q", path, resolvedTarget)
	}
}

func TestRegisterUsesDefaultRootsWhenUnset(t *testing.T) {
	setupRegistry(t)

	SetAllowedRoots([]string{"", "  "})
	if _, err := Register(writeTempFile(t, "default-roots.mp4", "body")); err != nil {
		t.Fatalf("Register(temp file) error = %v, want default temp root to allow it", err)
	}
}

func TestLookupRejectsExpiredToken(t *testing.T) {
	setupRegistry(t)

	SetTokenTTL(20 * time.Millisecond)
	token, err := Register(writeTempFile(t, "expiring.mp4", "expiring"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if _, ok := Lookup(token); !ok {
		t.Fatal("fresh token was rejected")
	}

	time.Sleep(60 * time.Millisecond)

	if _, ok := Lookup(token); ok {
		t.Fatal("expired token still resolved")
	}

	registry.mu.RLock()
	_, present := registry.entries[token]
	registry.mu.RUnlock()
	if present {
		t.Fatal("expired entry was not deleted on lookup")
	}
}

func TestSweepExpired(t *testing.T) {
	setupRegistry(t)

	path := writeTempFile(t, "sweep.mp4", "sweep")

	SetTokenTTL(20 * time.Millisecond)
	expiring, err := Register(path)
	if err != nil {
		t.Fatalf("Register(expiring) error = %v", err)
	}

	SetTokenTTL(time.Hour)
	live, err := Register(path)
	if err != nil {
		t.Fatalf("Register(live) error = %v", err)
	}

	time.Sleep(60 * time.Millisecond)
	SweepExpired()

	if _, ok := Lookup(expiring); ok {
		t.Fatal("expired token survived SweepExpired")
	}
	if _, ok := Lookup(live); !ok {
		t.Fatal("live token was removed by SweepExpired")
	}
}

func TestUnregisterRevokesToken(t *testing.T) {
	setupRegistry(t)

	token, err := Register(writeTempFile(t, "revoked.mp4", "revoked"))
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	Unregister(token)
	if _, ok := Lookup(token); ok {
		t.Fatal("token still resolves after Unregister")
	}

	// Revoking an unknown token must be a no-op.
	Unregister(token)
	Unregister("not-a-token")
}

func TestLookupRejectsMalformedTokens(t *testing.T) {
	setupRegistry(t)

	for _, token := range []string{
		"",
		"abc",
		strings.Repeat("a", tokenBytes*2-1),
		strings.Repeat("z", tokenBytes*2),
		"../../etc/passwd",
		strings.Repeat("a", tokenBytes*2) + "/",
	} {
		if _, ok := Lookup(token); ok {
			t.Fatalf("Lookup(%q) = true, want false", token)
		}
	}
}

func TestMiddlewareDelegatesNonMediaPaths(t *testing.T) {
	setupRegistry(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	for _, path := range []string{"/", "/index.html", "/media", "/assets/logo.png"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		Middleware(next).ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("path %q status = %d, want %d", path, rec.Code, http.StatusNoContent)
		}
	}
}

func TestRegisterEvictsOldestEntry(t *testing.T) {
	setupRegistry(t)

	path := writeTempFile(t, "oldest.mp4", "oldest")
	oldestToken, err := Register(path)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	for i := 0; i < maxEntries; i++ {
		if _, err := Register(path); err != nil {
			t.Fatalf("Register() #%d error = %v", i, err)
		}
	}

	if _, ok := Lookup(oldestToken); ok {
		t.Fatal("oldest token still registered after reaching the cap")
	}
}

func TestRegisterPrefersExpiredEviction(t *testing.T) {
	setupRegistry(t)

	path := writeTempFile(t, "expired-evict.mp4", "expired")

	SetTokenTTL(time.Nanosecond)
	expiredToken, err := Register(path)
	if err != nil {
		t.Fatalf("Register(expired) error = %v", err)
	}

	SetTokenTTL(time.Hour)
	liveToken, err := Register(path)
	if err != nil {
		t.Fatalf("Register(live) error = %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	// Filling the registry must drop the expired entry before the live one.
	for i := 0; i < maxEntries-1; i++ {
		if _, err := Register(path); err != nil {
			t.Fatalf("Register() #%d error = %v", i, err)
		}
	}

	if _, ok := Lookup(expiredToken); ok {
		t.Fatal("expired token still registered")
	}
	if _, ok := Lookup(liveToken); !ok {
		t.Fatal("live token was evicted while an expired one was available")
	}
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func writeFileIn(t *testing.T, dir, name, content string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create dir %q: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
	return path
}

func TestMiddlewareRejectsNestedPaths(t *testing.T) {
	setupRegistry(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/media/"+strings.Repeat("a", 64)+"/extra", nil)
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
