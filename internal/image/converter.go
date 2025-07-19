package image

import (
	"context"
	"fileforge-desktop/internal/interfaces"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/utils"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/h2non/bimg"
)

const (
	DefaultFilePermissions = 0644
	DirectoryPermissions   = 0755
	OperationCancelledMsg  = "operation cancelled: %w"
)

// Ensure ImageConverter implements the Converter interface
var _ interfaces.Converter = (*ImageConverter)(nil)

type ImageConverter struct {
	formats map[string]bimg.ImageType
}

func NewImageConverter() *ImageConverter {
	return &ImageConverter{
		formats: map[string]bimg.ImageType{
			"webp": bimg.WEBP,
			"jpeg": bimg.JPEG,
			"png":  bimg.PNG,
			"gif":  bimg.GIF,
		},
	}
}

func (c *ImageConverter) Convert(input []byte, opts map[string]any) ([]byte, error) {
	img := bimg.NewImage(input)

	format, ok := opts["format"].(string)
	if !ok {
		format = "webp"
	}

	imageType, exists := c.formats[format]
	if !exists {
		return nil, fmt.Errorf("unsupported image format: %s", format)
	}

	return img.Convert(imageType)
}

func (c *ImageConverter) SupportedFormats() []string {
	var formats []string
	for format := range c.formats {
		formats = append(formats, format)
	}
	return formats
}

func (c *ImageConverter) ConvertSingle(ctx context.Context, inputPath, outputPath, format string) error {
	// Check context before starting
	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	// Validate input file extension
	if err := c.validateInputFile(inputPath); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	// Validate output format
	if err := c.validateOutputFormat(format); err != nil {
		return fmt.Errorf("format validation failed: %w", err)
	}

	input, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	// Check context before conversion
	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	opts := map[string]any{"format": format}
	output, err := c.Convert(input, opts)
	if err != nil {
		return fmt.Errorf("error converting file: %w", err)
	}

	// Check context before writing
	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	err = os.WriteFile(outputPath, output, DefaultFilePermissions)
	if err != nil {
		return fmt.Errorf("error writing output file: %w", err)
	}

	return nil
}

// ConvertBatch converts multiple files in batch and returns results with error
func (c *ImageConverter) ConvertBatch(ctx context.Context, inputPaths []string, outputDir, format string, keepStructure bool, workers int) ([]models.ConversionResult, error) {
	if len(inputPaths) == 0 {
		return nil, fmt.Errorf("no input paths provided")
	}

	if outputDir == "" {
		return nil, fmt.Errorf("output directory cannot be empty")
	}

	// Check context before starting
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, DirectoryPermissions); err != nil {
		return nil, fmt.Errorf("error creating output directory: %w", err)
	}

	results := make([]models.ConversionResult, len(inputPaths))
	resultsMutex := sync.Mutex{}

	// Use the provided context directly
	workerPool := utils.NewWorkerPool(ctx, workers)
	defer func() {
		// Ensure proper cleanup regardless of how we exit
		workerPool.Cancel()
		workerPool.Close()
	}()

	// Define the processing function
	processFunc := func(job utils.Job) error {
		inputPath := job.InputFile
		outputPath := job.OutputFile

		// Ensure output directory exists
		if err := os.MkdirAll(filepath.Dir(outputPath), DirectoryPermissions); err != nil {
			return fmt.Errorf("error creating output directory: %w", err)
		}

		// Convert the file with context
		return c.ConvertSingle(ctx, inputPath, outputPath, format)
	}

	// Start workers
	workerPool.Start(processFunc)

	// Create job index map
	jobIndexMap := make(map[string]int)

	// Initialize results
	for i, inputPath := range inputPaths {
		outputPath := c.generateOutputPath(inputPath, outputDir, format, keepStructure)
		results[i] = models.ConversionResult{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Success:    false,
		}
		jobIndexMap[outputPath] = i
	}

	// Use channels to coordinate goroutines properly
	submitDone := make(chan bool, 1)
	resultsDone := make(chan bool, 1)

	// Start result collection in a separate goroutine
	go func() {
		defer func() {
			close(resultsDone)
		}()
		c.collectResults(ctx, workerPool, jobIndexMap, results, &resultsMutex)
	}()

	// Submit jobs in a separate goroutine
	go func() {
		defer func() {
			workerPool.CloseJobs()
			close(submitDone)
		}()
		c.submitJobs(ctx, inputPaths, outputDir, format, keepStructure, results, workerPool, &resultsMutex)
	}()

	// Wait for submission to complete
	<-submitDone

	// Wait for all results to be collected with timeout
	select {
	case <-resultsDone:
		// Normal completion
	case <-ctx.Done():
		// Context cancelled - this will cause cleanup
	}

	return results, nil
}

func (c *ImageConverter) submitJobs(ctx context.Context, inputPaths []string, outputDir, format string, keepStructure bool, results []models.ConversionResult, workerPool *utils.WorkerPool, resultsMutex *sync.Mutex) {
	totalFiles := len(inputPaths)
	fmt.Printf("Starting to submit %d jobs to worker pool\n", totalFiles)

	for i, inputPath := range inputPaths {
		// Check if context is cancelled before each submission
		select {
		case <-ctx.Done():
			fmt.Printf("Context cancelled, stopping job submission at %d/%d\n", i, totalFiles)
			return
		default:
		}

		outputPath := c.generateOutputPath(inputPath, outputDir, format, keepStructure)

		// Create and submit job
		job := utils.Job{
			InputFile:  inputPath,
			OutputFile: outputPath,
			Options:    map[string]any{"format": format},
		}

		// Submit with blocking behavior
		if workerPool.Submit(job) {
			if (i+1)%10 == 0 || i+1 == totalFiles {
				fmt.Printf("Successfully submitted %d/%d jobs to worker pool\n", i+1, totalFiles)
			}
		} else {
			// This happens if the context is cancelled or pool is closed
			resultsMutex.Lock()
			results[i].Message = "worker pool was cancelled or closed"
			results[i].Success = false
			resultsMutex.Unlock()
			fmt.Printf("Failed to submit job %d/%d - worker pool cancelled\n", i+1, totalFiles)
			break // Exit early if cancelled
		}
	}

	fmt.Printf("Finished submitting all jobs to worker pool\n")
}

func (c *ImageConverter) collectResults(ctx context.Context, workerPool *utils.WorkerPool, jobIndexMap map[string]int, results []models.ConversionResult, resultsMutex *sync.Mutex) {
	expectedResults := len(jobIndexMap)
	processedCount := 0

	// Collect all results
	for {
		select {
		case result, ok := <-workerPool.Results():
			if !ok {
				// Channel closed
				fmt.Printf("Results channel closed, stopping collection\n")
				goto cleanup
			}

			index, exists := jobIndexMap[result.Job.OutputFile]
			if !exists {
				continue
			}

			c.updateResult(index, result.Error, results, resultsMutex)
			processedCount++

			// Log progress
			if processedCount%10 == 0 || processedCount == expectedResults {
				fmt.Printf("Processed result %d/%d for file: %s\n", processedCount, expectedResults, result.Job.InputFile)
			}

			// Break when we've processed all expected results
			if processedCount >= expectedResults {
				goto cleanup
			}

		case <-ctx.Done():
			fmt.Printf("Context cancelled during result collection\n")
			goto cleanup
		}
	}

cleanup:
	fmt.Printf("Finished collecting results. Processed: %d, Expected: %d\n", processedCount, expectedResults)

	// Mark any remaining unprocessed jobs as failed
	resultsMutex.Lock()
	unprocessedCount := 0
	for i := range results {
		if results[i].Message == "" {
			results[i].Message = "job was not processed by worker pool"
			results[i].Success = false
			unprocessedCount++
		}
	}
	if unprocessedCount > 0 {
		fmt.Printf("Marked %d unprocessed jobs as failed\n", unprocessedCount)
	}
	resultsMutex.Unlock()
}

func (c *ImageConverter) updateResult(index int, err error, results []models.ConversionResult, resultsMutex *sync.Mutex) {
	resultsMutex.Lock()
	defer resultsMutex.Unlock()

	if err != nil {
		results[index].Message = fmt.Sprintf("conversion failed: %v", err)
		results[index].Error = err.Error()
		results[index].Success = false
	} else {
		results[index].Message = "conversion successful"
		results[index].Success = true
	}
}

func (c *ImageConverter) generateOutputPath(inputPath, outputDir, format string, keepStructure bool) string {
	if keepStructure {
		return c.generateStructuredOutputPath(inputPath, outputDir, format)
	}
	return c.generateFlatOutputPath(inputPath, outputDir, format)
}

func (c *ImageConverter) generateFlatOutputPath(inputPath, outputDir, format string) string {
	baseName := filepath.Base(inputPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	return filepath.Join(outputDir, fmt.Sprintf("%s.%s", nameWithoutExt, format))
}

func (c *ImageConverter) generateStructuredOutputPath(inputPath, outputDir, format string) string {
	// For now, implement flat structure. Can be enhanced later for complex directory structures
	return c.generateFlatOutputPath(inputPath, outputDir, format)
}

// validateInputFile checks if the input file has a supported image extension
func (c *ImageConverter) validateInputFile(inputPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input path cannot be empty")
	}

	ext := strings.ToLower(filepath.Ext(inputPath))
	if ext == "" {
		return fmt.Errorf("input file has no extension")
	}

	// Remove the dot from extension
	ext = ext[1:]

	// Define supported input extensions
	// TODO: Make this configurable or extendable
	supportedInputExts := map[string]bool{
		"jpg":  true,
		"jpeg": true,
		"png":  true,
		"gif":  true,
		"webp": true,
		"bmp":  true,
		"tiff": true,
		"tif":  true,
	}

	if !supportedInputExts[ext] {
		var supported []string
		for supportedExt := range supportedInputExts {
			supported = append(supported, supportedExt)
		}
		return fmt.Errorf("unsupported input file extension '%s'. Supported extensions: %v", ext, supported)
	}

	return nil
}

// validateOutputFormat checks if the output format is supported
func (c *ImageConverter) validateOutputFormat(format string) error {
	if format == "" {
		return fmt.Errorf("output format cannot be empty")
	}

	format = strings.ToLower(format)

	if _, exists := c.formats[format]; !exists {
		return fmt.Errorf("unsupported output format '%s'. Supported formats: %v", format, c.SupportedFormats())
	}

	return nil
}
