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

	// Submit jobs and initialize results
	jobIndexMap := c.submitJobs(inputPaths, outputDir, format, keepStructure, results, workerPool, &resultsMutex)

	// Close the jobs channel after all jobs are submitted
	workerPool.CloseJobs()

	// Collect results - use the actual number of submitted jobs
	c.collectResults(workerPool, jobIndexMap, results, &resultsMutex)

	// Make sure to close the worker pool properly
	workerPool.Close()

	return results
}

func (c *ImageConverter) submitJobs(inputPaths []string, outputDir, format string, keepStructure bool, results []models.FileConversionResult, workerPool *utils.WorkerPool, resultsMutex *sync.Mutex) map[string]int {
	jobIndexMap := make(map[string]int)

	for i, inputPath := range inputPaths {
		outputPath := c.generateOutputPath(inputPath, outputDir, format, keepStructure)

		// Initialize result
		results[i] = models.FileConversionResult{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Success:    false,
		}

		// Create and submit job
		job := utils.Job{
			InputFile:  inputPath,
			OutputFile: outputPath,
			Options:    map[string]any{"format": format},
		}

		if workerPool.Submit(job) {
			jobIndexMap[outputPath] = i // Only add to map if successfully submitted
		} else {
			resultsMutex.Lock()
			results[i].Message = "failed to submit job to worker pool"
			resultsMutex.Unlock()
		}
	}

	return jobIndexMap
}

func (c *ImageConverter) generateOutputPath(inputPath, outputDir, format string, keepStructure bool) string {
	if keepStructure {
		return c.generateStructuredOutputPath(inputPath, outputDir, format)
	}
	return c.generateFlatOutputPath(inputPath, outputDir, format)
}

func (c *ImageConverter) collectResults(workerPool *utils.WorkerPool, jobIndexMap map[string]int, results []models.FileConversionResult, resultsMutex *sync.Mutex) {
	expectedResults := len(jobIndexMap)
	processedCount := 0

	for result := range workerPool.Results() {
		index, exists := jobIndexMap[result.Job.OutputFile]
		if !exists {
			continue // Don't increment processedCount for invalid results
		}

		c.updateResult(index, result.Error, results, resultsMutex)
		processedCount++

		if processedCount >= expectedResults {
			break
		}
	}
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
