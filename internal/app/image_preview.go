package app

import (
	"strings"

	"fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func (a *App) GetImagePreviewSourceV1(inputPath string) models.ImagePreviewSourceResponseV1 {
	tool, ok := a.imageCropTool()
	if !ok {
		return models.ImagePreviewSourceResponseV1{
			Success: false,
			Message: "Image crop tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.crop is not registered", nil),
		}
	}

	return tool.GetImagePreviewSource(strings.TrimSpace(inputPath))
}

func (a *App) GetImageCropPreviewV1(req models.ImageCropPreviewRequestV1) models.ImageCropPreviewResponseV1 {
	tool, ok := a.imageCropTool()
	if !ok {
		return models.ImageCropPreviewResponseV1{
			Success: false,
			Message: "Image crop tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.crop is not registered", nil),
		}
	}

	return tool.GetImageCropPreview(req)
}

func (a *App) GetImageAnnotatePreviewV1(req models.ImageAnnotatePreviewRequestV1) models.ImageAnnotatePreviewResponseV1 {
	tool, ok := a.imageAnnotateTool()
	if !ok {
		return models.ImageAnnotatePreviewResponseV1{
			Success: false,
			Message: "Image annotate tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.annotate is not registered", nil),
		}
	}

	return tool.GetImageAnnotatePreviewV1(req)
}

func (a *App) imageCropTool() (*image.CropTool, bool) {
	reg := registry.GetGlobalRegistry()
	rawTool, err := reg.GetToolV2(image.ToolIDImageCropV1)
	if err != nil {
		return nil, false
	}

	cropTool, ok := rawTool.(*image.CropTool)
	if !ok {
		return nil, false
	}

	return cropTool, true
}

func (a *App) imageAnnotateTool() (*image.AnnotateTool, bool) {
	reg := registry.GetGlobalRegistry()
	rawTool, err := reg.GetToolV2(image.ToolIDImageAnnotateV1)
	if err != nil {
		return nil, false
	}

	annotateTool, ok := rawTool.(*image.AnnotateTool)
	if !ok {
		return nil, false
	}

	return annotateTool, true
}
