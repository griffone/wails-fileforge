package interfaces

// Converter defines the interface for file conversion operations.
type Converter interface {
	Convert(input []byte, opts map[string]any) ([]byte, error)
	SupportedFormats() []string
}

// BatchConverter extends Converter with batch processing capabilities.
type BatchConverter interface {
	Converter
	ConvertBatch(files []string, opts map[string]any, workerCount int) error
}
