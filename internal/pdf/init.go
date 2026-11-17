package pdf

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewMergeTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewSplitTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewCropTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewOptimizeTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewOCRTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewImagesToPDFTool())
}
