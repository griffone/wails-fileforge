package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestNewWithWriterFiltersLevels(t *testing.T) {
	var buffer bytes.Buffer
	logger := NewWithWriter(&buffer, slog.LevelInfo)

	logger.Debug("hidden message")
	logger.Info("visible message")
	logger.Warn("warning message")
	logger.Error("error message")

	output := buffer.String()
	if strings.Contains(output, "hidden message") {
		t.Fatalf("debug entry leaked into an info logger: %q", output)
	}
	for _, expected := range []string{"visible message", "warning message", "error message"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
	if !strings.Contains(output, "level=INFO") || !strings.Contains(output, "level=ERROR") {
		t.Fatalf("output is not a slog text handler: %q", output)
	}
}

func TestWithJobAddsFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := WithJob(NewWithWriter(&buffer, slog.LevelDebug), "job-123", "tool.image.convert")

	logger.Info("converting")

	output := buffer.String()
	for _, expected := range []string{"job_id=job-123", "tool_id=tool.image.convert", "converting"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestWithRecipeAddsFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := WithRecipe(NewWithWriter(&buffer, slog.LevelDebug), "recipe-email", "run-9")

	logger.Info("step started")

	output := buffer.String()
	for _, expected := range []string{"recipe_id=recipe-email", "run_id=run-9", "step started"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestWithJobOmitsEmptyFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := WithJob(NewWithWriter(&buffer, slog.LevelDebug), "job-1", "  ")

	logger.Info("message")

	output := buffer.String()
	if !strings.Contains(output, "job_id=job-1") {
		t.Fatalf("output %q does not contain the job id", output)
	}
	if strings.Contains(output, "tool_id") {
		t.Fatalf("output %q contains an empty tool id", output)
	}
}

func TestSetDefault(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var buffer bytes.Buffer
	logger := NewWithWriter(&buffer, slog.LevelInfo)
	SetDefault(logger)
	slog.Info("global message")

	if !strings.Contains(buffer.String(), "global message") {
		t.Fatalf("default logger output = %q, want the global message", buffer.String())
	}

	SetDefault(nil)
	if slog.Default() != logger {
		t.Fatal("SetDefault(nil) replaced the default logger")
	}
}

func TestLevelFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		fallback slog.Level
		want     slog.Level
	}{
		{name: "unset uses fallback", env: "", fallback: slog.LevelWarn, want: slog.LevelWarn},
		{name: "debug", env: "debug", fallback: slog.LevelInfo, want: slog.LevelDebug},
		{name: "info", env: "INFO", fallback: slog.LevelError, want: slog.LevelInfo},
		{name: "warn with spaces", env: " warn ", fallback: slog.LevelInfo, want: slog.LevelWarn},
		{name: "error", env: "error", fallback: slog.LevelInfo, want: slog.LevelError},
		{name: "unknown uses fallback", env: "trace", fallback: slog.LevelWarn, want: slog.LevelWarn},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(EnvLevel, test.env)
			if got := levelFromEnv(test.fallback); got != test.want {
				t.Fatalf("levelFromEnv(%s) = %s, want %s", test.fallback, got, test.want)
			}
		})
	}
}
