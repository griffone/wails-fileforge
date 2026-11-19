// Package logx configures the application's structured logger. It standardizes
// the text handler, the environment-driven level, and the correlation fields
// attached to job and recipe logs.
package logx

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// EnvLevel is the environment variable that overrides the log level.
const EnvLevel = "FILEFORGE_LOG_LEVEL"

// New returns a text logger writing to stderr. FILEFORGE_LOG_LEVEL overrides
// level when it names debug, info, warn, or error.
func New(level slog.Level) *slog.Logger {
	return NewWithWriter(os.Stderr, levelFromEnv(level))
}

// NewWithWriter returns a text logger writing to w at the given level. The
// explicit writer and level make this constructor deterministic for tests and
// for callers that manage their own output sink.
func NewWithWriter(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = io.Discard
	}

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// SetDefault installs logger as the process-wide slog default. A nil logger is
// ignored.
func SetDefault(logger *slog.Logger) {
	if logger == nil {
		return
	}
	slog.SetDefault(logger)
}

// WithJob returns a logger with job and tool correlation fields. Empty values
// are omitted.
func WithJob(logger *slog.Logger, jobID, toolID string) *slog.Logger {
	return withFields(logger, "job_id", jobID, "tool_id", toolID)
}

// WithRecipe returns a logger with recipe and run correlation fields. Empty
// values are omitted.
func WithRecipe(logger *slog.Logger, recipeID, runID string) *slog.Logger {
	return withFields(logger, "recipe_id", recipeID, "run_id", runID)
}

func withFields(logger *slog.Logger, keyValues ...string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}

	args := make([]any, 0, len(keyValues))
	for index := 0; index+1 < len(keyValues); index += 2 {
		value := strings.TrimSpace(keyValues[index+1])
		if value == "" {
			continue
		}
		args = append(args, keyValues[index], value)
	}
	if len(args) == 0 {
		return logger
	}

	return logger.With(args...)
}

func levelFromEnv(fallback slog.Level) slog.Level {
	raw, ok := os.LookupEnv(EnvLevel)
	if !ok {
		return fallback
	}

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return fallback
	}
}
