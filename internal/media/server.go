package media

import (
	"errors"
	"net"
	"net/http"
	"sync"
)

var loopbackServer = struct {
	once     sync.Once
	baseURL  string
	err      error
	server   *http.Server
	listener net.Listener
}{}

// EnsureServer starts a loopback-only HTTP server for registered media. Wails
// development mode loads the webview from the frontend dev server, so the
// embedded asset-server middleware is unreachable there. The loopback URL is
// used as a fallback while production keeps using the relative asset-server
// path.
func EnsureServer() (string, error) {
	loopbackServer.once.Do(func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			loopbackServer.err = err
			return
		}

		handler := withCORS(Middleware(http.NotFoundHandler()))
		server := &http.Server{Handler: handler}
		loopbackServer.listener = listener
		loopbackServer.server = server
		loopbackServer.baseURL = "http://" + listener.Addr().String()

		go func() {
			_ = server.Serve(listener)
		}()
	})

	return loopbackServer.baseURL, loopbackServer.err
}

// CloseServer stops the loopback media server. It is safe to call when the
// server was never started.
func CloseServer() error {
	if loopbackServer.server == nil {
		return nil
	}

	err := loopbackServer.server.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Range, Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
