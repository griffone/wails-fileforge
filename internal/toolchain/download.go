package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	// downloadRequestTimeout bounds one download attempt, body included. Model
	// files are large, so the budget is generous but finite: a stalled
	// connection can no longer hang the install forever.
	downloadRequestTimeout = 30 * time.Minute

	// downloadMaxRetries is the number of extra attempts after the first one.
	// Only transient failures (network errors, 429 and 5xx) are retried.
	downloadMaxRetries = 3
)

// downloadRetryBackoff is the delay before each retry: 1s, 2s and 4s. Tests
// override it through HTTPDownloader.retryBackoff to stay fast.
var downloadRetryBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

// Downloader fetches one URL, or one pinned artifact, into destPath and reports
// byte progress. The callback receives downloaded and total bytes; total is 0
// when neither the server nor the artifact provides a size.
type Downloader interface {
	Download(ctx context.Context, url, destPath string, onProgress func(downloaded, total int64)) error
	DownloadArtifact(ctx context.Context, artifact Artifact, destPath string, onProgress func(downloaded, total int64)) error
}

// HTTPDownloader is the real downloader: HTTPS with the default TLS
// verification, a temp file next to the destination, and an atomic rename so a
// partial or corrupted file is never published.
type HTTPDownloader struct {
	Client *http.Client

	// retryBackoff overrides downloadRetryBackoff in tests. Production leaves
	// it nil and uses the 1s/2s/4s schedule.
	retryBackoff []time.Duration
}

// Download implements Downloader for an unpinned URL. It delegates to
// DownloadArtifact without size or checksum metadata.
func (d *HTTPDownloader) Download(ctx context.Context, url, destPath string, onProgress func(downloaded, total int64)) error {
	return d.DownloadArtifact(ctx, Artifact{URL: url}, destPath, onProgress)
}

// DownloadArtifact downloads one pinned artifact. Each attempt streams the body
// through a hasher while copying, enforces Content-Length when the server sends
// one, and compares the final size and SHA-256 before publishing atomically.
// Transient failures are retried with backoff; checksum and size mismatches are
// terminal so a corrupted upstream response is reported instead of retried.
// Every failure path removes the temporary file, and cancellation aborts both
// the transfer and the retry wait.
func (d *HTTPDownloader) DownloadArtifact(ctx context.Context, artifact Artifact, destPath string, onProgress func(downloaded, total int64)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(artifact.URL) == "" {
		return fmt.Errorf("download URL is required")
	}
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("download destination is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= downloadMaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		attemptContext, cancel := context.WithTimeout(ctx, downloadRequestTimeout)
		err := d.downloadOnce(attemptContext, artifact, destPath, onProgress)
		cancel()
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if !isRetryableDownloadError(err) {
			return err
		}

		lastErr = err
		if attempt == downloadMaxRetries {
			break
		}
		if err := waitForDownloadRetry(ctx, d.retryDelay(attempt)); err != nil {
			return err
		}
	}

	return fmt.Errorf("download %s failed after %d attempts: %w", artifact.URL, downloadMaxRetries+1, lastErr)
}

// downloadOnce performs a single request, verifying the body before publishing
// it over destPath. The temporary file is local to this attempt and removed on
// every failure.
func (d *HTTPDownloader) downloadOnce(ctx context.Context, artifact Artifact, destPath string, onProgress func(downloaded, total int64)) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, artifact.URL, nil)
	if err != nil {
		return fmt.Errorf("prepare download %s: %w", artifact.URL, err)
	}

	response, err := d.httpClient().Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return retryableDownloadError{err: fmt.Errorf("download %s: %w", artifact.URL, err)}
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		statusErr := fmt.Errorf("download %s: unexpected status %s", artifact.URL, response.Status)
		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
			return retryableDownloadError{err: statusErr}
		}

		return statusErr
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

	total := response.ContentLength
	if total <= 0 {
		total = artifact.SizeBytes
	}
	hasher := sha256.New()
	writer := &progressWriter{
		writer:     io.MultiWriter(tempFile, hasher),
		total:      total,
		onProgress: onProgress,
	}

	_, copyErr := io.Copy(writer, response.Body)
	closeErr := tempFile.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if response.ContentLength >= 0 && writer.written != response.ContentLength {
			return retryableDownloadError{err: fmt.Errorf(
				"download %s: received %d bytes, want Content-Length %d: %w",
				artifact.URL, writer.written, response.ContentLength, copyErr,
			)}
		}

		return retryableDownloadError{err: fmt.Errorf("download %s: %w", artifact.URL, copyErr)}
	}
	if closeErr != nil {
		return fmt.Errorf("write %s: %w", destPath, closeErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if response.ContentLength >= 0 && writer.written != response.ContentLength {
		return retryableDownloadError{err: fmt.Errorf(
			"download %s: received %d bytes, want Content-Length %d",
			artifact.URL, writer.written, response.ContentLength,
		)}
	}
	if artifact.SizeBytes > 0 && writer.written != artifact.SizeBytes {
		return fmt.Errorf("download %s: received %d bytes, want %d", artifact.URL, writer.written, artifact.SizeBytes)
	}
	if expected := normalizeSHA256(artifact.SHA256); expected != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if actual != expected {
			return fmt.Errorf("download %s: checksum is %s, want %s", artifact.URL, actual, expected)
		}
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

	// The client timeout is a hard ceiling for one attempt, body included. The
	// caller's context still aborts an install immediately.
	return &http.Client{Timeout: downloadRequestTimeout}
}

// retryDelay returns the backoff before the given retry attempt.
func (d *HTTPDownloader) retryDelay(attempt int) time.Duration {
	schedule := downloadRetryBackoff
	if d != nil && len(d.retryBackoff) > 0 {
		schedule = d.retryBackoff
	}
	if len(schedule) == 0 {
		return 0
	}
	if attempt < len(schedule) {
		return schedule[attempt]
	}

	return schedule[len(schedule)-1]
}

// waitForDownloadRetry waits for delay or for the context to be cancelled.
func waitForDownloadRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryableDownloadError marks a failure that a new attempt may fix: network
// errors, 429/5xx responses and truncated bodies. Size and checksum mismatches
// are deliberately not wrapped in it.
type retryableDownloadError struct {
	err error
}

func (e retryableDownloadError) Error() string {
	return e.err.Error()
}

func (e retryableDownloadError) Unwrap() error {
	return e.err
}

// isRetryableDownloadError reports whether err is transient. A per-attempt
// deadline is transient too, unless the caller's context is already done.
func isRetryableDownloadError(err error) bool {
	var retryable retryableDownloadError
	if errors.As(err, &retryable) {
		return true
	}

	return errors.Is(err, context.DeadlineExceeded)
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
