package pdf

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterTool(NewMergeTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewSplitTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewCropTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewOptimizeTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewOCRTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewImagesToPDFTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewPDFToDOCXTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewRotateTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewRemovePagesTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewReorderTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewExtractImagesTool())
}
