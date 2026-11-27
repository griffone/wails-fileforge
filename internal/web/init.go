package web

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterTool(NewDownloadTool())
}
