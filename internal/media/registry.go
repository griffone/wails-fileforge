// Package media exposes explicitly registered local files over a token-scoped
// HTTP endpoint. Only files registered through Register can be served, and the
// endpoint supports HTTP Range requests so the WebView can seek in media.
package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// maxEntries bounds the registry so long sessions cannot grow it without
	// limit. The oldest or expired registration is evicted when the cap is
	// reached.
	maxEntries = 128
	// tokenBytes is the amount of entropy used per media token (32 bytes -> 64
	// hex characters).
	tokenBytes = 32
	// pathPrefix is the URL prefix owned by this package.
	pathPrefix = "/media/"
	// defaultTokenTTL is how long a registered token stays valid when no TTL is
	// configured. It is long enough to cover a paused playback session because
	// only new requests are rejected after expiry; an open stream keeps its
	// file handle.
	defaultTokenTTL = time.Hour
)

// audioContentTypes pins the Content-Type of the audio formats the sync editor
// streams. Go's built-in mime table carries no audio entries, so without this
// map the served type depends on the host MIME database: it exists on Linux,
// but a Windows or minimal container environment may resolve nothing and fall
// back to content sniffing, which can hand <audio> an unusable type. Video
// formats resolve consistently enough on every supported host and are left to
// the platform table.
var audioContentTypes = map[string]string{
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
}

// contentTypeFor returns the pinned content type for a known media extension,
// or an empty string when the platform table should decide.
func contentTypeFor(path string) string {
	return audioContentTypes[strings.ToLower(filepath.Ext(path))]
}

type entry struct {
	path      string
	createdAt time.Time
	expiresAt time.Time
}

var registry = struct {
	mu      sync.RWMutex
	entries map[string]entry
	roots   []string
	ttl     time.Duration
}{
	entries: make(map[string]entry, maxEntries),
	ttl:     defaultTokenTTL,
}

// SetAllowedRoots constrains Register to files located under roots. Roots are
// made absolute and resolved through symlinks so comparisons use real
// filesystem locations. An empty list restores the default roots: the user home
// directory and the OS temp directory.
func SetAllowedRoots(roots []string) {
	resolved := make([]string, 0, len(roots))
	for _, root := range roots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		resolved = append(resolved, resolvePath(trimmed))
	}

	registry.mu.Lock()
	registry.roots = resolved
	registry.mu.Unlock()
}

// SetTokenTTL changes how long newly registered tokens stay valid. Values less
// than or equal to zero restore the default TTL.
func SetTokenTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}

	registry.mu.Lock()
	registry.ttl = ttl
	registry.mu.Unlock()
}

// Unregister revokes token immediately. Unknown tokens are ignored.
func Unregister(token string) {
	registry.mu.Lock()
	delete(registry.entries, token)
	registry.mu.Unlock()
}

// SweepExpired removes every expired registration. Lookup already rejects
// expired tokens, so sweeping only releases memory.
func SweepExpired() {
	registry.mu.Lock()
	sweepExpiredLocked(time.Now())
	registry.mu.Unlock()
}

// resetForTests clears registrations, allowed roots, and the configured TTL. It
// exists so package tests start from a known state.
func resetForTests() {
	registry.mu.Lock()
	registry.entries = make(map[string]entry, maxEntries)
	registry.roots = nil
	registry.ttl = defaultTokenTTL
	registry.mu.Unlock()
}

// Register validates path as an existing regular file inside the allowed roots
// and returns a random token that can be used to stream it through Middleware.
func Register(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("media: path is required")
	}

	resolved := resolvePath(trimmed)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("media: stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("media: path is not a regular file")
	}

	if !withinAllowedRoots(resolved) {
		return "", fmt.Errorf("media: path is outside allowed roots: %s", trimmed)
	}

	token, err := newToken()
	if err != nil {
		return "", fmt.Errorf("media: generate token: %w", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	now := time.Now()
	if len(registry.entries) >= maxEntries {
		evictLocked(now)
	}
	registry.entries[token] = entry{
		path:      resolved,
		createdAt: now,
		expiresAt: now.Add(registry.ttl),
	}

	return token, nil
}

// Lookup returns the file path registered for token. Invalid, unknown, and
// expired tokens return false; expired entries are deleted on access.
func Lookup(token string) (string, bool) {
	if !validToken(token) {
		return "", false
	}

	registry.mu.RLock()
	registered, ok := registry.entries[token]
	registry.mu.RUnlock()
	if !ok {
		return "", false
	}

	if !registered.expiresAt.IsZero() && time.Now().After(registered.expiresAt) {
		registry.mu.Lock()
		delete(registry.entries, token)
		registry.mu.Unlock()
		return "", false
	}

	return registered.path, true
}

// Middleware serves registered media at /media/<token> and delegates every
// other request to next. Filesystem paths never appear in responses. Only GET
// and HEAD are served; media responses are never cached by an intermediary.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, pathPrefix) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token := strings.TrimPrefix(r.URL.Path, pathPrefix)
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(w, r)
			return
		}

		path, ok := Lookup(token)
		if !ok {
			http.NotFound(w, r)
			return
		}

		file, err := os.Open(path)
		if err != nil {
			http.Error(w, "media unavailable", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			http.Error(w, "media unavailable", http.StatusInternalServerError)
			return
		}
		if !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		// Pin the audio content type before ServeContent: it only fills the
		// header when it is empty, so this keeps <audio> seeking on every host.
		if contentType := contentTypeFor(path); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}

		// ServeContent handles Range, HEAD and conditional requests.
		http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
	})
}

func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// validToken reports whether token has the exact shape produced by newToken:
// lowercase-or-uppercase hexadecimal of the expected length.
func validToken(token string) bool {
	if len(token) != tokenBytes*2 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}

// withinAllowedRoots reports whether path lives under one of the configured
// roots, falling back to the default roots when none are configured.
func withinAllowedRoots(path string) bool {
	registry.mu.RLock()
	roots := registry.roots
	if len(roots) == 0 {
		roots = defaultRoots()
	}
	registry.mu.RUnlock()

	for _, root := range roots {
		if isWithinRoot(root, path) {
			return true
		}
	}
	return false
}

// isWithinRoot reports whether path equals root or lives below it. filepath.Rel
// is used instead of string prefixes so ".." segments, sibling directories with
// a shared name prefix, and symlinked roots cannot bypass the check.
func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// defaultRoots returns the user home and OS temp directory, resolved through
// symlinks. It mirrors the roots used by the preview service.
func defaultRoots() []string {
	roots := make([]string, 0, 2)
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		roots = append(roots, resolvePath(home))
	}
	if temp := strings.TrimSpace(os.TempDir()); temp != "" {
		roots = append(roots, resolvePath(temp))
	}
	return roots
}

// resolvePath returns an absolute path with symlinks resolved when the path
// exists. Missing paths keep their cleaned absolute form so callers get a
// consistent error from Stat.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if evaluated, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		return evaluated
	}
	return abs
}

func sweepExpiredLocked(now time.Time) {
	for token, registered := range registry.entries {
		if !registered.expiresAt.IsZero() && now.After(registered.expiresAt) {
			delete(registry.entries, token)
		}
	}
}

// evictLocked removes expired entries first and then the oldest registration so
// the registry stays under maxEntries.
func evictLocked(now time.Time) {
	sweepExpiredLocked(now)
	if len(registry.entries) < maxEntries {
		return
	}

	var oldestToken string
	var oldest time.Time
	for token, registered := range registry.entries {
		if oldestToken == "" || registered.createdAt.Before(oldest) {
			oldestToken = token
			oldest = registered.createdAt
		}
	}
	if oldestToken != "" {
		delete(registry.entries, oldestToken)
	}
}
