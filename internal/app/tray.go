package app

import (
	"fmt"
	"log"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// baseWindowTitle is the window title while nothing runs.
	baseWindowTitle = "FileForge Desktop"
	// trayRefreshDelay coalesces bursts of job/recipe state events into one
	// menu rebuild. Progress events are not subscribed, so a progress tick never
	// rebuilds the tray menu.
	trayRefreshDelay = 250 * time.Millisecond

	taskKindJob    = "job"
	taskKindRecipe = "recipe"
)

// terminalStatuses mirrors the job engine's terminal set. Active jobs report a
// stage name as their status, so any other value counts as running work.
var terminalStatuses = map[string]struct{}{
	models.JobStatusSuccess:        {},
	models.JobStatusFailed:         {},
	models.JobStatusPartialSuccess: {},
	models.JobStatusCancelled:      {},
	models.JobStatusInterrupted:    {},
}

// shouldHideOnClose decides what a window-close request means. Closing while
// work is running keeps the process alive in the tray; an idle app quits.
func shouldHideOnClose(running int) bool {
	return running > 0
}

// closeAction is what a close request resolves to.
type closeAction int

const (
	closeQuit closeAction = iota
	closeHide
)

// closeDecision combines the running count with tray availability. Without a
// usable tray a hidden window could not be restored, so the close falls back to
// the pre-tray behavior and quits even when work is running.
func closeDecision(running int, trayReady bool) closeAction {
	if !trayReady || !shouldHideOnClose(running) {
		return closeQuit
	}

	return closeHide
}

// windowTitle renders the running count for the window title.
func windowTitle(running int) string {
	if running <= 0 {
		return baseWindowTitle
	}
	if running == 1 {
		return baseWindowTitle + " — 1 task running"
	}

	return fmt.Sprintf("%s — %d tasks running", baseWindowTitle, running)
}

// trayTooltip renders the running count for the tray tooltip. macOS ignores
// tray tooltips, so the count also lives in the window title and menu header.
func trayTooltip(running int) string {
	if running <= 0 {
		return baseWindowTitle
	}

	return windowTitle(running)
}

// runningTask is one active unit shown in the tray menu.
type runningTask struct {
	id    string
	kind  string
	label string
}

// Tray owns desktop chrome that outlives the window: the system tray menu, the
// close decision, and the running-task count in the title and tooltip.
type Tray struct {
	app      *application.App
	window   *application.WebviewWindow
	notifier *Notifier
	tooling  *services.ToolingService
	recipes  *recipes.Service
	icon     []byte

	mu             sync.Mutex
	tray           *application.SystemTray
	refreshTimer   *time.Timer
	quitting       bool
	hideNoticeSent bool

	// refreshMu serializes menu rebuilds; mu must not be held while calling
	// Wails APIs, which can bounce through the main thread.
	refreshMu sync.Mutex
}

// NewTray builds the desktop chrome for the main window. icon is the source
// image for the tray icon; it is downscaled before it reaches the platform.
func NewTray(bundle *Services, app *application.App, window *application.WebviewWindow, icon []byte) *Tray {
	tray := &Tray{
		app:      app,
		window:   window,
		notifier: bundle.Notifier,
		icon:     icon,
	}
	if bundle.Lifecycle != nil {
		tray.tooling = bundle.Lifecycle.toolingService
		tray.recipes = bundle.Lifecycle.recipeService
	}

	return tray
}

// Install wires the close hook, the running-count listeners and, when the
// platform can host one, the tray icon. It never panics: a tray failure leaves
// the app usable and restores the old close-quits behavior.
func (t *Tray) Install() {
	// The close hook is installed first so no close request can bypass the
	// running-task decision, even when the tray cannot be created.
	t.window.RegisterHook(events.Common.WindowClosing, t.onWindowClosing)
	t.window.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
		t.mu.Lock()
		t.hideNoticeSent = false
		t.mu.Unlock()
	})
	// Cmd+Q, the macOS app menu and a failed tray all quit through Wails
	// directly; stop the debounce so a rebuild cannot touch a dying window.
	t.app.OnShutdown(func() {
		t.mu.Lock()
		t.quitting = true
		if t.refreshTimer != nil {
			t.refreshTimer.Stop()
		}
		t.mu.Unlock()
	})

	t.installTray()

	t.app.Event.On(services.JobStateEventName, func(*application.CustomEvent) {
		t.scheduleRefresh()
	})
	t.app.Event.On(recipes.StateEventName, func(*application.CustomEvent) {
		t.scheduleRefresh()
	})

	t.scheduleRefresh()
}

// installTray creates the tray when the platform can render one.
func (t *Tray) installTray() {
	supported, reason := traySupported()
	if !supported {
		log.Printf("tray.disabled reason=%q close_behavior=quit", reason)
		return
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			// A broken tray must never abort startup: fall back to close-quits.
			t.mu.Lock()
			t.tray = nil
			t.mu.Unlock()
			log.Printf("tray.create.failed err=%v close_behavior=quit", recovered)
		}
	}()

	tray := t.app.SystemTray.New()
	if label := trayLabel(); label != "" {
		tray.SetLabel(label)
	}
	tray.SetIcon(resizeTrayIcon(t.icon))
	tray.SetTooltip(trayTooltip(0))
	tray.SetMenu(t.buildMenu(nil))
	// A single click opens the menu where the platform delegates clicks to Go
	// (Windows, macOS); Linux hosts render the dbusmenu themselves. Double
	// click is the shortcut back to the window.
	tray.OnClick(tray.OpenMenu)
	tray.OnRightClick(tray.OpenMenu)
	tray.OnDoubleClick(t.showWindow)

	t.mu.Lock()
	t.tray = tray
	t.mu.Unlock()

	log.Printf("tray.created")
}

// onWindowClosing intercepts every close request and decides between hiding
// with live work and quitting explicitly.
func (t *Tray) onWindowClosing(event *application.WindowEvent) {
	t.mu.Lock()
	quitting := t.quitting
	trayReady := t.tray != nil
	t.mu.Unlock()

	// During shutdown the windows must really close.
	if quitting {
		return
	}

	running := len(t.runningTasks())
	// The window is always cancelled here: the app quits through its own
	// shutdown path instead of the platform's last-window rule.
	event.Cancel()

	if closeDecision(running, trayReady) == closeQuit {
		if !trayReady && running > 0 {
			log.Printf("tray.close.quit_with_running_tasks running=%d", running)
		}
		go t.Quit()
		return
	}

	t.window.Hide()
	t.notifyHiddenOnce(running)
	log.Printf("tray.close.hidden running=%d", running)
}

// Quit stops the process explicitly. Wails runs ServiceShutdown on every
// service during application shutdown, so stores are flushed on the way out.
func (t *Tray) Quit() {
	t.mu.Lock()
	if t.quitting {
		t.mu.Unlock()
		return
	}
	t.quitting = true
	if t.refreshTimer != nil {
		t.refreshTimer.Stop()
	}
	t.mu.Unlock()

	log.Printf("app.quit.requested")
	t.app.Quit()
}

// showWindow brings the window back from the tray or a notification click.
func (t *Tray) showWindow() {
	t.mu.Lock()
	t.hideNoticeSent = false
	t.mu.Unlock()

	t.window.Show()
	t.window.Focus()
}

// notifyHiddenOnce tells the user where the app went, once per hide. The flag
// resets when the window is shown again.
func (t *Tray) notifyHiddenOnce(running int) {
	t.mu.Lock()
	if t.hideNoticeSent {
		t.mu.Unlock()
		return
	}
	t.hideNoticeSent = true
	t.mu.Unlock()

	if t.notifier == nil {
		return
	}

	body := fmt.Sprintf("%d tasks continue in the background. Use the FileForge tray icon to reopen the window or quit.", running)
	if running == 1 {
		body = "1 task continues in the background. Use the FileForge tray icon to reopen the window or quit."
	}
	if err := t.notifier.Notify("FileForge is still running", body); err != nil {
		log.Printf("tray.hide.notify_failed err=%v", err)
	}
}

// scheduleRefresh debounces menu and title updates after a state change.
func (t *Tray) scheduleRefresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quitting {
		return
	}
	if t.refreshTimer != nil {
		t.refreshTimer.Stop()
	}
	t.refreshTimer = time.AfterFunc(trayRefreshDelay, t.refresh)
}

// refresh rebuilds the title, tooltip and tray menu from the live state.
func (t *Tray) refresh() {
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()

	t.mu.Lock()
	if t.quitting {
		t.mu.Unlock()
		return
	}
	tray := t.tray
	t.mu.Unlock()

	tasks := t.runningTasks()
	t.window.SetTitle(windowTitle(len(tasks)))
	if tray == nil {
		return
	}

	tray.SetTooltip(trayTooltip(len(tasks)))
	tray.SetMenu(t.buildMenu(tasks))
}

// runningTasks lists active direct jobs and recipe runs. Recipe-step jobs are
// already filtered out by the tooling service, so a recipe is counted once.
func (t *Tray) runningTasks() []runningTask {
	tasks := make([]runningTask, 0, 4)

	if t.tooling != nil {
		for _, job := range t.tooling.ListJobs(0).Jobs {
			if _, terminal := terminalStatuses[job.Status]; terminal {
				continue
			}
			tasks = append(tasks, runningTask{
				id:    job.JobID,
				kind:  taskKindJob,
				label: t.toolLabel(job.ToolID),
			})
		}
	}

	if t.recipes != nil {
		names := t.recipeNames()
		for _, run := range t.recipes.ListRuns(0) {
			if _, terminal := terminalStatuses[run.Status]; terminal {
				continue
			}
			label := names[run.RecipeID]
			if label == "" {
				label = run.RecipeID
			}
			tasks = append(tasks, runningTask{id: run.JobID, kind: taskKindRecipe, label: label})
		}
	}

	return tasks
}

// toolLabel resolves the display name for a tool id, falling back to the id.
func (t *Tray) toolLabel(toolID string) string {
	if t.tooling == nil {
		return toolID
	}
	registry := t.tooling.Registry()
	if registry == nil {
		return toolID
	}
	tool, err := registry.GetTool(toolID)
	if err != nil || tool == nil {
		return toolID
	}
	if name := tool.Manifest().Name; name != "" {
		return name
	}

	return toolID
}

// recipeNames maps recipe ids to their display names.
func (t *Tray) recipeNames() map[string]string {
	names := make(map[string]string)
	if t.recipes == nil {
		return names
	}
	for _, recipe := range t.recipes.List() {
		names[recipe.ID] = recipe.Name
	}

	return names
}

// buildMenu renders the tray menu: window actions, the running-task section and
// an explicit quit.
func (t *Tray) buildMenu(tasks []runningTask) *application.Menu {
	menu := t.app.NewMenu()
	menu.Add("Show window").OnClick(func(*application.Context) {
		t.showWindow()
	})
	menu.Add("Hide window").OnClick(func(*application.Context) {
		t.window.Hide()
	})
	menu.AddSeparator()

	header := "Running tasks"
	switch len(tasks) {
	case 0:
		header = "No running tasks"
	case 1:
		header = "Running tasks (1)"
	default:
		header = fmt.Sprintf("Running tasks (%d)", len(tasks))
	}
	menu.Add(header).SetEnabled(false)

	for _, task := range tasks {
		taskMenu := menu.AddSubmenu(task.label)
		taskMenu.Add("Show in window").OnClick(func(*application.Context) {
			t.showWindow()
		})
		taskMenu.Add("Cancel task").OnClick(func(*application.Context) {
			t.cancelTask(task)
		})
	}

	menu.AddSeparator()
	menu.Add("Quit FileForge").OnClick(func(*application.Context) {
		t.Quit()
	})

	return menu
}

// cancelTask requests cancellation of one active job or recipe run.
func (t *Tray) cancelTask(task runningTask) {
	switch task.kind {
	case taskKindRecipe:
		if t.recipes != nil {
			t.recipes.Cancel(task.id)
		}
	default:
		if t.tooling != nil {
			t.tooling.CancelJob(task.id)
		}
	}

	log.Printf("tray.cancel.requested kind=%s id=%s", task.kind, task.id)
	t.scheduleRefresh()
}
