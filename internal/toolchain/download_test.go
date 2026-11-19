package toolchain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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

// testRetryBackoff keeps retry tests fast without changing the production
// schedule.
var testRetryBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}

func sha256Hex(payload []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func artifactForPayload(url string, payload []byte) Artifact {
	return Artifact{
		URL:       url,
		SHA256:    sha256Hex(payload),
		SizeBytes: int64(len(payload)),
		FileName:  filepath.Base(url),
	}
}

func assertNoDownloadTempFiles(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read destination dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-download-") {
			t.Fatalf("temporary download file was left behind: %s", entry.Name())
		}
	}
}

func TestArtifactVerify(t *testing.T) {
	payload := []byte("artifact payload")
	checksum := sha256Hex(payload)

	tests := []struct {
		name     string
		artifact Artifact
		write    bool
		wantErr  string
	}{
		{
			name:     "matching size and checksum",
			artifact: Artifact{SHA256: checksum, SizeBytes: int64(len(payload))},
			write:    true,
		},
		{
			name:     "upper case checksum",
			artifact: Artifact{SHA256: strings.ToUpper(checksum), SizeBytes: int64(len(payload))},
			write:    true,
		},
		{
			name:     "checksum without pinned size",
			artifact: Artifact{SHA256: checksum},
			write:    true,
		},
		{
			name:     "no pinned metadata",
			artifact: Artifact{},
			write:    true,
		},
		{
			name:     "wrong checksum",
			artifact: Artifact{SHA256: strings.Repeat("0", 64), SizeBytes: int64(len(payload))},
			write:    true,
			wantErr:  "checksum",
		},
		{
			name:     "wrong size",
			artifact: Artifact{SHA256: checksum, SizeBytes: int64(len(payload) + 1)},
			write:    true,
			wantErr:  "bytes",
		},
		{
			name:     "missing file",
			artifact: Artifact{SHA256: checksum, SizeBytes: int64(len(payload))},
			write:    false,
			wantErr:  "stat",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "payload.bin")
			if test.write {
				if err := os.WriteFile(path, payload, 0o644); err != nil {
					t.Fatalf("write payload: %v", err)
				}
			}

			err := test.artifact.Verify(path)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}

				return
			}
			if err == nil {
				t.Fatal("expected a verification error")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error %q does not mention %q", err, test.wantErr)
			}
		})
	}
}

func TestHTTPDownloaderDownloadArtifactVerifiesAndPublishes(t *testing.T) {
	payload := []byte(strings.Repeat("fileforge", 8192))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "models", "ggml-small.bin")
	artifact := artifactForPayload(server.URL+"/ggml-small.bin", payload)

	var lastDownloaded int64
	err := (&HTTPDownloader{Client: server.Client()}).DownloadArtifact(context.Background(), artifact, destPath, func(downloaded, total int64) {
		lastDownloaded = downloaded
		if total != int64(len(payload)) {
			t.Errorf("progress total = %d, want %d", total, len(payload))
		}
	})
	if err != nil {
		t.Fatalf("download artifact: %v", err)
	}

	written, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if string(written) != string(payload) {
		t.Fatalf("downloaded %d bytes, want %d", len(written), len(payload))
	}
	if lastDownloaded != int64(len(payload)) {
		t.Fatalf("final progress = %d, want %d", lastDownloaded, len(payload))
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}

func TestHTTPDownloaderDownloadArtifactChecksumMismatchRemovesTemp(t *testing.T) {
	payload := []byte("served payload")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "model.bin")
	artifact := Artifact{
		URL:       server.URL + "/model.bin",
		SHA256:    sha256Hex([]byte("different payload")),
		SizeBytes: int64(len(payload)),
		FileName:  "model.bin",
	}

	err := (&HTTPDownloader{Client: server.Client(), retryBackoff: testRetryBackoff}).DownloadArtifact(context.Background(), artifact, destPath, nil)
	if err == nil {
		t.Fatal("expected a checksum error")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}

func TestHTTPDownloaderDownloadArtifactSizeMismatchFails(t *testing.T) {
	payload := []byte("short payload")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "model.bin")
	artifact := Artifact{
		URL:       server.URL + "/model.bin",
		SHA256:    sha256Hex(payload),
		SizeBytes: int64(len(payload) + 10),
		FileName:  "model.bin",
	}

	err := (&HTTPDownloader{Client: server.Client(), retryBackoff: testRetryBackoff}).DownloadArtifact(context.Background(), artifact, destPath, nil)
	if err == nil {
		t.Fatal("expected a size error")
	}
	if !strings.Contains(err.Error(), "want") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}

func TestHTTPDownloaderRetriesTransientStatus(t *testing.T) {
	payload := []byte("retried payload")

	for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					http.Error(writer, "transient", status)

					return
				}
				_, _ = writer.Write(payload)
			}))
			defer server.Close()

			destPath := filepath.Join(t.TempDir(), "payload.bin")
			artifact := artifactForPayload(server.URL+"/payload.bin", payload)

			err := (&HTTPDownloader{Client: server.Client(), retryBackoff: testRetryBackoff}).DownloadArtifact(context.Background(), artifact, destPath, nil)
			if err != nil {
				t.Fatalf("download after a transient status: %v", err)
			}
			if got := attempts.Load(); got != 2 {
				t.Fatalf("attempts = %d, want 2", got)
			}
			written, err := os.ReadFile(destPath)
			if err != nil || string(written) != string(payload) {
				t.Fatalf("downloaded payload = %q, %v", written, err)
			}
			assertNoDownloadTempFiles(t, filepath.Dir(destPath))
		})
	}
}

func TestHTTPDownloaderDoesNotRetryNotFound(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(writer, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "missing.bin")
	err := (&HTTPDownloader{Client: server.Client(), retryBackoff: testRetryBackoff}).DownloadArtifact(
		context.Background(),
		Artifact{URL: server.URL + "/missing.bin"},
		destPath,
		nil,
	)
	if err == nil {
		t.Fatal("expected a status error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("a 404 must not be retried, got %d attempts", got)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}

func TestHTTPDownloaderDownloadArtifactCancelRemovesTemp(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(2048))
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(strings.Repeat("x", 1024)))
		if flusher, ok := writer.(http.Flusher); ok {
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
	err := (&HTTPDownloader{Client: server.Client()}).DownloadArtifact(ctx, Artifact{URL: server.URL + "/partial.bin"}, destPath, func(downloaded, _ int64) {
		if downloaded > 0 && !cancelled {
			cancelled = true
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the context error, got %v", err)
	}
	if !cancelled {
		t.Fatal("expected progress before the cancellation")
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file after cancellation, got %v", statErr)
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}

func TestHTTPDownloaderRetryHonorsContextCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		http.Error(writer, "transient", http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	downloader := &HTTPDownloader{
		Client:       server.Client(),
		retryBackoff: []time.Duration{time.Hour},
	}
	result := make(chan error, 1)
	destPath := filepath.Join(t.TempDir(), "never.bin")
	go func() {
		result <- downloader.DownloadArtifact(ctx, Artifact{URL: server.URL + "/never.bin"}, destPath, nil)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the download did not reach the server")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected the context error, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not interrupt the retry wait")
	}

	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}

func TestHTTPDownloaderContentLengthMismatchFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(2048))
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(strings.Repeat("x", 1024)))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "truncated.bin")
	err := (&HTTPDownloader{Client: server.Client(), retryBackoff: testRetryBackoff}).DownloadArtifact(
		context.Background(),
		Artifact{URL: server.URL + "/truncated.bin", SizeBytes: 2048},
		destPath,
		nil,
	)
	if err == nil {
		t.Fatal("expected a Content-Length mismatch error")
	}
	if !strings.Contains(err.Error(), "Content-Length") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file, got %v", statErr)
	}
	assertNoDownloadTempFiles(t, filepath.Dir(destPath))
}
