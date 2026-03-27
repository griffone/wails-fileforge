package image

import (
	"fileforge-desktop/internal/registry"
	"log"
)

func init() {
	adapter := NewImageToolAdapter(NewImageConverter())
	cropTool := NewCropTool()

	registry.GetGlobalRegistry().SafeRegisterToolV2(adapter)
	registry.GetGlobalRegistry().SafeRegisterToolV2(cropTool)

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

// Add validation method to your converter
func (c *ImageConverter) Validate() error {
	// Add any necessary validation logic
	return nil
}
