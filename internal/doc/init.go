package doc

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewMDToPDFTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewDOCXToPDFTool())
}
