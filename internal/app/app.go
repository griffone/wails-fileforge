package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fileforge-desktop/internal/automation"
	_ "fileforge-desktop/internal/doc"
	_ "fileforge-desktop/internal/image"
	"fileforge-desktop/internal/media"
	"fileforge-desktop/internal/models"
	_ "fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/preview"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	"fileforge-desktop/internal/toolchain"
	_ "fileforge-desktop/internal/video"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Services bundles every bound Wails service. main.go registers each field so
// the frontend gets one module per domain; the bundle itself is not bound.
type Services struct {
	Lifecycle  *App
	Tools      *ToolService
	Recipes    *RecipeService
	Automation *AutomationService
	Toolchain  *ToolchainService
	Preview    *PreviewService
	System     *SystemService
	Notifier   *Notifier
}

// App is the composition root. It owns the underlying domain services, the
// shared lifetime runtime, and the Wails lifecycle hooks. It exposes no RPC
// methods of its own.
type App struct {
	runtime           *Runtime
	toolingService    *services.ToolingService
	recipeService     *recipes.Service
	automationService *automation.Service
	previewService    *preview.PreviewService
	toolchainService  *toolchain.Service
}

// New builds the full service graph and returns the bound-service bundle.
func New(notifier *Notifier) *Services {
	reg := registry.GetGlobalRegistry()
	runtime := &Runtime{}
	toolingService := services.NewToolingServiceWithBus(reg, WailsBus{})

	recipeService := recipes.NewServiceWithPersistence(reg, toolingService.JobHistory(), func(evt models.RecipeProgressEvent) {
		WailsBus{}.Emit(recipes.ProgressEventName, evt)
	}, recipes.DefaultPath())
	recipeService.SetUserRecipesPath(recipes.DefaultUserRecipesPath())
	// Recipe steps run on the same engine as direct tool jobs so one scheduler
	// governs queueing, cancellation, and resource classes.
	recipeService.SetEngine(toolingService.Engine())
	recipeService.SetStateEmitter(func(result models.RecipeResult) {
		WailsBus{}.Emit(recipes.StateEventName, result)
	})
	automationStore := automation.NewStore(automation.DefaultPath())
	automationService := automation.NewService(automationStore, recipeService, func(evt models.AutomationRunEvent) {
		WailsBus{}.Emit(automation.RunEventName, evt)
	})
	automationService.SetRunStore(automation.NewRunStore(automation.DefaultRunPath()))
	// initialize preview service with sensible defaults; AllowedRoots: $HOME + TMP
	home, _ := os.UserHomeDir()
	allowed := []string{}
	if home != "" {
		allowed = append(allowed, home)
	}
	allowed = append(allowed, os.TempDir())
	// Media streaming shares the preview roots: it may only register files the
	// user could already preview.
	media.SetAllowedRoots(allowed)
	previewService := preview.NewPreviewService(preview.Config{AllowedRoots: allowed, MaxQueue: 1024})
	toolchainService := newToolchainService()

	lifecycle := &App{
		runtime:           runtime,
		toolingService:    toolingService,
		recipeService:     recipeService,
		automationService: automationService,
		previewService:    previewService,
		toolchainService:  toolchainService,
	}

	return &Services{
		Lifecycle:  lifecycle,
		Tools:      &ToolService{tooling: toolingService, registry: reg, runtime: runtime},
		Recipes:    &RecipeService{service: recipeService, runtime: runtime},
		Automation: &AutomationService{service: automationService},
		Toolchain:  &ToolchainService{service: toolchainService, runtime: runtime},
		Preview:    &PreviewService{service: previewService, runtime: runtime},
		System:     &SystemService{runtime: runtime, notifier: notifier},
		Notifier:   notifier,
	}
}

// newToolchainService builds the managed toolchain installer for the running
// machine. The installer is best-effort infrastructure: an unresolvable data
// directory falls back to the user cache directory so app startup never fails.
func newToolchainService() *toolchain.Service {
	dataDir, err := toolchain.DataDir()
	if err != nil || strings.TrimSpace(dataDir) == "" {
		if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && strings.TrimSpace(cacheDir) != "" {
			dataDir = filepath.Join(cacheDir, "fileforge")
		} else {
			dataDir = filepath.Join(os.TempDir(), "fileforge")
		}
	}

	return toolchain.NewService(
		dataDir,
		toolchain.CurrentPlatform(),
		&toolchain.HTTPDownloader{},
		&toolchain.ExecCommandRunner{},
		func(evt models.ToolchainInstallEvent) {
			WailsBus{}.Emit(toolchain.InstallEventName, evt)
		},
	)
}

// ServiceStartup wires the Wails application context into the app services so
// job persistence and interrupted-job recovery become active.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.runtime.Set(ctx)
	if a.toolingService != nil {
		a.toolingService.SetContext(ctx)
	}
	// The managed toolchain is optional infrastructure: applying the managed
	// PATH and starting the first-run install must never abort app startup.
	if a.toolchainService != nil {
		_ = a.toolchainService.ApplyEnvironment()
		a.toolchainService.StartIfNeeded(ctx)
	}
	// Automation rules are optional: a broken rule or watch folder is reported
	// as degraded and must never abort app startup.
	if a.automationService != nil {
		a.automationService.Start(ctx)
	}
	return nil
}

// ServiceShutdown gracefully stops services owned by App.
func (a *App) ServiceShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.shutdown(ctx)
}

// shutdown gracefully stops services owned by App in dependency order.
func (a *App) shutdown(ctx context.Context) error {
	var firstErr error
	if a.toolchainService != nil {
		a.toolchainService.Cancel()
	}
	if a.automationService != nil {
		a.automationService.Shutdown()
	}
	if a.recipeService != nil {
		a.recipeService.Shutdown()
	}
	// shutdown preview service
	if a.previewService != nil {
		if err := a.previewService.Shutdown(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("app: %w", err)
			}
		}
	}
	if err := media.CloseServer(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("app media server: %w", err)
	}
	if a.toolingService != nil {
		if err := a.toolingService.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("app: %w", err)
		}
	}
	return firstErr
}
