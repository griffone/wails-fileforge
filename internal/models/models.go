package models

type ConversionRequest struct {
	InputPath  string         `json:"inputPath"`
	OutputPath string         `json:"outputPath"`
	Format     string         `json:"format"`
	Options    map[string]any `json:"options"`
	Category   string         `json:"category"`
}

type ConversionResult struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	OutputPath string `json:"outputPath"`
}

type SupportedFormat struct {
	Category string   `json:"category"`
	Formats  []string `json:"formats"`
}
