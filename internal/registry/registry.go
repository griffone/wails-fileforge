package registry

import (
	"fileforge-desktop/internal/interfaces"
	"fmt"
	"sync"
)

type Registry struct {
	mu         sync.RWMutex
	converters map[string]interfaces.Converter
}

// NewRegistry creates a new registry instance
func NewRegistry() *Registry {
	return &Registry{
		converters: make(map[string]interfaces.Converter),
	}
}

// Global registry
var GlobalRegistry = NewRegistry()

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
	r.mu.RLock()
	defer r.mu.RUnlock()

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
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.converters[category]
	return exists
}

// GetAllCategories returns all registered categories
func (r *Registry) GetAllCategories() []string {
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
