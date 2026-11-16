package media

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoopbackServerServesRegisteredMediaWithRange(t *testing.T) {
	baseURL, err := EnsureServer()
	if err != nil {
		t.Fatalf("ensure media server: %v", err)
	}
	if baseURL == "" {
		t.Fatal("expected a loopback media server URL")
	}

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

	response, err := http.Get(baseURL + "/media/" + token)
	if err != nil {
		t.Fatalf("get media: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	if response.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("expected CORS header for the loopback media server")
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(content) {
		t.Fatalf("expected %q, got %q", content, body)
	}

	request, err := http.NewRequest(http.MethodGet, baseURL+"/media/"+token, nil)
	if err != nil {
		t.Fatalf("build range request: %v", err)
	}
	request.Header.Set("Range", "bytes=2-5")

	rangeResponse, err := http.DefaultClient.Do(request)
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
