package media

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterAndServe(t *testing.T) {
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
}

func TestMiddlewareRangeRequest(t *testing.T) {
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

func TestMiddlewareUnknownToken(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/media/does-not-exist", nil)
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRegisterRejectsInvalidPaths(t *testing.T) {
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

func TestMiddlewareDelegatesNonMediaPaths(t *testing.T) {
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

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestMiddlewareRejectsNestedPaths(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/media/"+strings.Repeat("a", 64)+"/extra", nil)
	Middleware(http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
