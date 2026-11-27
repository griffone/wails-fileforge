package audio

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterTool(NewExtractTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewConvertTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewNormalizeTool())
}
