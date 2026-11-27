package registry

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/tools"
)

type Registry struct {
	mu          sync.RWMutex
	tools       map[string]tools.Tool
	initialized bool
	initOnce    sync.Once
	initErrors  []error
}

// NewRegistry creates a new registry instance
func NewRegistry() *Registry {
	return &Registry{
		tools:       make(map[string]tools.Tool),
		initialized: false,
		initErrors:  make([]error, 0),
	}
}

// Global registry with safe initialization
var (
	globalRegistry *Registry
	globalOnce     sync.Once
)

// GetGlobalRegistry returns the global registry, ensuring it's properly initialized
func GetGlobalRegistry() *Registry {
	globalOnce.Do(func() {
		globalRegistry = NewRegistry()
		globalRegistry.markInitialized()
	})
	return globalRegistry
}

// markInitialized marks the registry as initialized
func (r *Registry) markInitialized() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initialized = true
}

// IsInitialized returns whether the registry has been properly initialized
func (r *Registry) IsInitialized() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.initialized
}

// GetInitializationErrors returns any errors that occurred during initialization
func (r *Registry) GetInitializationErrors() []error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Return a copy to prevent external modification
	errors := make([]error, len(r.initErrors))
	copy(errors, r.initErrors)
	return errors
}

// AddInitializationError adds an error to the initialization error list (thread-safe)
func (r *Registry) AddInitializationError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initErrors = append(r.initErrors, err)
}

// WaitForInitialization blocks until the registry is initialized
// This is useful when you need to ensure the registry is ready before use
func (r *Registry) WaitForInitialization() {
	r.initOnce.Do(func() {
		// This ensures that if WaitForInitialization is called before
		// the global registry initialization, it will trigger it
		if r == globalRegistry {
			r.markInitialized()
		}
	})
}

// RegisterTool adds a Tool to the registry.
func (r *Registry) RegisterTool(tool tools.Tool) error {
	if tool == nil {
		return fmt.Errorf("tool cannot be nil")
	}

	id := strings.TrimSpace(tool.ID())
	if id == "" {
		return fmt.Errorf("tool id cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[id]; exists {
		return fmt.Errorf("tool '%s' already exists", id)
	}

	r.tools[id] = tool
	return nil
}

// SafeRegisterTool stores registration errors in initErrors.
func (r *Registry) SafeRegisterTool(tool tools.Tool) {
	if err := r.RegisterTool(tool); err != nil {
		toolID := "<nil>"
		if tool != nil {
			toolID = tool.ID()
		}
		r.AddInitializationError(fmt.Errorf("failed to register tool '%s': %w", toolID, err))
	}
}

// GetTool resolves one tool. A failed or duplicate registration only removes
// that tool id; it never blocks lookups of every other tool. Registration
// errors stay available through GetInitializationErrors for startup logging.
func (r *Registry) GetTool(toolID string) (tools.Tool, error) {
	if !r.IsInitialized() {
		r.WaitForInitialization()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[toolID]
	if !ok {
		return nil, fmt.Errorf("tool '%s' not found", toolID)
	}

	return tool, nil
}

func (r *Registry) ListTools(ctx context.Context) []models.ToolCatalogEntry {
	if !r.IsInitialized() {
		r.WaitForInitialization()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	toolIDs := make([]string, 0, len(r.tools))
	for toolID := range r.tools {
		toolIDs = append(toolIDs, toolID)
	}
	sort.Strings(toolIDs)

	catalog := make([]models.ToolCatalogEntry, 0, len(toolIDs))
	for _, toolID := range toolIDs {
		tool := r.tools[toolID]
		catalog = append(catalog, models.ToolCatalogEntry{
			Manifest: tool.Manifest(),
			State:    tool.RuntimeState(ctx),
		})
	}

	return catalog
}
