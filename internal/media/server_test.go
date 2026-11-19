//go:build !production

package media

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoopbackServerServesRegisteredMediaWithRange(t *testing.T) {
	setupRegistry(t)
	baseURL := startTestServer(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	content := []byte("0123456789")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	token, err := Register(path)
	if err != nil {
		t.Fatalf("register media: %v", err)
	}

	request, err := http.NewRequest(http.MethodGet, baseURL+"/media/"+token, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	// The frontend dev server runs on a localhost port, so this is the origin
	// the loopback server must accept in development.
	request.Header.Set("Origin", "http://localhost:9245")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("get media: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:9245" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}
	if got := response.Header.Get("Vary"); !strings.Contains(got, "Origin") {
		t.Fatalf("Vary = %q, want it to include Origin", got)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(content) {
		t.Fatalf("expected %q, got %q", content, body)
	}

	rangeRequest, err := http.NewRequest(http.MethodGet, baseURL+"/media/"+token, nil)
	if err != nil {
		t.Fatalf("build range request: %v", err)
	}
	rangeRequest.Header.Set("Range", "bytes=2-5")

	rangeResponse, err := http.DefaultClient.Do(rangeRequest)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	defer rangeResponse.Body.Close()

	if rangeResponse.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rangeResponse.StatusCode)
	}
	rangeBody, err := io.ReadAll(rangeResponse.Body)
	if err != nil {
		t.Fatalf("read range body: %v", err)
	}
	if strings.TrimSpace(string(rangeBody)) != "2345" {
		t.Fatalf("expected range body 2345, got %q", rangeBody)
	}
}

func TestLoopbackServerRestartsAfterClose(t *testing.T) {
	setupRegistry(t)

	firstURL := startTestServer(t)
	if firstURL == "" {
		t.Fatal("expected a loopback media server URL")
	}

	path := writeTempFile(t, "restart.mp4", "restart body")
	token, err := Register(path)
	if err != nil {
		t.Fatalf("register media: %v", err)
	}

	if body := fetchMedia(t, firstURL, token); body != "restart body" {
		t.Fatalf("first server body = %q, want %q", body, "restart body")
	}

	if err := CloseServer(); err != nil {
		t.Fatalf("CloseServer() error = %v", err)
	}

	secondURL, err := EnsureServer()
	if err != nil {
		t.Fatalf("EnsureServer() after close error = %v", err)
	}
	if secondURL == "" {
		t.Fatal("expected a new loopback media server URL after restart")
	}

	// Registrations survive a restart: the token store is process-wide.
	if body := fetchMedia(t, secondURL, token); body != "restart body" {
		t.Fatalf("second server body = %q, want %q", body, "restart body")
	}

	rangeRequest, err := http.NewRequest(http.MethodGet, secondURL+"/media/"+token, nil)
	if err != nil {
		t.Fatalf("build range request: %v", err)
	}
	rangeRequest.Header.Set("Range", "bytes=0-6")

	rangeResponse, err := http.DefaultClient.Do(rangeRequest)
	if err != nil {
		t.Fatalf("range request: %v", err)
	}
	defer rangeResponse.Body.Close()

	rangeBody, err := io.ReadAll(rangeResponse.Body)
	if err != nil {
		t.Fatalf("read range body: %v", err)
	}
	if string(rangeBody) != "restart" {
		t.Fatalf("range body = %q, want %q", rangeBody, "restart")
	}
}

func TestLoopbackServerRejectsNonReadMethods(t *testing.T) {
	setupRegistry(t)
	baseURL := startTestServer(t)

	token, err := Register(writeTempFile(t, "post.mp4", "body"))
	if err != nil {
		t.Fatalf("register media: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, baseURL+"/media/"+token, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post media: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestCloseServerIsIdempotent(t *testing.T) {
	setupRegistry(t)

	if err := CloseServer(); err != nil {
		t.Fatalf("CloseServer() before start error = %v", err)
	}

	if _, err := EnsureServer(); err != nil {
		t.Fatalf("EnsureServer() error = %v", err)
	}
	if err := CloseServer(); err != nil {
		t.Fatalf("first CloseServer() error = %v", err)
	}
	if err := CloseServer(); err != nil {
		t.Fatalf("second CloseServer() error = %v", err)
	}
}

func TestCORSPolicy(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		wantAllowed bool
	}{
		{name: "localhost dev server", origin: "http://localhost:9245", wantAllowed: true},
		{name: "localhost without port", origin: "http://localhost", wantAllowed: true},
		{name: "loopback ip", origin: "http://127.0.0.1:41234", wantAllowed: true},
		{name: "wails scheme", origin: "wails://wails.localhost", wantAllowed: true},
		{name: "wails custom host", origin: "wails://desktop", wantAllowed: true},
		{name: "wails.localhost", origin: "http://wails.localhost:9245", wantAllowed: true},
		{name: "wails.localhost subdomain", origin: "http://app.wails.localhost", wantAllowed: true},
		{name: "random https origin", origin: "https://evil.example", wantAllowed: false},
		{name: "localhost lookalike", origin: "http://localhost.evil.example", wantAllowed: false},
		{name: "https localhost", origin: "https://localhost:9245", wantAllowed: false},
		{name: "file scheme", origin: "file:///etc/passwd", wantAllowed: false},
		{name: "missing origin", origin: "", wantAllowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/media/token", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			got := rec.Header().Get("Access-Control-Allow-Origin")
			if tt.wantAllowed && got != tt.origin {
				t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tt.origin)
			}
			if !tt.wantAllowed && got != "" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want no allow header", got)
			}
			if vary := rec.Header().Get("Vary"); !strings.Contains(vary, "Origin") {
				t.Fatalf("Vary = %q, want it to include Origin", vary)
			}
		})
	}
}

func TestCORSHandlesPreflight(t *testing.T) {
	nextCalled := false
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/media/token", nil)
	req.Header.Set("Origin", "http://127.0.0.1:9245")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "Range")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:9245" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodGet) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want it to include GET", got)
	}
	if nextCalled {
		t.Fatal("preflight request reached the media handler")
	}

	denied := httptest.NewRequest(http.MethodOptions, "/media/token", nil)
	denied.Header.Set("Origin", "https://evil.example")
	deniedRec := httptest.NewRecorder()
	handler.ServeHTTP(deniedRec, denied)

	if got := deniedRec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed preflight Access-Control-Allow-Origin = %q, want no allow header", got)
	}
}

func startTestServer(t *testing.T) string {
	t.Helper()

	baseURL, err := EnsureServer()
	if err != nil {
		t.Fatalf("EnsureServer() error = %v", err)
	}
	if baseURL == "" {
		t.Fatal("expected a loopback media server URL")
	}

	t.Cleanup(func() {
		if err := CloseServer(); err != nil {
			t.Errorf("CloseServer() error = %v", err)
		}
	})
	return baseURL
}

func fetchMedia(t *testing.T, baseURL, token string) string {
	t.Helper()

	response, err := http.Get(baseURL + "/media/" + token)
	if err != nil {
		t.Fatalf("get media: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(body)
}
