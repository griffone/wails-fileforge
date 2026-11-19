package app

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Notifier wraps the Wails notifications service. Notifications are optional
// infrastructure: a platform without a notification daemon must never abort app
// startup, so startup and shutdown failures are logged and swallowed.
type Notifier struct {
	service *notifications.NotificationService

	mu    sync.RWMutex
	ready bool
}

var (
	_ application.ServiceStartup  = (*Notifier)(nil)
	_ application.ServiceShutdown = (*Notifier)(nil)
)

// NewNotifier builds the notification bridge around the Wails service.
func NewNotifier() *Notifier {
	return &Notifier{service: notifications.New()}
}

// ServiceStartup starts the underlying notification service. Any failure is
// logged and leaves the notifier disabled; it never fails app startup.
func (n *Notifier) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	if n.service == nil {
		return nil
	}

	if err := n.service.ServiceStartup(ctx, options); err != nil {
		log.Printf("notifications.startup.failed err=%v", err)
		n.setReady(false)
		return nil
	}

	n.setReady(true)
	n.service.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			log.Printf("notifications.response.failed err=%v", result.Error)
			return
		}

		// Clicking a notification should bring the app back to the front.
		app := application.Get()
		if app == nil || app.Window == nil {
			return
		}
		window := app.Window.Current()
		if window == nil {
			return
		}
		window.Show()
		window.Focus()
	})

	return nil
}

// ServiceShutdown stops the underlying service when it started successfully.
// It is always best-effort and returns nil.
func (n *Notifier) ServiceShutdown() error {
	if n.service == nil {
		return nil
	}

	n.mu.RLock()
	ready := n.ready
	n.mu.RUnlock()
	if !ready {
		return nil
	}

	if err := n.service.ServiceShutdown(); err != nil {
		log.Printf("notifications.shutdown.failed err=%v", err)
	}
	n.setReady(false)

	return nil
}

// Notify sends a desktop notification. Empty inputs and an unavailable
// notification service are reported as errors instead of being silenced.
func (n *Notifier) Notify(title, body string) error {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || body == "" {
		return fmt.Errorf("a notification needs a title and a body")
	}
	if n.service == nil {
		return fmt.Errorf("notifications are not available")
	}

	n.mu.RLock()
	ready := n.ready
	n.mu.RUnlock()
	if !ready {
		return fmt.Errorf("notifications are not available")
	}

	options := notifications.NotificationOptions{
		ID:    fmt.Sprintf("fileforge-%d", time.Now().UnixNano()),
		Title: title,
		Body:  body,
	}
	if err := n.service.SendNotification(options); err != nil {
		return fmt.Errorf("send notification: %w", err)
	}

	return nil
}

func (n *Notifier) setReady(ready bool) {
	n.mu.Lock()
	n.ready = ready
	n.mu.Unlock()
}
