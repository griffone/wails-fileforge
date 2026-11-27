package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const testRepository = "griffone/wails-fileforge"

// feedOptions customizes the fake release server. Every zero value is a
// working, correctly signed release for linux/amd64.
type feedOptions struct {
	tag          string
	draft        bool
	prerelease   bool
	artifactName string
	platform     string
	arch         string

	// artifact is the published payload. checksums and the signature are
	// derived from it.
	artifact []byte
	// servedArtifact, when set, replaces the bytes the download endpoint
	// returns without changing SHA256SUMS. It is how a tampered download is
	// simulated.
	servedArtifact []byte
	// sizeOverride replaces the asset size in the release JSON.
	sizeOverride int64

	omitChecksums bool
	omitSignature bool
	checksumsBody []byte
	signatureBody []byte
	releaseStatus int
	releaseBody   []byte
	// fail maps a served asset name to an HTTP status.
	fail map[string]int
	// partialArtifact truncates the artifact body and lies about its length,
	// producing a transport failure mid-download.
	partialArtifact bool
}

// feed is a local GitHub-releases stand-in: a latest-release endpoint, the
// selected artifact, SHA256SUMS and the detached signature.
type feed struct {
	server    *httptest.Server
	public    ed25519.PublicKey
	artifact  []byte
	served    []byte
	digest    [32]byte
	checksum  []byte
	signature []byte
	options   feedOptions
}

func newFeed(t *testing.T, adjust func(*feedOptions)) *feed {
	t.Helper()

	options := feedOptions{
		tag:           "v1.3.0",
		artifactName:  "fileforge-desktop-x86_64.AppImage",
		platform:      "linux",
		arch:          "amd64",
		artifact:      []byte("fileforge signed release artifact\n"),
		releaseStatus: http.StatusOK,
	}
	if adjust != nil {
		adjust(&options)
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	digest := sha256.Sum256(options.artifact)
	checksum := options.checksumsBody
	if checksum == nil {
		checksum = []byte(fmt.Sprintf("%x  %s\n", digest, options.artifactName))
	}
	signature := options.signatureBody
	if signature == nil {
		signature = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:])) + "\n")
	}
	served := options.artifact
	if options.servedArtifact != nil {
		served = options.servedArtifact
	}

	f := &feed{
		public:    public,
		artifact:  options.artifact,
		served:    served,
		digest:    digest,
		checksum:  checksum,
		signature: signature,
		options:   options,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/"+testRepository+"/releases/latest", f.serveRelease)
	mux.HandleFunc("/download/", f.serveAsset)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)

	return f
}

func (f *feed) provider(t *testing.T) *Provider {
	t.Helper()

	provider, err := NewProvider(ProviderConfig{
		Repository: testRepository,
		BaseURL:    f.server.URL,
		Platform:   f.options.platform,
		Arch:       f.options.arch,
		PublicKey:  f.public,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

func (f *feed) serveRelease(w http.ResponseWriter, _ *http.Request) {
	if f.options.releaseStatus != http.StatusOK {
		w.WriteHeader(f.options.releaseStatus)
		return
	}
	if f.options.releaseBody != nil {
		_, _ = w.Write(f.options.releaseBody)
		return
	}

	size := f.options.sizeOverride
	if size == 0 {
		size = int64(len(f.artifact))
	}

	assets := []releaseAsset{{
		Name:       f.options.artifactName,
		Size:       size,
		URL:        f.server.URL + "/download/" + f.options.artifactName,
		BrowserURL: f.server.URL + "/download/" + f.options.artifactName,
	}}
	if !f.options.omitChecksums {
		assets = append(assets, releaseAsset{
			Name:       checksumsAssetName,
			Size:       int64(len(f.checksum)),
			URL:        f.server.URL + "/download/" + checksumsAssetName,
			BrowserURL: f.server.URL + "/download/" + checksumsAssetName,
		})
	}
	if !f.options.omitSignature {
		name := f.options.artifactName + signatureSuffix
		assets = append(assets, releaseAsset{
			Name:       name,
			Size:       int64(len(f.signature)),
			URL:        f.server.URL + "/download/" + name,
			BrowserURL: f.server.URL + "/download/" + name,
		})
	}

	payload := releasePayload{
		TagName:    f.options.tag,
		Name:       "FileForge " + f.options.tag,
		Body:       "Release notes for " + f.options.tag,
		Draft:      f.options.draft,
		Prerelease: f.options.prerelease,
		Assets:     assets,
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (f *feed) serveAsset(w http.ResponseWriter, request *http.Request) {
	name := request.URL.Path[len("/download/"):]
	if status, ok := f.options.fail[name]; ok {
		w.WriteHeader(status)
		return
	}

	switch name {
	case f.options.artifactName:
		if f.options.partialArtifact {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(f.served)+64))
			_, _ = w.Write(f.served[:len(f.served)/2])
			return
		}
		_, _ = w.Write(f.served)
	case checksumsAssetName:
		if f.options.omitChecksums {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write(f.checksum)
	case f.options.artifactName + signatureSuffix:
		if f.options.omitSignature {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write(f.signature)
	default:
		http.NotFound(w, request)
	}
}

// eventSink records emitted events in order.
type eventSink struct {
	mu     sync.Mutex
	events []sinkEvent
}

type sinkEvent struct {
	name    string
	payload any
}

func (s *eventSink) Emit(name string, payload any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, sinkEvent{name: name, payload: payload})
}

func (s *eventSink) snapshot() []sinkEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sinkEvent(nil), s.events...)
}
