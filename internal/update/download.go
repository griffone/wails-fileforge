package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// ProgressFunc reports download progress. total is the expected size from the
// release descriptor and may be zero when the feed did not publish it.
type ProgressFunc func(written, total int64)

// Downloaded is a verified artifact on disk. It exists only after both the
// SHA256SUMS digest and the ed25519 signature matched.
type Downloaded struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	// digest is unexported so the binding generator only publishes the path
	// and size. Same-package tests assert it.
	digest [32]byte
}

// Download streams the selected artifact into dir, hashing while it writes,
// then verifies it against the descriptor. The order is fixed:
//
//  1. a pinned key must exist, or nothing is downloaded at all;
//  2. the stream is hashed and written to a partial file;
//  3. the SHA-256 digest must equal the one from SHA256SUMS;
//  4. the detached signature must verify over that digest.
//
// Any failure removes the partial file and returns a typed error. There is no
// path where unverified bytes are left behind for the caller to install.
func (p *Provider) Download(ctx context.Context, release *Release, dir string, onProgress ProgressFunc) (*Downloaded, error) {
	if len(p.publicKey) != ed25519.PublicKeySize {
		return nil, ErrNoPublicKey
	}
	if release == nil {
		return nil, ErrNoRelease
	}
	if len(release.Verification.Digest) != sha256.Size {
		return nil, fmt.Errorf("%w: descriptor has no sha256 digest", ErrMalformedChecksums)
	}
	if len(release.Verification.Signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("%w: descriptor has no 64-byte signature", ErrSignatureMissing)
	}

	name := filepath.Base(release.Asset.Name)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return nil, fmt.Errorf("%w: artifact name %q", ErrMalformedRelease, release.Asset.Name)
	}
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, err)
	}
	path := filepath.Join(dir, name)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDownloadFailed, err)
	}

	digest, written, err := p.streamTo(ctx, release, file, onProgress)
	closeErr := file.Close()
	if err == nil && closeErr != nil {
		err = fmt.Errorf("%w: %v", ErrDownloadFailed, closeErr)
	}
	if err == nil && subtle.ConstantTimeCompare(digest[:], release.Verification.Digest) != 1 {
		err = ErrDigestMismatch
	}
	if err == nil && !ed25519.Verify(p.publicKey, digest[:], release.Verification.Signature) {
		err = ErrSignatureInvalid
	}
	if err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	return &Downloaded{Path: path, Size: written, digest: digest}, nil
}

// streamTo copies the artifact body into dst while hashing it.
func (p *Provider) streamTo(ctx context.Context, release *Release, dst io.Writer, onProgress ProgressFunc) ([32]byte, int64, error) {
	var digest [32]byte
	hasher := sha256.New()
	writer := io.MultiWriter(dst, hasher)

	request, err := p.newRequest(ctx, release.Asset.URL, "application/octet-stream")
	if err != nil {
		return digest, 0, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return digest, 0, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return digest, 0, fmt.Errorf("%w: %s returned %s", ErrHTTPStatus, release.Asset.URL, response.Status)
	}

	total := release.Asset.Size
	if total <= 0 && response.ContentLength > 0 {
		total = response.ContentLength
	}

	buffer := make([]byte, 32*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return digest, written, fmt.Errorf("%w: %v", ErrDownloadFailed, err)
		}

		read, readErr := response.Body.Read(buffer)
		if read > 0 {
			if _, writeErr := writer.Write(buffer[:read]); writeErr != nil {
				return digest, written, fmt.Errorf("%w: %v", ErrDownloadFailed, writeErr)
			}
			written += int64(read)
			if onProgress != nil {
				onProgress(written, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return digest, written, fmt.Errorf("%w: %v", ErrNetwork, readErr)
		}
	}

	copy(digest[:], hasher.Sum(nil))
	return digest, written, nil
}
