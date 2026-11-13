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
	// limit. The oldest registration is evicted when the cap is reached.
	maxEntries = 128
	// tokenBytes is the amount of entropy used per media token (32 bytes -> 64
	// hex characters).
	tokenBytes = 32
	// pathPrefix is the URL prefix owned by this package.
	pathPrefix = "/media/"
)

type entry struct {
	path      string
	createdAt time.Time
}

var registry = struct {
	mu      sync.RWMutex
	entries map[string]entry
}{
	entries: make(map[string]entry, maxEntries),
}

// Register validates path as an existing regular file and returns a random
// token that can be used to stream it through Middleware.
func Register(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", errors.New("media: path is required")
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return "", fmt.Errorf("media: stat file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("media: path is not a regular file")
	}

	token, err := newToken()
	if err != nil {
		return "", fmt.Errorf("media: generate token: %w", err)
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if len(registry.entries) >= maxEntries {
		evictOldestLocked()
	}
	registry.entries[token] = entry{
		path:      filepath.Clean(trimmed),
		createdAt: time.Now(),
	}

	return token, nil
}

// Lookup returns the file path registered for token.
func Lookup(token string) (string, bool) {
	registry.mu.RLock()
	registered, ok := registry.entries[token]
	registry.mu.RUnlock()
	if !ok {
		return "", false
	}
	return registered.path, true
}

// Middleware serves registered media at /media/<token> and delegates every
// other request to next. Filesystem paths never appear in responses.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, pathPrefix) {
			next.ServeHTTP(w, r)
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

func evictOldestLocked() {
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
