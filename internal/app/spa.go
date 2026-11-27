package app

import (
	"net/http"
	"path"
	"strings"
)

// internalPathPrefixes are served by Wails or by the app itself and must never
// receive the SPA document.
var internalPathPrefixes = []string{"/wails/", "/media/"}

// SPAFallback rewrites extension-less navigation requests to the app root so
// the asset server can answer them with index.html.
//
// The Wails asset file server has no history fallback: it serves files, not
// routes. Without this, reloading a window whose address is /image-crop answers
// 404 and leaves a blank webview, which matters for every window opened on a
// tool route. Only GET/HEAD requests for a path whose last segment has no
// extension are rewritten; assets, the Wails runtime and the media endpoint are
// passed through untouched.
func SPAFallback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSPANavigation(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Clone so the rewrite cannot leak into a shared request object.
		rewritten := r.Clone(r.Context())
		rewritten.URL.Path = "/"
		next.ServeHTTP(w, rewritten)
	})
}

// isSPANavigation reports whether a request addresses a client-side route.
func isSPANavigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	urlPath := r.URL.Path
	if urlPath == "" || urlPath == "/" {
		return false
	}
	for _, prefix := range internalPathPrefixes {
		if strings.HasPrefix(urlPath, prefix) {
			return false
		}
	}

	// A dot in the last segment means a file (chunk, worker, favicon, cmap).
	return !strings.Contains(path.Base(urlPath), ".")
}
