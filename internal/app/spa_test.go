package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSPAFallbackRewritesClientRoutesOnly(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		path     string
		wantPath string
	}{
		{name: "tool route", method: http.MethodGet, path: "/image-crop", wantPath: "/"},
		{name: "nested route", method: http.MethodGet, path: "/recipe/email-pdf", wantPath: "/"},
		{name: "head request", method: http.MethodHead, path: "/pdf-merge", wantPath: "/"},
		{name: "root is untouched", method: http.MethodGet, path: "/", wantPath: "/"},
		{name: "script chunk", method: http.MethodGet, path: "/main-ABC123.js", wantPath: "/main-ABC123.js"},
		{name: "asset folder", method: http.MethodGet, path: "/assets/logo.svg", wantPath: "/assets/logo.svg"},
		{name: "pdf worker", method: http.MethodGet, path: "/assets/pdfjs/pdf.worker.min.mjs", wantPath: "/assets/pdfjs/pdf.worker.min.mjs"},
		{name: "favicon", method: http.MethodGet, path: "/favicon.ico", wantPath: "/favicon.ico"},
		{name: "wails runtime", method: http.MethodGet, path: "/wails/runtime", wantPath: "/wails/runtime"},
		{name: "media token", method: http.MethodGet, path: "/media/deadbeef", wantPath: "/media/deadbeef"},
		{name: "runtime post", method: http.MethodPost, path: "/image-crop", wantPath: "/image-crop"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var seen string
			handler := SPAFallback(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = r.URL.Path
			}))

			request := httptest.NewRequest(testCase.method, testCase.path, nil)
			handler.ServeHTTP(httptest.NewRecorder(), request)

			if seen != testCase.wantPath {
				t.Fatalf("%s %s reached the asset handler as %q, want %q", testCase.method, testCase.path, seen, testCase.wantPath)
			}
		})
	}
}

func TestSPAFallbackKeepsTheQueryString(t *testing.T) {
	var seenPath, seenQuery string
	handler := SPAFallback(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
	}))

	request := httptest.NewRequest(http.MethodGet, "/?route=%2Fimage-crop", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if seenPath != "/" || seenQuery != "route=%2Fimage-crop" {
		t.Fatalf("request reached the handler as path=%q query=%q, want / and the original query", seenPath, seenQuery)
	}
}
