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
	// Check if context is available and not cancelled
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return models.ConversionResult{}, fmt.Errorf("operation was cancelled: %w", s.ctx.Err())
		default:
		}
	}

	if req.InputPath == "" {
		return models.ConversionResult{}, fmt.Errorf("input path cannot be empty")
	}
	if req.Category == "" {
		return models.ConversionResult{}, fmt.Errorf("category cannot be empty")
	}

	converter, err := registry.GetGlobalRegistry().Get(req.Category)
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

	// Use context.Background() if service context is nil
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Convert the file with context
	err = converter.ConvertSingle(ctx, req.InputPath, outputPath, req.Format)
	if err != nil {
		return models.ConversionResult{}, fmt.Errorf("conversion failed: %w", err)
	}

	return models.ConversionResult{
		Success:    true,
		Message:    "Conversion successful",
		InputPath:  req.InputPath,
		OutputPath: outputPath,
	}, nil
}

// ConvertBatch converts multiple files in batch
func (s *ConversionService) ConvertBatch(req models.BatchConversionRequest) (models.BatchConversionResult, error) {
	// Check if context is available and not cancelled
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return models.BatchConversionResult{}, fmt.Errorf("operation was cancelled: %w", s.ctx.Err())
		default:
		}
	}

	converter, err := registry.GetGlobalRegistry().Get(req.Category)
	if err != nil {
		return models.BatchConversionResult{}, fmt.Errorf("converter not found: %w", err)
	}

	// Use context.Background() if service context is nil
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Use the interface method with context
	results, err := converter.ConvertBatch(ctx, req.InputPaths, req.OutputDir, req.Format, req.KeepStructure, req.Workers)
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
	categories := registry.GetGlobalRegistry().GetAllCategories()
	var result []models.SupportedFormat

	for _, category := range categories {
		converter, err := registry.GetGlobalRegistry().Get(category)
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
