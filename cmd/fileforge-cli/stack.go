package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "fileforge-desktop/internal/doc"
	_ "fileforge-desktop/internal/image"
	_ "fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
	"fileforge-desktop/internal/toolchain"
	_ "fileforge-desktop/internal/video"
)

// stack is the headless counterpart of internal/app.New. It wires the same
// registry, job engine, tooling service and recipe service without the Wails
// runtime. The blank imports above register every tool domain exactly like
// internal/app/app.go does.
type stack struct {
	registry *registry.Registry
	tooling  *services.ToolingService
	recipes  *recipes.Service
}

// newStack builds the service graph and applies the managed toolchain to the
// process PATH before any tool runs.
func newStack() *stack {
	applyManagedToolchain()

	reg := registry.GetGlobalRegistry()
	tooling := services.NewHeadlessToolingService(reg)

	// Run persistence is disabled: the CLI never writes recipe_runs.json. The
	// user recipe store keeps the app's real location so user recipes created
	// in the app are available here.
	recipeService := recipes.NewServiceWithPersistence(reg, tooling.JobHistory(), nil, "")
	recipeService.SetUserRecipesPath(recipes.DefaultUserRecipesPath())
	recipeService.SetEngine(tooling.Engine())

	return &stack{registry: reg, tooling: tooling, recipes: recipeService}
}

// close stops the services owned by the stack. A run has already reached its
// terminal state before close is called, so this only stops background
// goroutines.
func (s *stack) close() {
	if s == nil {
		return
	}
	if s.recipes != nil {
		s.recipes.Shutdown()
	}
	if s.tooling != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.tooling.Shutdown(ctx)
	}
}

// applyManagedToolchain prepends the managed binaries to PATH, mirroring the
// app's best-effort startup behaviour. A missing data directory leaves the
// system PATH untouched instead of aborting the run.
func applyManagedToolchain() {
	dataDir, err := toolchain.DataDir()
	if err != nil || strings.TrimSpace(dataDir) == "" {
		if cacheDir, cacheErr := os.UserCacheDir(); cacheErr == nil && strings.TrimSpace(cacheDir) != "" {
			dataDir = filepath.Join(cacheDir, "fileforge")
		} else {
			dataDir = filepath.Join(os.TempDir(), "fileforge")
		}
	}

	service := toolchain.NewService(dataDir, toolchain.CurrentPlatform(), nil, nil, nil)
	_ = service.ApplyEnvironment()
}
