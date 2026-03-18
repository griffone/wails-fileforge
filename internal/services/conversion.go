package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	err = converter.ConvertSingle(ctx, req.InputPath, outputPath, req.Format, req.Options)
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

	expandedPaths, outputPaths, err := s.expandPaths(req.InputPaths, req.OutputDir, req.Format, req.KeepStructure)
	if err != nil {
		return models.BatchConversionResult{}, err
	}

	if req.Options == nil {
		req.Options = make(map[string]any)
	}
	req.Options["outputPaths"] = outputPaths

	onProgress := func(result models.ConversionResult) {
		if app := application.Get(); app != nil {
			app.Event.Emit("conversion:progress", result)
		}
	}

	if app := application.Get(); app != nil {
		app.Event.Emit("conversion:start", map[string]any{
			"totalFiles": len(expandedPaths),
		})
	}

	// Use the interface method with context
	results, err := converter.ConvertBatch(ctx, expandedPaths, req.OutputDir, req.Format, req.KeepStructure, req.Workers, req.Options, onProgress)

	if app := application.Get(); app != nil {
		app.Event.Emit("conversion:complete", map[string]any{
			"totalFiles": len(expandedPaths),
			"error":      err,
		})
	}

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
func (s *ConversionService) expandPaths(inputPaths []string, outputDir, format string, keepStructure bool) ([]string, map[string]string, error) {
	var expandedPaths []string
	outputPaths := make(map[string]string)
	usedOutputPaths := make(map[string]int)

	for _, inputPath := range inputPaths {
		info, err := os.Stat(inputPath)
		if err != nil {
			expandedPaths = append(expandedPaths, inputPath)
			outputPaths[inputPath] = s.nextCollisionSafePath(
				filepath.Join(outputDir, s.outputFileName(inputPath, format)),
				usedOutputPaths,
			)
			continue
		}

		if info.IsDir() {
			rootFolderName := filepath.Base(filepath.Clean(inputPath))
			err = filepath.Walk(inputPath, func(path string, fInfo os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !fInfo.IsDir() {
					expandedPaths = append(expandedPaths, path)

					var calculatedOutputPath string
					if keepStructure {
						relPath, err := filepath.Rel(inputPath, path)
						if err == nil {
							calculatedOutputPath = filepath.Join(
								outputDir,
								rootFolderName,
								filepath.Dir(relPath),
								s.outputFileName(path, format),
							)
						}
					}

					if calculatedOutputPath == "" {
						calculatedOutputPath = filepath.Join(outputDir, s.outputFileName(path, format))
					}

					outputPaths[path] = s.nextCollisionSafePath(calculatedOutputPath, usedOutputPaths)
				}
				return nil
			})
			if err != nil {
				return nil, nil, fmt.Errorf("error walking directory: %w", err)
			}
		} else {
			expandedPaths = append(expandedPaths, inputPath)
			outputPaths[inputPath] = s.nextCollisionSafePath(
				filepath.Join(outputDir, s.outputFileName(inputPath, format)),
				usedOutputPaths,
			)
		}
	}

	return expandedPaths, outputPaths, nil
}

func (s *ConversionService) outputFileName(inputPath, format string) string {
	baseName := filepath.Base(inputPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	return fmt.Sprintf("%s.%s", nameWithoutExt, format)
}

func (s *ConversionService) nextCollisionSafePath(basePath string, used map[string]int) string {
	count := used[basePath]
	if count == 0 {
		used[basePath] = 1
		return basePath
	}

	dir := filepath.Dir(basePath)
	fileName := filepath.Base(basePath)
	ext := filepath.Ext(fileName)
	nameWithoutExt := strings.TrimSuffix(fileName, ext)

	for suffix := count + 1; ; suffix++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", nameWithoutExt, suffix, ext))
		if used[candidate] == 0 {
			used[basePath] = suffix
			used[candidate] = 1
			return candidate
		}
	}
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
