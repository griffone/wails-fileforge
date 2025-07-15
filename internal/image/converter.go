package image

import (
	"fileforge-desktop/internal/interfaces"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/utils"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/h2non/bimg"
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

func (c *ImageConverter) ConvertSingle(inputPath, outputPath, format string) error {
	input, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	opts := map[string]any{"format": format}
	output, err := c.Convert(input, opts)
	if err != nil {
		return fmt.Errorf("error converting file: %v", err)
	}

	return os.WriteFile(outputPath, output, 0644)
}

// ConvertBatch converts multiple files in batch
func (c *ImageConverter) ConvertBatch(inputPaths []string, outputDir, format string, keepStructure bool) []models.FileConversionResult {
	results := make([]models.FileConversionResult, len(inputPaths))
	resultsMutex := sync.Mutex{}

	// Create worker pool
	workerPool := utils.NewWorkerPool(utils.DefaultWorkerCount)

	// Define the processing function
	processFunc := func(job utils.Job) error {
		inputPath := job.InputFile
		outputPath := job.OutputFile

		// Ensure output directory exists
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			return fmt.Errorf("error creating output directory: %v", err)
		}

		// Convert the file
		return c.ConvertSingle(inputPath, outputPath, format)
	}

	// Start workers
	workerPool.Start(processFunc)

	// Create job index map
	jobIndexMap := make(map[string]int)

	// Initialize results
	for i, inputPath := range inputPaths {
		outputPath := c.generateOutputPath(inputPath, outputDir, format, keepStructure)
		results[i] = models.FileConversionResult{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Success:    false,
		}
		jobIndexMap[outputPath] = i
	}

	// Start result collection in a separate goroutine BEFORE submitting jobs
	resultsDone := make(chan bool)
	go func() {
		defer close(resultsDone)
		c.collectResults(workerPool, jobIndexMap, results, &resultsMutex)
	}()

	// Submit jobs in a separate goroutine to avoid blocking
	go func() {
		defer workerPool.CloseJobs() // Close jobs channel when done submitting
		c.submitJobs(inputPaths, outputDir, format, keepStructure, results, workerPool, &resultsMutex)
	}()

	// Wait for all results to be collected
	<-resultsDone

	// Clean up
	workerPool.Cancel() // Cancel any remaining workers
	workerPool.Close()  // Wait for workers to finish and close results channel

	return results
}

func (c *ImageConverter) submitJobs(inputPaths []string, outputDir, format string, keepStructure bool, results []models.FileConversionResult, workerPool *utils.WorkerPool, resultsMutex *sync.Mutex) {
	totalFiles := len(inputPaths)
	fmt.Printf("Starting to submit %d jobs to worker pool\n", totalFiles)

	for i, inputPath := range inputPaths {
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
			// This should only happen if the context is cancelled
			resultsMutex.Lock()
			results[i].Message = "worker pool was cancelled"
			results[i].Success = false
			resultsMutex.Unlock()
			fmt.Printf("Failed to submit job %d/%d - worker pool cancelled\n", i+1, totalFiles)
			break // Exit early if cancelled
		}
	}

	fmt.Printf("Finished submitting all jobs to worker pool\n")
}

func (c *ImageConverter) collectResults(workerPool *utils.WorkerPool, jobIndexMap map[string]int, results []models.FileConversionResult, resultsMutex *sync.Mutex) {
	expectedResults := len(jobIndexMap)
	processedCount := 0

	// Collect all results
	for result := range workerPool.Results() {
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
			break
		}
	}

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

func (c *ImageConverter) updateResult(index int, err error, results []models.FileConversionResult, resultsMutex *sync.Mutex) {
	resultsMutex.Lock()
	defer resultsMutex.Unlock()

	if err != nil {
		results[index].Message = fmt.Sprintf("conversion failed: %v", err)
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
