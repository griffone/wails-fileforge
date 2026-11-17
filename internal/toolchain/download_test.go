package toolchain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestHTTPDownloaderWritesFileAndReportsProgress(t *testing.T) {
	payload := []byte(strings.Repeat("fileforge", 8192))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "models", "ggml-small.bin")
	var progress []struct {
		downloaded int64
		total      int64
	}

	downloader := &HTTPDownloader{Client: server.Client()}
	if err := downloader.Download(context.Background(), server.URL+"/model.bin", destPath, func(downloaded, total int64) {
		progress = append(progress, struct {
			downloaded int64
			total      int64
		}{downloaded: downloaded, total: total})
	}); err != nil {
		t.Fatalf("download: %v", err)
	}

	written, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if string(written) != string(payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(written), len(payload))
	}
	if len(progress) == 0 {
		t.Fatal("expected at least one progress callback")
	}
	last := progress[len(progress)-1]
	if last.downloaded != int64(len(payload)) || last.total != int64(len(payload)) {
		t.Fatalf("unexpected final progress: %+v", last)
	}

	entries, err := os.ReadDir(filepath.Dir(destPath))
	if err != nil {
		t.Fatalf("read destination dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-download-") {
			t.Fatalf("temporary download file was left behind: %s", entry.Name())
		}
	}
}

func TestHTTPDownloaderReportsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "missing.bin")
	err := (&HTTPDownloader{Client: server.Client()}).Download(context.Background(), server.URL+"/missing.bin", destPath, nil)
	if err == nil {
		t.Fatal("expected a status error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
}

func TestHTTPDownloaderCancelRemovesPartialFile(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(2048))
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(strings.Repeat("x", 1024)))
		flusher, ok := writer.(http.Flusher)
		if ok {
			flusher.Flush()
		}

		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	destPath := filepath.Join(t.TempDir(), "partial.bin")
	cancelled := false
	err := (&HTTPDownloader{Client: server.Client()}).Download(ctx, server.URL+"/partial.bin", destPath, func(downloaded, _ int64) {
		if downloaded > 0 && !cancelled {
			cancelled = true
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if !cancelled {
		t.Fatal("expected progress before the cancellation")
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file after cancellation, got %v", statErr)
	}

	entries, readErr := os.ReadDir(filepath.Dir(destPath))
	if readErr != nil {
		t.Fatalf("read destination dir: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-download-") {
			t.Fatalf("temporary download file was left behind: %s", entry.Name())
		}
	}
}

func TestHTTPDownloaderHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	destPath := filepath.Join(t.TempDir(), "never.bin")
	err := (&HTTPDownloader{}).Download(ctx, "http://127.0.0.1:1/never.bin", destPath, nil)
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
}

func TestHTTPDownloaderRequiresURLAndDestination(t *testing.T) {
	downloader := &HTTPDownloader{}
	if err := downloader.Download(context.Background(), "", filepath.Join(t.TempDir(), "file"), nil); err == nil {
		t.Fatal("expected an error for a missing URL")
	}
	if err := downloader.Download(context.Background(), "https://example.com/file", "", nil); err == nil {
		t.Fatal("expected an error for a missing destination")
	}
}
