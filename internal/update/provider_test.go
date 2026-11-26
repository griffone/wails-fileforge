package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderCheckParsesTheLatestStableRelease(t *testing.T) {
	feed := newFeed(t, nil)

	release, err := feed.provider(t).Check(context.Background(), "1.2.0")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if release.Version != "1.3.0" || release.Tag != "v1.3.0" {
		t.Fatalf("version/tag = %q/%q, want 1.3.0/v1.3.0", release.Version, release.Tag)
	}
	if release.Notes != "Release notes for v1.3.0" {
		t.Fatalf("notes = %q", release.Notes)
	}
	if release.Asset.Name != feed.options.artifactName || release.Asset.Kind != AssetKindAppImage {
		t.Fatalf("asset = %+v", release.Asset)
	}
	if release.Asset.Size != int64(len(feed.artifact)) {
		t.Fatalf("asset size = %d, want %d", release.Asset.Size, len(feed.artifact))
	}
	if !strings.HasSuffix(release.Asset.URL, "/download/"+feed.options.artifactName) {
		t.Fatalf("asset URL = %q", release.Asset.URL)
	}
	if string(release.Verification.Digest) != string(feed.digest[:]) {
		t.Fatalf("digest = %x, want %x", release.Verification.Digest, feed.digest)
	}
	if len(release.Verification.Signature) != 64 {
		t.Fatalf("signature length = %d, want 64", len(release.Verification.Signature))
	}
}

func TestProviderRejectsDraftOrPrerelease(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(*feedOptions)
	}{
		{name: "draft", adjust: func(options *feedOptions) { options.draft = true }},
		{name: "prerelease", adjust: func(options *feedOptions) { options.prerelease = true }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			feed := newFeed(t, test.adjust)
			if _, err := feed.provider(t).Check(context.Background(), "1.2.0"); !errors.Is(err, ErrNotStable) {
				t.Fatalf("Check() error = %v, want ErrNotStable", err)
			}
		})
	}
}

func TestSelectAsset(t *testing.T) {
	asset := func(name string) releaseAsset { return releaseAsset{Name: name, Size: 1} }

	tests := []struct {
		name     string
		assets   []releaseAsset
		platform string
		arch     string
		wantName string
		wantErr  error
	}{
		{
			name: "windows prefers the NSIS installer over the zip",
			assets: []releaseAsset{
				asset("fileforge-desktop-windows-amd64.zip"),
				asset("fileforge-desktop-amd64-installer.exe"),
			},
			platform: "windows",
			arch:     "amd64",
			wantName: "fileforge-desktop-amd64-installer.exe",
		},
		{
			name: "windows falls back to the portable zip",
			assets: []releaseAsset{
				asset("fileforge-desktop-windows-amd64.zip"),
			},
			platform: "windows",
			arch:     "amd64",
			wantName: "fileforge-desktop-windows-amd64.zip",
		},
		{
			name:     "linux uses the AppImage",
			assets:   []releaseAsset{asset("notes.txt"), asset("fileforge-desktop-x86_64.AppImage")},
			platform: "linux",
			arch:     "amd64",
			wantName: "fileforge-desktop-x86_64.AppImage",
		},
		{
			name:     "linux arm64 is unsupported",
			assets:   []releaseAsset{asset("fileforge-desktop-aarch64.AppImage")},
			platform: "linux",
			arch:     "arm64",
			wantErr:  ErrUnsupportedPlatform,
		},
		{
			name:     "darwin is unsupported",
			assets:   []releaseAsset{asset("fileforge-desktop.dmg")},
			platform: "darwin",
			arch:     "amd64",
			wantErr:  ErrUnsupportedPlatform,
		},
		{
			name:     "windows without an installer or zip",
			assets:   []releaseAsset{asset("fileforge-cli.exe")},
			platform: "windows",
			arch:     "amd64",
			wantErr:  ErrAssetMissing,
		},
		{
			name:     "linux without an AppImage",
			assets:   []releaseAsset{asset("fileforge-desktop.deb")},
			platform: "linux",
			arch:     "amd64",
			wantErr:  ErrAssetMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selected, err := selectAsset(test.assets, test.platform, test.arch)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("selectAsset() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectAsset() error = %v", err)
			}
			if selected.Name != test.wantName {
				t.Fatalf("selected %q, want %q", selected.Name, test.wantName)
			}
		})
	}
}

func TestProviderRequiresChecksumsAndSignature(t *testing.T) {
	t.Run("missing SHA256SUMS", func(t *testing.T) {
		feed := newFeed(t, func(options *feedOptions) { options.omitChecksums = true })
		if _, err := feed.provider(t).Check(context.Background(), "1.2.0"); !errors.Is(err, ErrChecksumsMissing) {
			t.Fatalf("Check() error = %v, want ErrChecksumsMissing", err)
		}
	})

	t.Run("missing signature", func(t *testing.T) {
		feed := newFeed(t, func(options *feedOptions) { options.omitSignature = true })
		if _, err := feed.provider(t).Check(context.Background(), "1.2.0"); !errors.Is(err, ErrSignatureMissing) {
			t.Fatalf("Check() error = %v, want ErrSignatureMissing", err)
		}
	})

	t.Run("checksums without the asset", func(t *testing.T) {
		feed := newFeed(t, func(options *feedOptions) {
			options.checksumsBody = []byte(strings.Repeat("0", 64) + "  another-artifact\n")
		})
		if _, err := feed.provider(t).Check(context.Background(), "1.2.0"); !errors.Is(err, ErrChecksumMissing) {
			t.Fatalf("Check() error = %v, want ErrChecksumMissing", err)
		}
	})
}

func TestProviderRejectsMalformedMetadata(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(*feedOptions)
		want   error
	}{
		{
			name:   "invalid JSON",
			adjust: func(options *feedOptions) { options.releaseBody = []byte("{") },
			want:   ErrMalformedRelease,
		},
		{
			name:   "missing tag",
			adjust: func(options *feedOptions) { options.releaseBody = []byte("{}") },
			want:   ErrMalformedRelease,
		},
		{
			name: "garbage checksums",
			adjust: func(options *feedOptions) {
				options.checksumsBody = []byte("not a checksums file\n")
			},
			want: ErrMalformedChecksums,
		},
		{
			name: "signature is not base64",
			adjust: func(options *feedOptions) {
				options.signatureBody = []byte("this is not a signature")
			},
			want: ErrMalformedSignature,
		},
		{
			name: "signature is too short",
			adjust: func(options *feedOptions) {
				options.signatureBody = []byte("c2hvcnQ=\n")
			},
			want: ErrMalformedSignature,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			feed := newFeed(t, test.adjust)
			if _, err := feed.provider(t).Check(context.Background(), "1.2.0"); !errors.Is(err, test.want) {
				t.Fatalf("Check() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestProviderHTTPFailuresAreTyped(t *testing.T) {
	tests := []struct {
		name   string
		adjust func(*feedOptions)
	}{
		{
			name:   "release endpoint",
			adjust: func(options *feedOptions) { options.releaseStatus = http.StatusInternalServerError },
		},
		{
			name: "checksums asset",
			adjust: func(options *feedOptions) {
				options.fail = map[string]int{checksumsAssetName: http.StatusBadGateway}
			},
		},
		{
			name: "signature asset",
			adjust: func(options *feedOptions) {
				options.fail = map[string]int{
					options.artifactName + signatureSuffix: http.StatusNotFound,
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			feed := newFeed(t, test.adjust)
			if _, err := feed.provider(t).Check(context.Background(), "1.2.0"); !errors.Is(err, ErrHTTPStatus) {
				t.Fatalf("Check() error = %v, want ErrHTTPStatus", err)
			}
		})
	}
}

func TestProviderNetworkFailureIsTyped(t *testing.T) {
	feed := newFeed(t, nil)
	provider := feed.provider(t)
	feed.server.Close()

	if _, err := provider.Check(context.Background(), "1.2.0"); !errors.Is(err, ErrNetwork) {
		t.Fatalf("Check() error = %v, want ErrNetwork", err)
	}
}

func TestProviderSendsTokenButNeverLeaksIt(t *testing.T) {
	const token = "ghp_supersecretvalue"
	var received string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		received = request.Header.Get("Authorization")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	provider, err := NewProvider(ProviderConfig{
		Repository: testRepository,
		Token:      token,
		BaseURL:    server.URL,
		Platform:   "linux",
		Arch:       "amd64",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	_, err = provider.Check(context.Background(), "1.2.0")
	if !errors.Is(err, ErrHTTPStatus) {
		t.Fatalf("Check() error = %v, want ErrHTTPStatus", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error leaks the token: %v", err)
	}
	if received != "Bearer "+token {
		t.Fatalf("Authorization = %q, want the bearer token", received)
	}
}

func TestProviderRefusesInvalidRepository(t *testing.T) {
	if _, err := NewProvider(ProviderConfig{Repository: "not-a-repository"}); err == nil {
		t.Fatal("NewProvider accepted a repository without a slash")
	}
}
