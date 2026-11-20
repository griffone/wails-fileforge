package doc

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterTool(NewMDToPDFTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewDOCXToPDFTool())
}
