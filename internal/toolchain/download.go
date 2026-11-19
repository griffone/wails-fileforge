package toolchain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fileforge-desktop/internal/platform/fsx"
)

const (
	downloadTempPattern = ".fileforge-download-*"
	downloadFileMode    = os.FileMode(0o644)
	progressInterval    = 200 * time.Millisecond
)

// Downloader fetches one URL into destPath and reports byte progress. The
// callback receives downloaded and total bytes; total is 0 when the server does
// not send a Content-Length.
type Downloader interface {
	Download(ctx context.Context, url, destPath string, onProgress func(downloaded, total int64)) error
}

// HTTPDownloader is the real downloader: HTTPS with the default TLS
// verification, a temp file next to the destination, and an atomic rename so a
// partial file is never published.
type HTTPDownloader struct {
	Client *http.Client
}

// Download implements Downloader. Cancellation comes from ctx and aborts the
// request; the temp file is removed and the destination is left untouched.
func (d *HTTPDownloader) Download(ctx context.Context, url, destPath string, onProgress func(downloaded, total int64)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("download URL is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("download destination is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("prepare download %s: %w", url, err)
	}

	response, err := d.httpClient().Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("download %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", url, response.Status)
	}

	destinationDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", destinationDir, err)
	}

	tempFile, err := os.CreateTemp(destinationDir, downloadTempPattern)
	if err != nil {
		return fmt.Errorf("create a download file in %s: %w", destinationDir, err)
	}
	tempPath := tempFile.Name()
	published := false
	defer func() {
		_ = tempFile.Close()
		if !published {
			_ = os.Remove(tempPath)
		}
	}()

	writer := &progressWriter{
		writer:     tempFile,
		total:      response.ContentLength,
		onProgress: onProgress,
	}

	_, copyErr := io.Copy(writer, response.Body)
	closeErr := tempFile.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("download %s: %w", url, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("write %s: %w", destPath, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Chmod(tempPath, downloadFileMode); err != nil {
		return fmt.Errorf("prepare %s: %w", destPath, err)
	}
	if err := fsx.ReplaceFile(tempPath, destPath); err != nil {
		return fmt.Errorf("publish %s: %w", destPath, err)
	}

	published = true
	writer.report(true)

	return nil
}

func (d *HTTPDownloader) httpClient() *http.Client {
	if d != nil && d.Client != nil {
		return d.Client
	}

	// No overall timeout: model downloads are large and progress is driven by
	// the caller's context. TLS verification stays at the Go default.
	return &http.Client{}
}

// progressWriter reports copy progress at a bounded rate so a fast download
// does not flood the event stream.
type progressWriter struct {
	writer     io.Writer
	total      int64
	written    int64
	onProgress func(downloaded, total int64)
	lastEmit   time.Time
}

func (w *progressWriter) Write(payload []byte) (int, error) {
	written, err := w.writer.Write(payload)
	w.written += int64(written)
	w.report(false)

	return written, err
}

func (w *progressWriter) report(force bool) {
	if w.onProgress == nil {
		return
	}

	now := time.Now()
	if !force && !w.lastEmit.IsZero() && now.Sub(w.lastEmit) < progressInterval {
		return
	}

	w.lastEmit = now
	w.onProgress(w.written, w.total)
}
