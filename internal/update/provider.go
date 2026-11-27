package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// DefaultRepository is the project the release feed points at.
	DefaultRepository = "griffone/wails-fileforge"
	// DefaultBaseURL is the public GitHub API.
	DefaultBaseURL = "https://api.github.com"
	// DefaultTokenEnv names the environment variable that carries an optional
	// GitHub token. It raises the rate limit and unlocks private repositories.
	// It is read by the composition root, never written to a log or an error.
	DefaultTokenEnv = "FILEFORGE_GITHUB_TOKEN"

	// checksumsAssetName is the asset the provider reads digests from.
	checksumsAssetName = "SHA256SUMS"
	// signatureSuffix is appended to the artifact name for its detached
	// signature.
	signatureSuffix = ".sig"

	// maxMetadataBytes bounds the release JSON, checksums and signature
	// payloads. They are tiny; a multi-megabyte response is a broken feed.
	maxMetadataBytes = 4 << 20
)

// Asset kinds, used to decide whether the selected file can be installed.
const (
	AssetKindInstaller = "installer"
	AssetKindAppImage  = "appimage"
	AssetKindZip       = "zip"
)

// Asset is the release file selected for the running platform.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
	Kind string `json:"kind,omitempty"`
}

// Verification is everything needed to authenticate the downloaded bytes. The
// digest comes from SHA256SUMS; the signature is the detached .sig sidecar.
// Both are raw bytes.
type Verification struct {
	Digest    []byte `json:"digest,omitempty"`
	Signature []byte `json:"signature,omitempty"`
}

// Release is the descriptor the provider returns from Check.
type Release struct {
	Version      string       `json:"version"`
	Tag          string       `json:"tag"`
	Name         string       `json:"name,omitempty"`
	Notes        string       `json:"notes,omitempty"`
	PublishedAt  time.Time    `json:"publishedAt,omitempty"`
	Asset        Asset        `json:"asset"`
	Verification Verification `json:"verification"`
}

// ProviderConfig configures the GitHub release provider.
type ProviderConfig struct {
	// Repository is "owner/repo". Empty uses DefaultRepository.
	Repository string
	// Token is an optional GitHub token. It is sent as a bearer header and is
	// never included in an error or a log line.
	Token string
	// BaseURL overrides the GitHub API. Empty uses DefaultBaseURL. A test
	// server sets this to its own URL.
	BaseURL string
	// HTTPClient is optional; nil uses a 30 second timeout client.
	HTTPClient *http.Client
	// Platform and Arch select the artifact. Empty uses runtime.GOOS and
	// runtime.GOARCH.
	Platform string
	Arch     string
	// PublicKey is the pinned ed25519 key. Download refuses with
	// ErrNoPublicKey when it is empty.
	PublicKey ed25519.PublicKey
}

// Provider is a small release provider over the GitHub Releases API. It reads
// the latest stable release, selects one artifact for the running platform,
// and attaches the digest and detached signature to the descriptor. It never
// installs anything.
type Provider struct {
	repository string
	token      string
	baseURL    string
	client     *http.Client
	platform   string
	arch       string
	publicKey  ed25519.PublicKey
}

// NewProvider builds a release provider.
func NewProvider(cfg ProviderConfig) (*Provider, error) {
	repository := strings.TrimSpace(cfg.Repository)
	if repository == "" {
		repository = DefaultRepository
	}
	if !strings.Contains(repository, "/") || strings.HasPrefix(repository, "/") || strings.HasSuffix(repository, "/") {
		return nil, fmt.Errorf("update: repository must be \"owner/repo\", got %q", repository)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	platform := strings.TrimSpace(cfg.Platform)
	if platform == "" {
		platform = runtime.GOOS
	}
	arch := strings.TrimSpace(cfg.Arch)
	if arch == "" {
		arch = runtime.GOARCH
	}

	return &Provider{
		repository: repository,
		token:      strings.TrimSpace(cfg.Token),
		baseURL:    baseURL,
		client:     client,
		platform:   platform,
		arch:       arch,
		publicKey:  cfg.PublicKey,
	}, nil
}

type releaseAsset struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	URL        string `json:"url"`
	BrowserURL string `json:"browser_download_url"`
}

type releasePayload struct {
	TagName     string         `json:"tag_name"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	PublishedAt time.Time      `json:"published_at"`
	Assets      []releaseAsset `json:"assets"`
}

// Check resolves the latest stable release and returns a descriptor for the
// artifact that matches the provider's platform. It only hits
// /releases/latest, so GitHub itself never serves a prerelease; the explicit
// draft/prerelease check rejects a feed that ignores that contract.
func (p *Provider) Check(ctx context.Context, currentVersion string) (*Release, error) {
	var payload releasePayload
	if err := p.getJSON(ctx, p.baseURL+"/repos/"+p.repository+"/releases/latest", &payload); err != nil {
		return nil, err
	}

	tag := strings.TrimSpace(payload.TagName)
	if tag == "" {
		return nil, fmt.Errorf("%w: release has no tag_name", ErrMalformedRelease)
	}
	if payload.Draft || payload.Prerelease {
		return nil, fmt.Errorf("%w: %s", ErrNotStable, tag)
	}

	selected, err := selectAsset(payload.Assets, p.platform, p.arch)
	if err != nil {
		return nil, err
	}

	digest, err := p.fetchChecksum(ctx, payload.Assets, selected.Name)
	if err != nil {
		return nil, err
	}
	signature, err := p.fetchSignature(ctx, payload.Assets, selected.Name)
	if err != nil {
		return nil, err
	}

	return &Release{
		Version:     strings.TrimPrefix(tag, "v"),
		Tag:         tag,
		Name:        payload.Name,
		Notes:       payload.Body,
		PublishedAt: payload.PublishedAt,
		Asset: Asset{
			Name: selected.Name,
			URL:  p.assetURL(selected),
			Size: selected.Size,
			Kind: assetKind(selected.Name),
		},
		Verification: Verification{Digest: digest, Signature: signature},
	}, nil
}

// selectAsset picks one asset for a platform. Windows prefers the NSIS
// installer over the portable ZIP; Linux/amd64 uses the AppImage. Anything
// else is unsupported: no guessing, no picking a package the app cannot
// install.
func selectAsset(assets []releaseAsset, platform, arch string) (releaseAsset, error) {
	if platform != "windows" && platform != "linux" {
		return releaseAsset{}, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, platform, arch)
	}
	if arch != "amd64" {
		return releaseAsset{}, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, platform, arch)
	}

	switch platform {
	case "windows":
		var installer, zip releaseAsset
		for _, asset := range assets {
			lower := strings.ToLower(asset.Name)
			switch {
			case strings.HasSuffix(lower, ".exe") && strings.Contains(lower, "installer"):
				if installer.Name == "" {
					installer = asset
				}
			case strings.HasSuffix(lower, ".zip") && strings.Contains(lower, "windows"):
				if zip.Name == "" {
					zip = asset
				}
			}
		}
		if installer.Name != "" {
			return installer, nil
		}
		if zip.Name != "" {
			return zip, nil
		}
		return releaseAsset{}, fmt.Errorf("%w: windows/amd64", ErrAssetMissing)
	case "linux":
		for _, asset := range assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), ".appimage") {
				return asset, nil
			}
		}
		return releaseAsset{}, fmt.Errorf("%w: linux/amd64 (AppImage)", ErrAssetMissing)
	default:
		return releaseAsset{}, fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, platform, arch)
	}
}

// assetKind classifies a selected asset.
func assetKind(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".appimage"):
		return AssetKindAppImage
	case strings.HasSuffix(lower, ".exe") && strings.Contains(lower, "installer"):
		return AssetKindInstaller
	case strings.HasSuffix(lower, ".zip"):
		return AssetKindZip
	default:
		return ""
	}
}

// fetchChecksum reads SHA256SUMS and returns the raw digest for artifact.
func (p *Provider) fetchChecksum(ctx context.Context, assets []releaseAsset, artifact string) ([]byte, error) {
	asset, ok := findAsset(assets, checksumsAssetName)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrChecksumsMissing, checksumsAssetName)
	}

	body, err := p.getBytes(ctx, p.assetURL(asset))
	if err != nil {
		return nil, err
	}

	digest, err := parseSHA256SUMS(body, artifact)
	if err != nil {
		return nil, err
	}
	return digest, nil
}

// fetchSignature reads <artifact>.sig and decodes the 64-byte signature.
func (p *Provider) fetchSignature(ctx context.Context, assets []releaseAsset, artifact string) ([]byte, error) {
	name := artifact + signatureSuffix
	asset, ok := findAsset(assets, name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSignatureMissing, name)
	}

	body, err := p.getBytes(ctx, p.assetURL(asset))
	if err != nil {
		return nil, err
	}

	signature, err := parseSignature(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrMalformedSignature, name, err)
	}
	return signature, nil
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

// assetURL returns the download URL. Private repositories need the API asset
// URL and an authenticated request; public repositories work with the browser
// URL. Neither carries the token.
func (p *Provider) assetURL(asset releaseAsset) string {
	if p.token != "" && asset.URL != "" {
		return asset.URL
	}
	if asset.BrowserURL != "" {
		return asset.BrowserURL
	}
	return asset.URL
}

// parseSHA256SUMS finds artifact in a sha256sum-style checksums file. It
// accepts the binary marker and a leading "./" because both appear in files
// produced by different tools.
func parseSHA256SUMS(data []byte, artifact string) ([]byte, error) {
	want := filepath.Base(artifact)
	valid := 0
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != 32 {
			continue
		}
		valid++

		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		name = strings.TrimPrefix(name, "./")
		if filepath.Base(name) == want {
			return decoded, nil
		}
	}
	if valid == 0 {
		return nil, fmt.Errorf("%w: no checksum lines", ErrMalformedChecksums)
	}
	return nil, fmt.Errorf("%w: %s", ErrChecksumMissing, artifact)
}

// parseSignature decodes base64 into a raw ed25519 signature.
func parseSignature(data []byte) ([]byte, error) {
	value := strings.TrimSpace(string(data))
	if value == "" {
		return nil, errors.New("empty signature")
	}

	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil {
		return nil, errors.New("not valid base64")
	}
	if len(decoded) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature is %d bytes, want %d", len(decoded), ed25519.SignatureSize)
	}
	return decoded, nil
}

func (p *Provider) getJSON(ctx context.Context, url string, target any) error {
	body, err := p.getBytes(ctx, url)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedRelease, err)
	}
	return nil
}

func (p *Provider) getBytes(ctx context.Context, url string) ([]byte, error) {
	response, err := p.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	if len(body) > maxMetadataBytes {
		return nil, fmt.Errorf("%w: response larger than %d bytes", ErrMalformedRelease, maxMetadataBytes)
	}
	return body, nil
}

// newRequest builds an authenticated GET. The token is only ever placed in a
// request header; it never becomes part of a URL or an error.
func (p *Provider) newRequest(ctx context.Context, url, accept string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "fileforge-updater")
	if p.token != "" {
		request.Header.Set("Authorization", "Bearer "+p.token)
	}
	return request, nil
}

func (p *Provider) get(ctx context.Context, url string) (*http.Response, error) {
	accept := "application/vnd.github+json"
	if strings.Contains(url, "/releases/assets/") {
		// The API asset endpoint returns JSON metadata unless the caller asks
		// for the bytes.
		accept = "application/octet-stream"
	}
	request, err := p.newRequest(ctx, url, accept)
	if err != nil {
		return nil, err
	}

	response, err := p.client.Do(request)
	if err != nil {
		// The token lives in a header. The error carries the URL and the
		// transport cause, never the request headers.
		return nil, fmt.Errorf("%w: %v", ErrNetwork, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%w: %s returned %s", ErrHTTPStatus, url, response.Status)
	}
	return response, nil
}
