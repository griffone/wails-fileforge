package media

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
)

// loopbackServer holds the restartable loopback listener. A mutex instead of
// sync.Once lets CloseServer tear it down and EnsureServer start a fresh
// listener on a new port afterwards.
var loopbackServer = struct {
	mu       sync.Mutex
	server   *http.Server
	listener net.Listener
	baseURL  string
}{}

// EnsureServer starts a loopback-only HTTP server for registered media and
// returns its base URL. Wails development mode loads the webview from the
// frontend dev server, so the embedded asset-server middleware is unreachable
// there; the loopback URL is used as a fallback. Production builds keep using
// the relative asset-server path and never start a listener: EnsureServer
// returns an empty URL in that case.
func EnsureServer() (string, error) {
	if !loopbackEnabled() {
		return "", nil
	}

	loopbackServer.mu.Lock()
	defer loopbackServer.mu.Unlock()

	if loopbackServer.server != nil {
		return loopbackServer.baseURL, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}

	server := &http.Server{
		Handler:           withCORS(Middleware(http.NotFoundHandler())),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	baseURL := "http://" + listener.Addr().String()
	loopbackServer.listener = listener
	loopbackServer.server = server
	loopbackServer.baseURL = baseURL

	go func() {
		serveErr := server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("media: loopback server stopped: %v", serveErr)
		}

		loopbackServer.mu.Lock()
		// Only clear the state when this is still the current server; a restart
		// may have installed a newer one while the goroutine was waking up.
		if loopbackServer.server == server {
			loopbackServer.server = nil
			loopbackServer.listener = nil
			loopbackServer.baseURL = ""
		}
		loopbackServer.mu.Unlock()
	}()

	return baseURL, nil
}

// CloseServer stops the loopback media server, clears its state, and allows a
// later EnsureServer to start a fresh listener. It is safe to call when the
// server was never started or was already closed.
func CloseServer() error {
	loopbackServer.mu.Lock()
	server := loopbackServer.server
	loopbackServer.server = nil
	loopbackServer.listener = nil
	loopbackServer.baseURL = ""
	loopbackServer.mu.Unlock()

	if server == nil {
		return nil
	}

	err := server.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// withCORS restricts cross-origin access to local desktop webview origins. The
// request Origin is echoed only when it is allowed; other origins receive no
// allow header, so the browser blocks the response. Preflight requests are
// answered here because Middleware rejects every method except GET and HEAD.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Range, Content-Type")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, Content-Type")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin reports whether origin belongs to a local desktop webview.
// Allowed schemes and hosts: http on localhost or 127.0.0.1 with any port, the
// wails:// custom scheme, and http on wails.localhost (including subdomains).
func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}

	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}

	switch strings.ToLower(parsed.Scheme) {
	case "wails":
		return true
	case "http":
		host := strings.ToLower(parsed.Hostname())
		if host == "localhost" || host == "127.0.0.1" {
			return true
		}
		return host == "wails.localhost" || strings.HasSuffix(host, ".wails.localhost")
	default:
		return false
	}
}
