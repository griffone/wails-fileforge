package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"fileforge-desktop/internal/platform/execx"
)

// EncoderLister returns the encoder names an ffmpeg build exposes.
type EncoderLister interface {
	List(ctx context.Context) (map[string]struct{}, error)
}

// EncoderListerFunc adapts a function to EncoderLister for tests.
type EncoderListerFunc func(context.Context) (map[string]struct{}, error)

func (f EncoderListerFunc) List(ctx context.Context) (map[string]struct{}, error) {
	return f(ctx)
}

// ExecEncoderLister asks ffmpeg for its encoder list. It is the only place the
// audio domain runs `ffmpeg -encoders`.
type ExecEncoderLister struct{}

// List runs `ffmpeg -encoders` and parses the audio encoder names.
func (ExecEncoderLister) List(ctx context.Context) (map[string]struct{}, error) {
	var stdout strings.Builder
	result, err := execx.Run(ctx, execx.Command{
		Path: "ffmpeg",
		Args: []string{"-hide_banner", "-encoders"},
		Stdout: func(line string) {
			stdout.WriteString(line)
			stdout.WriteByte('\n')
		},
		StderrTailLines: 40,
		StderrTailBytes: 8 * 1024,
	})
	if err != nil {
		return nil, commandError(result.Stderr, err)
	}

	return ParseEncoderList(stdout.String()), nil
}

// ParseEncoderList extracts audio encoder names from `ffmpeg -encoders` output.
// The first column is a six-character capability mask; audio encoders carry an
// "A" there. Header and separator lines do not have a six-character first field
// and are skipped.
func ParseEncoderList(output string) map[string]struct{} {
	encoders := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		flags := fields[0]
		if len(flags) != 6 || !strings.ContainsRune(flags, 'A') {
			continue
		}
		encoders[fields[1]] = struct{}{}
	}

	return encoders
}

// EncoderProbe caches the ffmpeg encoder list for the process lifetime. A
// successful probe is reused; a failed probe is retried on the next call so a
// transient startup failure does not poison the app until restart.
type EncoderProbe struct {
	mu       sync.Mutex
	loaded   bool
	encoders map[string]struct{}
	lister   EncoderLister
}

// NewEncoderProbe builds a probe. A nil lister uses the real ffmpeg binary.
func NewEncoderProbe(lister EncoderLister) *EncoderProbe {
	return &EncoderProbe{lister: lister}
}

var (
	defaultEncoderProbeOnce sync.Once
	defaultEncoderProbe     *EncoderProbe
)

// DefaultEncoderProbe returns the process-wide probe so `ffmpeg -encoders` runs
// once even when every tool asks for runtime state.
func DefaultEncoderProbe() *EncoderProbe {
	defaultEncoderProbeOnce.Do(func() {
		defaultEncoderProbe = NewEncoderProbe(ExecEncoderLister{})
	})

	return defaultEncoderProbe
}

// Has reports whether one encoder is available in the cached list.
func (p *EncoderProbe) Has(ctx context.Context, encoder string) (bool, error) {
	encoders, err := p.load(ctx)
	if err != nil {
		return false, err
	}
	_, ok := encoders[strings.TrimSpace(encoder)]

	return ok, nil
}

// Missing returns the requested encoders that are absent, preserving order.
func (p *EncoderProbe) Missing(ctx context.Context, encoders []string) ([]string, error) {
	available, err := p.load(ctx)
	if err != nil {
		return nil, err
	}

	missing := make([]string, 0)
	for _, encoder := range encoders {
		if _, ok := available[strings.TrimSpace(encoder)]; !ok {
			missing = append(missing, encoder)
		}
	}

	return missing, nil
}

func (p *EncoderProbe) load(ctx context.Context) (map[string]struct{}, error) {
	if p == nil {
		return nil, &AudioError{Code: ErrorCodeRuntimeUnavailable, Message: "encoder probe is not configured"}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.loaded {
		return p.encoders, nil
	}

	lister := p.lister
	if lister == nil {
		lister = ExecEncoderLister{}
	}
	encoders, err := lister.List(ctx)
	if err != nil {
		return nil, err
	}

	p.encoders = encoders
	p.loaded = true

	return encoders, nil
}

func commandError(stderr string, err error) error {
	cause := err
	var execErr *execx.Error
	if errors.As(err, &execErr) && execErr.Err != nil {
		cause = execErr.Err
	}

	return &AudioError{
		Code:    ErrorCodeRuntimeUnavailable,
		Message: fmt.Sprintf("ffmpeg encoder list could not be read (%s)", strings.TrimSpace(stderr)),
		Cause:   cause,
	}
}
