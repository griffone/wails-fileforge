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

var GlobalRegistry = &Registry{
	converters: make(map[string]interfaces.Converter),
}

func (r *Registry) Register(category string, converter interfaces.Converter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.converters[category] = converter
}

func (r *Registry) Get(category string) (interfaces.Converter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	converter, exists := r.converters[category]
	if !exists {
		available := r.getAvailableCategoriesUnsafe()
		return nil, fmt.Errorf("converter for category '%s' not found. Available: %v", category, available)
	}
	return converter, nil
}

func (r *Registry) GetAllCategories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getAvailableCategoriesUnsafe()
}

func (r *Registry) getAvailableCategoriesUnsafe() []string {
	var categories []string
	for category := range r.converters {
		categories = append(categories, category)
	}
	return categories
}
