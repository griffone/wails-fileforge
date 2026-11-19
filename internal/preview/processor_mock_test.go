package preview

import (
	"context"
)

// MockProcessor returns predictable bytes for testing without libvips.
// It lives in a test file so it never compiles into the production binary.
type MockProcessor struct {
	Data        []byte
	ContentType string
	Err         error
	// ProcessFunc overrides the static result when set.
	ProcessFunc func(ctx context.Context, req PreviewRequest) ([]byte, string, error)
	// Started receives one signal per Process call when non-nil.
	Started chan struct{}
}

func (m *MockProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	if m.Started != nil {
		select {
		case m.Started <- struct{}{}:
		default:
		}
	}
	if m.ProcessFunc != nil {
		return m.ProcessFunc(ctx, req)
	}
	return m.Data, m.ContentType, m.Err
}

func NewMockProcessor(data []byte, contentType string) JobProcessor {
	return &MockProcessor{Data: data, ContentType: contentType}
}
