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

// ConvertFile converts a single file based on the provided request
func (s *ConversionService) ConvertFile(req models.ConversionRequest) (models.ConversionResult, error) {
	if req.InputPath == "" {
		return models.ConversionResult{}, fmt.Errorf("input path cannot be empty")
	}
	if req.Category == "" {
		return models.ConversionResult{}, fmt.Errorf("category cannot be empty")
	}

	converter, err := registry.GlobalRegistry.Get(req.Category)
	if err != nil {
		return models.ConversionResult{}, fmt.Errorf("converter not found: %w", err)
	}

	// Generate output path if not provided
	outputPath := req.OutputPath
	if outputPath == "" {
		ext := filepath.Ext(req.InputPath)
		baseName := strings.TrimSuffix(filepath.Base(req.InputPath), ext)
		dir := filepath.Dir(req.InputPath)
		outputPath = filepath.Join(dir, fmt.Sprintf("%s.%s", baseName, req.Format))
	}

	// Convert the file
	err = converter.ConvertSingle(req.InputPath, outputPath, req.Format)
	if err != nil {
		return models.ConversionResult{}, fmt.Errorf("conversion failed: %w", err)
	}

	return models.ConversionResult{
		Success:    true,
		Message:    "Conversion successful",
		OutputPath: outputPath,
	}, nil
}

// ConvertBatch converts multiple files in batch
func (s *ConversionService) ConvertBatch(req models.BatchConversionRequest) (models.BatchConversionResult, error) {
	// Check if context is available and not cancelled
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return models.BatchConversionResult{}, fmt.Errorf("operation was cancelled")
		default:
		}
	}

	converter, err := registry.GlobalRegistry.Get(req.Category)
	if err != nil {
		return models.BatchConversionResult{}, fmt.Errorf("converter not found: %w", err)
	}

	// Use the interface method
	results, err := converter.ConvertBatch(req.InputPaths, req.OutputDir, req.Format, req.KeepStructure, req.Workers)
	if err != nil {
		return models.BatchConversionResult{}, fmt.Errorf("batch conversion failed: %w", err)
	}

	totalFiles := len(results)
	successCount := 0
	failureCount := 0

	// Convert from interfaces.ConversionResult to models.ConversionResult
	modelResults := make([]models.ConversionResult, len(results))
	for i, result := range results {
		errorMsg := result.Error
		modelResults[i] = models.ConversionResult{
			Success:    result.Success,
			Message:    result.Message,
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
	}, nil
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
