package app

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// MainWindowName is the name main.go gives the primary window. Only that
	// window hides instead of closing while work runs; every other window
	// closes normally.
	MainWindowName = "main"

	// toolWindowNamePrefix namespaces secondary windows so they can never
	// collide with the primary window.
	toolWindowNamePrefix = "tool:"

	// toolWindowRouteParam is the query parameter the SPA reads at bootstrap.
	// The shell rewrites the URL to the route it names before Angular starts,
	// so the window never paints Home first.
	toolWindowRouteParam = "route"

	// A tool window holds one tool, not the whole workbench, so it opens
	// smaller than the main window while staying usable side by side.
	toolWindowWidth     = 1180
	toolWindowHeight    = 840
	toolWindowMinWidth  = 900
	toolWindowMinHeight = 620
)

// toolWindowRoutes maps every openable route to the backend tool it runs. It is
// the backend counterpart of frontend/src/app/config/tools.ts: the routes are
// frontend-owned, the ids are the registry contract. Any route missing here is
// rejected, which keeps arbitrary paths and absolute URLs out of a window.
var toolWindowRoutes = map[string]string{
	"/image-converter":    "tool.image.convert",
	"/image-crop":         "tool.image.crop",
	"/image-annotate":     "tool.image.annotate",
	"/image-compress":     "tool.image.compress",
	"/image-watermark":    "tool.image.watermark",
	"/image-ocr":          "tool.image.ocr",
	"/image-background":   "tool.image.background",
	"/image-upscale":      "tool.image.upscale",
	"/pdf-merge":          "tool.pdf.merge",
	"/pdf-split":          "tool.pdf.split",
	"/pdf-crop":           "tool.pdf.crop",
	"/pdf-optimize":       "tool.pdf.optimize",
	"/pdf-ocr":            "tool.pdf.ocr",
	"/pdf-rotate":         "tool.pdf.rotate",
	"/pdf-remove-pages":   "tool.pdf.remove_pages",
	"/pdf-reorder":        "tool.pdf.reorder",
	"/pdf-extract-images": "tool.pdf.extract_images",
	"/pdf-from-images":    "tool.pdf.images",
	"/pdf-to-docx":        "tool.pdf.to_docx",
	"/doc-md-to-pdf":      "tool.doc.md_to_pdf",
	"/doc-docx-to-pdf":    "tool.doc.docx_to_pdf",
	"/video-convert":      "tool.video.convert",
	"/video-trim":         "tool.video.trim",
	"/video-merge":        "tool.video.merge",
	"/video-transcribe":   "tool.video.transcribe",
	"/video-audio-sync":   "tool.video.audio_sync",
	"/web-download":       "tool.web.download",
	"/audio-extract":      "tool.audio.extract",
	"/audio-convert":      "tool.audio.convert",
	"/audio-normalize":    "tool.audio.normalize",
}

// toolWindowSpec is everything the opener needs to create one window.
type toolWindowSpec struct {
	name  string
	title string
	url   string
}

// toolWindow is the service's handle on one secondary window.
//
// It deliberately exposes no close-cancelling hook: the tray's hide-on-close
// behaviour is registered only for the primary window, and secondary windows
// can only observe their close so the service can forget them. That makes
// "closing a secondary window never quits the app and never cancels a job" a
// property of the type, not of a convention.
type toolWindow struct {
	name      string
	show      func()
	focus     func()
	onClosing func(callback func())
}

// toolWindowOpener creates a window for a spec. main.go attaches the real
// implementation after application.New; tests supply a fake.
type toolWindowOpener func(spec toolWindowSpec) *toolWindow

// WindowService opens one extra window per tool route. Windows are keyed by
// route, so asking twice focuses the first one instead of duplicating it.
type WindowService struct {
	tooling *services.ToolingService

	mu      sync.Mutex
	opener  toolWindowOpener
	windows map[string]*toolWindow
}

// NewWindowService builds the service. Windows can only be created once the
// Wails application exists, so main.go calls AttachWindows before app.Run.
func NewWindowService(tooling *services.ToolingService) *WindowService {
	return &WindowService{
		tooling: tooling,
		windows: make(map[string]*toolWindow),
	}
}

// AttachWindows binds the window service to the running Wails application. It
// is a package function rather than a method so no Wails type appears on the
// bound RPC surface, and main.go calls it after application.New.
func AttachWindows(app *application.App, service *WindowService) {
	if service == nil {
		return
	}

	service.attach(app)
}

// attach installs the real window opener. The opener registers a WindowClosing
// listener, never a hook, so a close request is never cancelled.
func (s *WindowService) attach(app *application.App) {
	if app == nil || app.Window == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.opener = func(spec toolWindowSpec) *toolWindow {
		window := app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:           spec.name,
			Title:          spec.title,
			URL:            spec.url,
			Width:          toolWindowWidth,
			Height:         toolWindowHeight,
			MinWidth:       toolWindowMinWidth,
			MinHeight:      toolWindowMinHeight,
			EnableFileDrop: true,
		})

		return &toolWindow{
			name:  spec.name,
			show:  func() { window.Show() },
			focus: func() { window.Focus() },
			onClosing: func(callback func()) {
				window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
					callback()
				})
			},
		}
	}
}

// OpenToolWindow opens the window for one tool route, or focuses the existing
// window for that route. A route that is not a registered tool page is rejected
// with a message the UI can show as-is.
func (s *WindowService) OpenToolWindow(route string) models.ToolWindowResponse {
	canonical, toolID, err := s.resolveToolRoute(route)
	if err != nil {
		return models.ToolWindowResponse{
			Success: false,
			Message: err.Error(),
			Route:   strings.TrimSpace(route),
		}
	}

	s.mu.Lock()
	if existing, ok := s.windows[canonical]; ok {
		s.mu.Unlock()
		existing.show()
		existing.focus()
		return models.ToolWindowResponse{
			Success:    true,
			Message:    fmt.Sprintf("%s is already open; its window was focused", canonical),
			Route:      canonical,
			WindowName: existing.name,
			Created:    false,
		}
	}

	opener := s.opener
	if opener == nil {
		s.mu.Unlock()
		return models.ToolWindowResponse{
			Success: false,
			Message: "open tool window: the desktop window manager is not ready",
			Route:   canonical,
		}
	}

	spec := toolWindowSpec{
		name:  toolWindowName(canonical),
		title: s.toolWindowTitle(toolID, canonical),
		url:   "/?" + toolWindowRouteParam + "=" + url.QueryEscape(canonical),
	}

	// Creating the window and registering its close listener under the lock
	// keeps two concurrent requests for the same route from opening a
	// duplicate: the second call waits and then finds the entry.
	window := opener(spec)
	s.windows[canonical] = window
	window.onClosing(func() { s.forget(canonical, window) })
	s.mu.Unlock()

	window.show()
	window.focus()

	log.Printf("window.open route=%s name=%s running_windows=%d", canonical, window.name, s.openWindowCount())

	return models.ToolWindowResponse{
		Success:    true,
		Message:    fmt.Sprintf("%s opened in a new window", canonical),
		Route:      canonical,
		WindowName: window.name,
		Created:    true,
	}
}

// forget drops a closed window from the map so the next request for the same
// route opens a fresh one. It only removes the entry when it still points at
// the window that closed.
func (s *WindowService) forget(route string, window *toolWindow) {
	s.mu.Lock()
	if current, ok := s.windows[route]; ok && current == window {
		delete(s.windows, route)
	}
	s.mu.Unlock()

	log.Printf("window.closed route=%s name=%s running_windows=%d", route, window.name, s.openWindowCount())
}

// openWindowCount reports how many secondary windows the service tracks.
func (s *WindowService) openWindowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.windows)
}

// resolveToolRoute validates a route and returns its canonical form plus the
// tool it runs. Only registered tool pages are openable: Home, Settings, the
// catalog, recipes and automations stay in the primary window.
func (s *WindowService) resolveToolRoute(raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", errors.New("open tool window: a tool route is required")
	}
	if !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") || strings.Contains(trimmed, "://") {
		return "", "", fmt.Errorf("open tool window: %q is not an app route such as /image-crop", trimmed)
	}
	if strings.ContainsAny(trimmed, "?#") {
		return "", "", fmt.Errorf("open tool window: %q must be a plain route without a query or fragment", trimmed)
	}

	route := strings.TrimRight(trimmed, "/")
	if route == "" {
		route = "/"
	}

	toolID, ok := toolWindowRoutes[route]
	if !ok {
		return "", "", fmt.Errorf("open tool window: %q is not an openable tool page", trimmed)
	}

	registry := s.registry()
	if registry == nil {
		return "", "", errors.New("open tool window: the tool registry is not ready")
	}
	tool, err := registry.GetTool(toolID)
	if err != nil || tool == nil {
		return "", "", fmt.Errorf("open tool window: %q has no registered tool", trimmed)
	}

	return route, toolID, nil
}

// toolWindowTitle is the tool name followed by the product name, so the title
// bar says what the window does and still names the app.
func (s *WindowService) toolWindowTitle(toolID, route string) string {
	name := ""
	if registry := s.registry(); registry != nil {
		if tool, err := registry.GetTool(toolID); err == nil && tool != nil {
			name = tool.Manifest().Name
		}
	}
	if name == "" {
		name = strings.TrimPrefix(route, "/")
	}

	return fmt.Sprintf("%s — %s", name, baseWindowTitle)
}

// toolWindowName derives a stable window name from the route, e.g.
// "tool:pdf-extract-images".
func toolWindowName(route string) string {
	return toolWindowNamePrefix + strings.ReplaceAll(strings.TrimPrefix(route, "/"), "/", "-")
}

// registry returns the tool registry, or nil when the service was built without
// one (unit tests that only exercise route validation).
func (s *WindowService) registry() *registry.Registry {
	if s.tooling == nil {
		return nil
	}

	return s.tooling.Registry()
}
