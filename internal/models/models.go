package models

type ConversionRequest struct {
	InputPath  string         `json:"inputPath"`
	OutputPath string         `json:"outputPath"`
	Format     string         `json:"format"`
	Options    map[string]any `json:"options"`
	Category   string         `json:"category"`
}

type ConversionResult struct {
	InputPath  string `json:"inputPath"`
	OutputPath string `json:"outputPath"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Error      string `json:"error"`
}

type SupportedFormat struct {
	Category string   `json:"category"`
	Formats  []string `json:"formats"`
}

// New models for batch conversion
type BatchConversionRequest struct {
	InputPaths    []string       `json:"inputPaths"`
	OutputDir     string         `json:"outputDir"`
	Format        string         `json:"format"`
	Workers       int            `json:"workers"` // Number of concurrent workers
	Options       map[string]any `json:"options"`
	Category      string         `json:"category"`
	KeepStructure bool           `json:"keepStructure"` // Whether to maintain directory structure
}

type BatchConversionResult struct {
	Success      bool               `json:"success"`
	Message      string             `json:"message"`
	TotalFiles   int                `json:"totalFiles"`
	SuccessCount int                `json:"successCount"`
	FailureCount int                `json:"failureCount"`
	Results      []ConversionResult `json:"results"`
	Error        string             `json:"error"`
}
