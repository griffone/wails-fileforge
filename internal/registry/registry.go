package registry

import (
	"fileforge-desktop/internal/interfaces"
	"fmt"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	converters  map[string]interfaces.Converter
	initialized bool
	initOnce    sync.Once
	initErrors  []error
}

// NewRegistry creates a new registry instance
func NewRegistry() *Registry {
	return &Registry{
		converters:  make(map[string]interfaces.Converter),
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

// Register adds a converter to the registry
func (r *Registry) Register(category string, converter interfaces.Converter) error {
	if category == "" {
		return fmt.Errorf("category cannot be empty")
	}
	if converter == nil {
		return fmt.Errorf("converter cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.converters[category]; exists {
		return fmt.Errorf("converter for category '%s' already exists", category)
	}

	r.converters[category] = converter
	return nil
}

// SafeRegister is like Register but adds errors to the initialization error list instead of returning them
func (r *Registry) SafeRegister(category string, converter interfaces.Converter) {
	if err := r.Register(category, converter); err != nil {
		r.AddInitializationError(fmt.Errorf("failed to register converter '%s': %w", category, err))
	}
}

// RegisterOrReplace adds a converter, replacing any existing one
func (r *Registry) RegisterOrReplace(category string, converter interfaces.Converter) error {
	if category == "" {
		return fmt.Errorf("category cannot be empty")
	}
	if converter == nil {
		return fmt.Errorf("converter cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.converters[category] = converter
	return nil
}

// Unregister removes a converter from the registry
func (r *Registry) Unregister(category string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.converters[category]; !exists {
		return fmt.Errorf("converter for category '%s' not found", category)
	}

	delete(r.converters, category)
	return nil
}

// Get retrieves a converter by category
func (r *Registry) Get(category string) (interfaces.Converter, error) {
	// Ensure registry is initialized
	if !r.IsInitialized() {
		r.WaitForInitialization()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check for initialization errors first
	if len(r.initErrors) > 0 {
		return nil, fmt.Errorf("registry has initialization errors: %v", r.initErrors)
	}

	converter, exists := r.converters[category]
	if !exists {
		// Get available categories while still holding the lock
		available := r.getAvailableCategoriesSafe()
		return nil, fmt.Errorf("converter for category '%s' not found. Available: %v", category, available)
	}
	return converter, nil
}

// Exists checks if a converter exists for the given category
func (r *Registry) Exists(category string) bool {
	// Ensure registry is initialized
	if !r.IsInitialized() {
		r.WaitForInitialization()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.converters[category]
	return exists
}

// GetAllCategories returns all registered categories
func (r *Registry) GetAllCategories() []string {
	// Ensure registry is initialized
	if !r.IsInitialized() {
		r.WaitForInitialization()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getAvailableCategoriesSafe()
}

// Count returns the number of registered converters
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.converters)
}

// Clear removes all converters from the registry
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.converters = make(map[string]interfaces.Converter)
}

// getAvailableCategoriesSafe returns available categories (must be called with lock held)
func (r *Registry) getAvailableCategoriesSafe() []string {
	if len(r.converters) == 0 {
		return []string{}
	}

	// Pre-allocate slice with known capacity to avoid multiple allocations
	categories := make([]string, 0, len(r.converters))
	for category := range r.converters {
		categories = append(categories, category)
	}
	return categories
}

// GetSnapshot returns a copy of all converters (useful for testing/debugging)
func (r *Registry) GetSnapshot() map[string]interfaces.Converter {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]interfaces.Converter, len(r.converters))
	for k, v := range r.converters {
		snapshot[k] = v
	}
	return snapshot
}

// HealthCheck performs a comprehensive health check of the registry
func (r *Registry) HealthCheck() error {
	if !r.IsInitialized() {
		return fmt.Errorf("registry is not initialized")
	}

	initErrors := r.GetInitializationErrors()
	if len(initErrors) > 0 {
		return fmt.Errorf("registry has %d initialization error(s): %v", len(initErrors), initErrors)
	}

	r.mu.RLock()
	converterCount := len(r.converters)
	r.mu.RUnlock()

	if converterCount == 0 {
		return fmt.Errorf("no converters registered")
	}

	return nil
}

// MustBeHealthy panics if the registry is not healthy
func (r *Registry) MustBeHealthy() {
	if err := r.HealthCheck(); err != nil {
		panic(fmt.Sprintf("registry health check failed: %v", err))
	}
}
