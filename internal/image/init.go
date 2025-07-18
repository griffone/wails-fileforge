package image

import (
	"fileforge-desktop/internal/interfaces"
	"fileforge-desktop/internal/registry"
	"fmt"
)

const Category = "img" // Export the category constant

func init() {
	// Register the image converter with the global registry
	if err := RegisterWithRegistry(registry.GlobalRegistry); err != nil {
		fmt.Printf("Failed to register image converter: %v\n", err)
	}
}

// RegisterWithRegistry registers the image converter with a specific registry
func RegisterWithRegistry(reg *registry.Registry) error {
	converter := NewImageConverter()
	if converter == nil {
		return fmt.Errorf("failed to create image converter")
	}

	return reg.Register(Category, converter)
}

// GetConverter returns a new image converter instance
func GetConverter() interfaces.Converter {
	return NewImageConverter()
}

// IsRegistered checks if the image converter is registered in the global registry
func IsRegistered() bool {
	return registry.GlobalRegistry.Exists(Category)
}

// Add validation method to your converter
func (c *ImageConverter) Validate() error {
	// Add any necessary validation logic
	return nil
}
