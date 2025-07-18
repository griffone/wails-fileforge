package services

import (
	"context"
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

	if req.InputPath == "" {
		return models.ConversionResult{
			Success: false,
			Error:   "input path cannot be empty",
		}
	}
	if req.Category == "" {
		return models.ConversionResult{
			Success: false,
			Error:   "category cannot be empty",
		}
	}

	converter, err := registry.GlobalRegistry.Get(req.Category)
	if err != nil {
		return models.ConversionResult{
			Success: false,
			Error:   fmt.Sprintf("Converter not found: %v", err),
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
	err = converter.ConvertSingle(req.InputPath, outputPath, req.Format)
	if err != nil {
		return models.ConversionResult{
			Success: false,
			Error:   fmt.Sprintf("Conversion failed: %v", err),
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
	// Check if context is available and not cancelled
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return models.BatchConversionResult{
				Success: false,
				Error:   "operation was cancelled",
			}
		default:
		}
	}

	converter, err := registry.GlobalRegistry.Get(req.Category)
	if err != nil {
		return models.BatchConversionResult{
			Success: false,
			Error:   fmt.Sprintf("Converter not found: %v", err),
		}
	}

	// Use the interface method (no type assertion needed!)
	results := converter.ConvertBatch(req.InputPaths, req.OutputDir, req.Format, req.KeepStructure, req.Workers)

	totalFiles := len(results)
	successCount := 0
	failureCount := 0

	// Convert from interfaces.ConversionResult to models.ConversionResult
	modelResults := make([]models.ConversionResult, len(results))
	for i, result := range results {
		errorMsg := result.Error
		modelResults[i] = models.ConversionResult{
			Success:    result.Success,
			Message:    errorMsg,
			OutputPath: result.OutputPath,
			Error:      errorMsg,
		}

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
		Results:      modelResults,
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
