package image

import (
	"fileforge-desktop/internal/registry"
)

func init() {
	// Auto-register the image converter with the global registry
	registry.GlobalRegistry.Register("img", NewImageConverter())
}
