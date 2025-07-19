package image

import (
	"fileforge-desktop/internal/interfaces"
	"fileforge-desktop/internal/registry"
	"log"
)

const Category = "img" // Export the category constant

func init() {
	// Use SafeRegister for init functions - errors are stored in the registry
	registry.GetGlobalRegistry().SafeRegister(Category, NewImageConverter())

	// Optionally log any initialization errors (non-blocking)
	go func() {
		reg := registry.GetGlobalRegistry()
		reg.WaitForInitialization()
		if errors := reg.GetInitializationErrors(); len(errors) > 0 {
			for _, err := range errors {
				log.Printf("Registry initialization error: %v", err)
			}
		}
	}()
}

// GetConverter returns a new image converter instance
func GetConverter() interfaces.Converter {
	return NewImageConverter()
}

// IsRegistered checks if the image converter is registered in the global registry
func IsRegistered() bool {
	return registry.GetGlobalRegistry().Exists(Category)
}

// Add validation method to your converter
func (c *ImageConverter) Validate() error {
	// Add any necessary validation logic
	return nil
}
