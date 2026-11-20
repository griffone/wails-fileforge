package image

import (
	"fileforge-desktop/internal/registry"
	"log"
)

func init() {
	adapter := NewImageToolAdapter(NewImageConverter())
	cropTool := NewCropTool()
	annotateTool := NewAnnotateTool()
	compressTool := NewCompressTool()
	watermarkTool := NewWatermarkTool()
	ocrTool := NewImageOCRTool()
	rembgTool := NewBackgroundRemoveTool()

	registry.GetGlobalRegistry().SafeRegisterTool(adapter)
	registry.GetGlobalRegistry().SafeRegisterTool(cropTool)
	registry.GetGlobalRegistry().SafeRegisterTool(annotateTool)
	registry.GetGlobalRegistry().SafeRegisterTool(compressTool)
	registry.GetGlobalRegistry().SafeRegisterTool(watermarkTool)
	registry.GetGlobalRegistry().SafeRegisterTool(ocrTool)
	registry.GetGlobalRegistry().SafeRegisterTool(rembgTool)

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
