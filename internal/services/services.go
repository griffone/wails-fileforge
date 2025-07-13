package services

import (
	"context"
	"fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fmt"
	"path/filepath"
	"strings"
)

type ConversionService struct {
	ctx context.Context
}

func NewConversionService() *ConversionService {
	return &ConversionService{}
}

func (s *ConversionService) SetContext(ctx context.Context) {
	s.ctx = ctx
}

func (s *ConversionService) ConvertFile(req models.ConversionRequest) models.ConversionResult {
	converter, err := registry.GlobalRegistry.Get(req.Category)
	if err != nil {
		return models.ConversionResult{
			Success: false,
			Message: fmt.Sprintf("Converter not found: %v", err),
		}
	}

	// Generate output path if not provided
	outputPath := req.OutputPath
	if outputPath == "" {
		ext := filepath.Ext(req.InputPath)
		baseName := strings.TrimSuffix(filepath.Base(req.InputPath), ext)
		dir := filepath.Dir(req.InputPath)
		outputPath = filepath.Join(dir, fmt.Sprintf("%s.%s", baseName, req.Format))
	}

	// For image converter, use the ConvertSingle method if available
	if imageConverter, ok := converter.(*image.ImageConverter); ok {
		err = imageConverter.ConvertSingle(req.InputPath, outputPath, req.Format)
		if err != nil {
			return models.ConversionResult{
				Success: false,
				Message: fmt.Sprintf("Conversion failed: %v", err),
			}
		}
	} else {
		return models.ConversionResult{
			Success: false,
			Message: "Generic conversion not implemented yet",
		}
	}

	return models.ConversionResult{
		Success:    true,
		Message:    "Conversion successful",
		OutputPath: outputPath,
	}
}

// ConvertBatch converts multiple files in batch
func (s *ConversionService) ConvertBatch(req models.BatchConversionRequest) models.BatchConversionResult {
	converter, err := registry.GlobalRegistry.Get(req.Category)
	if err != nil {
		return models.BatchConversionResult{
			Success: false,
			Message: fmt.Sprintf("Converter not found: %v", err),
		}
	}

	// For image converter, use the ConvertBatch method if available
	if imageConverter, ok := converter.(*image.ImageConverter); ok {
		results := imageConverter.ConvertBatch(req.InputPaths, req.OutputDir, req.Format, req.KeepStructure)

		totalFiles := len(results)
		successCount := 0
		failureCount := 0

		for _, result := range results {
			if result.Success {
				successCount++
			} else {
				failureCount++
			}
		}

		success := failureCount == 0
		message := fmt.Sprintf("Batch conversion completed: %d successful, %d failed out of %d files",
			successCount, failureCount, totalFiles)

		return models.BatchConversionResult{
			Success:      success,
			Message:      message,
			TotalFiles:   totalFiles,
			SuccessCount: successCount,
			FailureCount: failureCount,
			Results:      results,
		}
	} else {
		return models.BatchConversionResult{
			Success: false,
			Message: "Batch conversion not implemented for this converter type",
		}
	}
}

func (s *ConversionService) GetSupportedFormats() []models.SupportedFormat {
	categories := registry.GlobalRegistry.GetAllCategories()
	var result []models.SupportedFormat

	for _, category := range categories {
		converter, err := registry.GlobalRegistry.Get(category)
		if err != nil {
			continue
		}

		result = append(result, models.SupportedFormat{
			Category: category,
			Formats:  converter.SupportedFormats(),
		})
	}

	return result
}
