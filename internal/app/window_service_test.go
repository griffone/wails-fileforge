package app

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// fakeToolWindow records what the service asked of one window.
type fakeToolWindow struct {
	spec    toolWindowSpec
	shown   int
	focused int
	closing func()
}

func (f *fakeToolWindow) handle() *toolWindow {
	return &toolWindow{
		name:  f.spec.name,
		show:  func() { f.shown++ },
		focus: func() { f.focused++ },
		onClosing: func(callback func()) {
			// A listener can observe a close; it cannot cancel it.
			f.closing = callback
		},
	}
}

// newFakeWindowService returns a service with a fake opener and the windows it
// created in order.
func newFakeWindowService(t *testing.T) (*WindowService, *[]*fakeToolWindow) {
	t.Helper()

	bundle := newTestApp(t)
	created := make([]*fakeToolWindow, 0, 2)

	bundle.Windows.opener = func(spec toolWindowSpec) *toolWindow {
		fake := &fakeToolWindow{spec: spec}
		created = append(created, fake)

		return fake.handle()
	}

	return bundle.Windows, &created
}

func TestResolveToolRouteAcceptsToolPages(t *testing.T) {
	a := newTestApp(t)

	cases := map[string]struct {
		wantRoute string
		wantTool  string
	}{
		"/image-crop":            {wantRoute: "/image-crop", wantTool: "tool.image.crop"},
		" /pdf-extract-images/ ": {wantRoute: "/pdf-extract-images", wantTool: "tool.pdf.extract_images"},
		"/audio-normalize":       {wantRoute: "/audio-normalize", wantTool: "tool.audio.normalize"},
	}

	for raw, want := range cases {
		route, toolID, err := a.Windows.resolveToolRoute(raw)
		if err != nil {
			t.Fatalf("resolveToolRoute(%q) error = %v, want none", raw, err)
		}
		if route != want.wantRoute {
			t.Fatalf("resolveToolRoute(%q) route = %q, want %q", raw, route, want.wantRoute)
		}
		if toolID != want.wantTool {
			t.Fatalf("resolveToolRoute(%q) tool = %q, want %q", raw, toolID, want.wantTool)
		}
	}
}

func TestResolveToolRouteRejectsEverythingElse(t *testing.T) {
	a := newTestApp(t)

	cases := map[string]string{
		"empty route":            "",
		"blank route":            "   ",
		"relative route":         "image-crop",
		"absolute https URL":     "https://example.test/image-crop",
		"absolute file URL":      "file:///etc/passwd",
		"protocol relative URL":  "//example.test/image-crop",
		"route with query":       "/image-crop?full=1",
		"route with fragment":    "/image-crop#top",
		"home":                   "/",
		"settings":               "/settings",
		"tool catalog":           "/tool-catalog",
		"automations":            "/automations",
		"duplicates":             "/duplicates",
		"recipe page":            "/recipe/email-pdf",
		"unknown tool route":     "/image-nope",
		"unregistered traversal": "/image-crop/../settings",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			route, toolID, err := a.Windows.resolveToolRoute(raw)
			if err == nil {
				t.Fatalf("resolveToolRoute(%q) = (%q, %q, nil), want an error", raw, route, toolID)
			}
			if route != "" || toolID != "" {
				t.Fatalf("resolveToolRoute(%q) returned a route on rejection: (%q, %q)", raw, route, toolID)
			}
			if !strings.Contains(err.Error(), "open tool window") {
				t.Fatalf("resolveToolRoute(%q) error = %q, want the open-tool-window prefix", raw, err)
			}
		})
	}
}

func TestOpenToolWindowCreatesThenFocusesTheSameRoute(t *testing.T) {
	service, created := newFakeWindowService(t)

	first := service.OpenToolWindow("/image-crop")
	if !first.Success || !first.Created {
		t.Fatalf("first open = %+v, want a created window", first)
	}
	if first.Route != "/image-crop" || first.WindowName != "tool:image-crop" {
		t.Fatalf("first open = %+v, want route /image-crop and window tool:image-crop", first)
	}
	if len(*created) != 1 {
		t.Fatalf("windows created = %d, want 1", len(*created))
	}

	spec := (*created)[0].spec
	if spec.name != "tool:image-crop" {
		t.Fatalf("window name = %q, want tool:image-crop", spec.name)
	}
	if spec.title != "Image Crop — FileForge Desktop" {
		t.Fatalf("window title = %q, want the tool name plus the product name", spec.title)
	}
	if spec.url != "/?route=%2Fimage-crop" {
		t.Fatalf("window URL = %q, want the route in the bootstrap query parameter", spec.url)
	}
	if (*created)[0].shown != 1 || (*created)[0].focused != 1 {
		t.Fatalf("new window show=%d focus=%d, want both once", (*created)[0].shown, (*created)[0].focused)
	}

	second := service.OpenToolWindow("/image-crop")
	if !second.Success || second.Created {
		t.Fatalf("second open = %+v, want the existing window focused", second)
	}
	if second.WindowName != first.WindowName {
		t.Fatalf("second open window = %q, want %q", second.WindowName, first.WindowName)
	}
	if len(*created) != 1 {
		t.Fatalf("windows created after the second open = %d, want 1", len(*created))
	}
	if (*created)[0].focused != 2 || (*created)[0].shown != 2 {
		t.Fatalf("existing window show=%d focus=%d, want both twice", (*created)[0].shown, (*created)[0].focused)
	}
}

func TestOpenToolWindowKeepsOneWindowPerRoute(t *testing.T) {
	service, created := newFakeWindowService(t)

	for _, route := range []string{"/image-crop", "/pdf-merge", "/image-crop", "/pdf-merge"} {
		if response := service.OpenToolWindow(route); !response.Success {
			t.Fatalf("OpenToolWindow(%q) failed: %s", route, response.Message)
		}
	}

	if len(*created) != 2 {
		t.Fatalf("windows created = %d, want one per route (2)", len(*created))
	}
	if service.openWindowCount() != 2 {
		t.Fatalf("tracked windows = %d, want 2", service.openWindowCount())
	}
}

func TestSecondaryWindowCloseForgetsItWithoutQuitting(t *testing.T) {
	service, created := newFakeWindowService(t)

	if response := service.OpenToolWindow("/pdf-merge"); !response.Success {
		t.Fatalf("open failed: %s", response.Message)
	}
	window := (*created)[0]
	if window.closing == nil {
		t.Fatal("window has no close listener, so the service can never forget it")
	}

	// The service only ever registers an observing listener: the handle type
	// has no hide or cancel hook, so a secondary close cannot become "keep the
	// app alive" or trigger the tray's quit path.
	window.closing()

	if service.openWindowCount() != 0 {
		t.Fatalf("tracked windows after close = %d, want 0", service.openWindowCount())
	}

	reopened := service.OpenToolWindow("/pdf-merge")
	if !reopened.Success || !reopened.Created {
		t.Fatalf("reopen after close = %+v, want a fresh window", reopened)
	}
	if len(*created) != 2 {
		t.Fatalf("windows created = %d, want 2 (one per open)", len(*created))
	}
}

func TestOpenToolWindowWithoutApplicationIsRejected(t *testing.T) {
	a := newTestApp(t)

	response := a.Windows.OpenToolWindow("/image-crop")
	if response.Success {
		t.Fatalf("open without Attach = %+v, want a failure", response)
	}
	if !strings.Contains(response.Message, "not ready") {
		t.Fatalf("message = %q, want the window-manager-not-ready explanation", response.Message)
	}
}

func TestOpenToolWindowRejectsUnknownRouteWithoutOpening(t *testing.T) {
	service, created := newFakeWindowService(t)

	response := service.OpenToolWindow("https://example.test/image-crop")
	if response.Success {
		t.Fatalf("open absolute URL = %+v, want a failure", response)
	}
	if len(*created) != 0 {
		t.Fatalf("windows created for a rejected route = %d, want 0", len(*created))
	}
	if response.Route != "https://example.test/image-crop" {
		t.Fatalf("rejected response route = %q, want the rejected input echoed back", response.Route)
	}
}

func TestMainWindowKeepsHideOnCloseWithRunningJobs(t *testing.T) {
	if !isMainWindow(MainWindowName) {
		t.Fatalf("isMainWindow(%q) = false, want true", MainWindowName)
	}
	if isMainWindow(toolWindowName("/image-crop")) {
		t.Fatal("a tool window name must not be treated as the main window")
	}

	// The primary window still hides while jobs run and quits when idle.
	if got := closeDecision(2, true); got != closeHide {
		t.Fatalf("main window close with running jobs = %v, want hide", got)
	}
	if got := closeDecision(0, true); got != closeQuit {
		t.Fatalf("main window close while idle = %v, want quit", got)
	}
}

func TestToolWindowRoutesCoverEveryRegisteredTool(t *testing.T) {
	a := newTestApp(t)

	registry := a.Windows.registry()
	if registry == nil {
		t.Fatal("tool registry is not ready")
	}

	tools := a.Tools.ListTools()
	if len(tools.Tools) != len(toolWindowRoutes) {
		t.Fatalf("openable routes = %d, registered tools = %d, want the same count", len(toolWindowRoutes), len(tools.Tools))
	}

	seen := make(map[string]struct{}, len(toolWindowRoutes))
	for route, toolID := range toolWindowRoutes {
		if _, duplicate := seen[toolID]; duplicate {
			t.Fatalf("route %q maps to %q, which another route already claims", route, toolID)
		}
		seen[toolID] = struct{}{}

		tool, err := registry.GetTool(toolID)
		if err != nil || tool == nil {
			t.Fatalf("route %q maps to %q, which is not registered: %v", route, toolID, err)
		}
	}
}

// frontendToolEntry matches one entry of frontend/src/app/config/tools.ts. The
// file is uniform: id, name, navName, description, route, in that order.
var frontendToolEntry = regexp.MustCompile(`id:\s*'([^']+)',\s*name:\s*'[^']*',\s*navName:\s*'[^']*',[\s\S]*?route:\s*'([^']+)',`)

// TestToolWindowRoutesMatchFrontend keeps the Go allowlist and the frontend
// tool registry from drifting: every openable route must exist there with the
// same tool id, and the frontend must not declare a route the backend rejects.
func TestToolWindowRoutesMatchFrontend(t *testing.T) {
	const path = "../../frontend/src/app/config/tools.ts"

	source, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("frontend tool registry not available: %v", err)
	}

	start := strings.Index(string(source), "export const TOOLS")
	if start < 0 {
		t.Fatalf("%s has no TOOLS export", path)
	}
	body := string(source)[start:]
	if end := strings.Index(body, "\n];"); end >= 0 {
		body = body[:end]
	}

	frontend := make(map[string]string)
	for _, match := range frontendToolEntry.FindAllStringSubmatch(body, -1) {
		frontend[match[2]] = match[1]
	}
	if len(frontend) == 0 {
		t.Fatalf("parsed no tool entries from %s", path)
	}

	backendRoutes := make([]string, 0, len(toolWindowRoutes))
	for route := range toolWindowRoutes {
		backendRoutes = append(backendRoutes, route)
	}
	sort.Strings(backendRoutes)

	for _, route := range backendRoutes {
		toolID, ok := frontend[route]
		if !ok {
			t.Fatalf("backend opens %q but the frontend registry has no such route", route)
		}
		if toolID != toolWindowRoutes[route] {
			t.Fatalf("route %q maps to %q in Go and %q in the frontend", route, toolWindowRoutes[route], toolID)
		}
	}

	for route := range frontend {
		if _, ok := toolWindowRoutes[route]; !ok {
			t.Fatalf("frontend declares tool route %q that the backend refuses to open", route)
		}
	}
}
