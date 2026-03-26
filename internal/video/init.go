package video

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewConvertTool())
	registry.GetGlobalRegistry().SafeRegisterToolV2(NewTrimTool())
}
