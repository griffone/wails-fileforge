package video

import "fileforge-desktop/internal/registry"

func init() {
	registry.GetGlobalRegistry().SafeRegisterTool(NewConvertTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewTrimTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewMergeTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewTranscribeTool())
	registry.GetGlobalRegistry().SafeRegisterTool(NewAudioSyncTool())
}
