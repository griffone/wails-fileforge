package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadVerifiesArtifact(t *testing.T) {
	feed := newFeed(t, nil)
	provider := feed.provider(t)

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	dir := t.TempDir()
	downloaded, err := provider.Download(context.Background(), release, dir, nil)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	content, err := os.ReadFile(downloaded.Path)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(content, feed.artifact) {
		t.Fatalf("downloaded bytes do not match the artifact")
	}
	if downloaded.Size != int64(len(feed.artifact)) {
		t.Fatalf("size = %d, want %d", downloaded.Size, len(feed.artifact))
	}
	if downloaded.digest != feed.digest {
		t.Fatalf("digest = %x, want %x", downloaded.digest, feed.digest)
	}
}

func TestDownloadRejectsTamperedArtifact(t *testing.T) {
	feed := newFeed(t, func(options *feedOptions) {
		options.servedArtifact = []byte("tampered bytes that do not match SHA256SUMS\n")
	})
	provider := feed.provider(t)

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	dir := t.TempDir()
	if _, err := provider.Download(context.Background(), release, dir, nil); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Download() error = %v, want ErrDigestMismatch", err)
	}
	assertEmptyDirectory(t, dir)
}

func TestDownloadRejectsWrongKey(t *testing.T) {
	feed := newFeed(t, nil)

	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	provider, err := NewProvider(ProviderConfig{
		Repository: testRepository,
		BaseURL:    feed.server.URL,
		Platform:   "linux",
		Arch:       "amd64",
		PublicKey:  other,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	dir := t.TempDir()
	if _, err := provider.Download(context.Background(), release, dir, nil); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("Download() error = %v, want ErrSignatureInvalid", err)
	}
	assertEmptyDirectory(t, dir)
}

func TestDownloadRequiresADescriptorSignature(t *testing.T) {
	feed := newFeed(t, nil)
	provider := feed.provider(t)

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	release.Verification.Signature = nil

	dir := t.TempDir()
	if _, err := provider.Download(context.Background(), release, dir, nil); !errors.Is(err, ErrSignatureMissing) {
		t.Fatalf("Download() error = %v, want ErrSignatureMissing", err)
	}
	assertEmptyDirectory(t, dir)
}

func TestDownloadRequiresAPinnedKey(t *testing.T) {
	feed := newFeed(t, nil)
	provider, err := NewProvider(ProviderConfig{
		Repository: testRepository,
		BaseURL:    feed.server.URL,
		Platform:   "linux",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	dir := t.TempDir()
	if _, err := provider.Download(context.Background(), release, dir, nil); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Download() error = %v, want ErrNoPublicKey", err)
	}
	assertEmptyDirectory(t, dir)
}

func TestDownloadRemovesThePartialFileOnFailure(t *testing.T) {
	feed := newFeed(t, func(options *feedOptions) { options.partialArtifact = true })
	provider := feed.provider(t)

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	dir := t.TempDir()
	if _, err := provider.Download(context.Background(), release, dir, nil); err == nil {
		t.Fatal("Download() succeeded for a truncated artifact")
	}
	assertEmptyDirectory(t, dir)
}

func TestDownloadReportsProgress(t *testing.T) {
	feed := newFeed(t, nil)
	provider := feed.provider(t)

	release, err := provider.Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	var written []int64
	_, err = provider.Download(context.Background(), release, t.TempDir(), func(current, total int64) {
		written = append(written, current)
		if total != int64(len(feed.artifact)) {
			t.Fatalf("progress total = %d, want %d", total, len(feed.artifact))
		}
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if len(written) == 0 || written[len(written)-1] != int64(len(feed.artifact)) {
		t.Fatalf("progress reports = %v, want a final report of %d", written, len(feed.artifact))
	}
}

// TestEndToEndSignedRelease is the real cryptographic check: a fresh keypair
// signs a fake artifact, a local server publishes the release JSON, SHA256SUMS
// and the detached signature, and the provider downloads and verifies it. The
// tampered server serves different bytes with the same checksums and signature,
// and the download is rejected.
func TestEndToEndSignedRelease(t *testing.T) {
	feed := newFeed(t, func(options *feedOptions) {
		options.tag = "v9.9.9"
		options.artifact = []byte("end to end artifact\n")
	})

	pinned := base64.StdEncoding.EncodeToString(feed.public)
	publicKey, err := ParsePublicKey(pinned)
	if err != nil {
		t.Fatalf("ParsePublicKey() error = %v", err)
	}
	provider, err := NewProvider(ProviderConfig{
		Repository: testRepository,
		BaseURL:    feed.server.URL,
		Platform:   "linux",
		Arch:       "amd64",
		PublicKey:  publicKey,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	release, err := provider.Check(context.Background(), "9.9.9")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	t.Logf("release %s: asset=%s size=%d digest=%x", release.Version, release.Asset.Name, release.Asset.Size, release.Verification.Digest)

	downloaded, err := provider.Download(context.Background(), release, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("verified Download() error = %v", err)
	}
	t.Logf("verified download: %s (%d bytes, sha256=%x)", downloaded.Path, downloaded.Size, downloaded.digest)

	tampered := newFeed(t, func(options *feedOptions) {
		options.tag = "v9.9.9"
		options.artifact = []byte("end to end artifact\n")
		options.servedArtifact = []byte("tampered end to end artifact\n")
	})
	tamperedProvider, err := NewProvider(ProviderConfig{
		Repository: testRepository,
		BaseURL:    tampered.server.URL,
		Platform:   "linux",
		Arch:       "amd64",
		PublicKey:  tampered.public,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	tamperedRelease, err := tamperedProvider.Check(context.Background(), "9.9.9")
	if err != nil {
		t.Fatalf("tampered Check() error = %v", err)
	}
	tamperedDir := t.TempDir()
	if _, err := tamperedProvider.Download(context.Background(), tamperedRelease, tamperedDir, nil); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tampered Download() error = %v, want ErrDigestMismatch", err)
	}
	t.Logf("tampered download rejected with %v; directory entries: %d", ErrDigestMismatch, directoryEntries(t, tamperedDir))
	assertEmptyDirectory(t, tamperedDir)
}

func assertEmptyDirectory(t *testing.T, dir string) {
	t.Helper()
	if entries := directoryEntries(t, dir); entries != 0 {
		t.Fatalf("%s has %d entries after a failed download, want 0", dir, entries)
	}
}

func directoryEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		t.Fatalf("read directory %s: %v", dir, err)
	}
	return len(entries)
}
