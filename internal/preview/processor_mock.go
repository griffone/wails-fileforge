package preview

import (
	"context"
)

// MockProcessor returns predictable bytes for testing without libvips.
type MockProcessor struct {
	Data        []byte
	ContentType string
	Err         error
}

func (m *MockProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	return m.Data, m.ContentType, m.Err
}

func NewMockProcessor(data []byte, contentType string) JobProcessor {
	return &MockProcessor{Data: data, ContentType: contentType}
}
