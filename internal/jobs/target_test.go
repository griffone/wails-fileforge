package jobs

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestParseTargetBytesAcceptsNumericShapes(t *testing.T) {
	tooLarge := math.Pow(2, 70)

	cases := []struct {
		name  string
		value any
		want  int64
		ok    bool
	}{
		{name: "float64", value: float64(10 * 1024 * 1024), want: 10 * 1024 * 1024, ok: true},
		{name: "fractional float64 truncates", value: float64(2048.9), want: 2048, ok: true},
		{name: "float32", value: float32(2048), want: 2048, ok: true},
		{name: "int", value: 2048, want: 2048, ok: true},
		{name: "int8", value: int8(12), want: 12, ok: true},
		{name: "int16", value: int16(2048), want: 2048, ok: true},
		{name: "int32", value: int32(2048), want: 2048, ok: true},
		{name: "int64", value: int64(2048), want: 2048, ok: true},
		{name: "uint", value: uint(2048), want: 2048, ok: true},
		{name: "uint8", value: uint8(12), want: 12, ok: true},
		{name: "uint16", value: uint16(2048), want: 2048, ok: true},
		{name: "uint32", value: uint32(2048), want: 2048, ok: true},
		{name: "uint64", value: uint64(2048), want: 2048, ok: true},
		{name: "json number integer", value: json.Number("2048"), want: 2048, ok: true},
		{name: "json number float", value: json.Number("2048.9"), want: 2048, ok: true},
		{name: "numeric string integer", value: "2048", want: 2048, ok: true},
		{name: "numeric string float", value: "2048.9", want: 2048, ok: true},
		{name: "nil", value: nil},
		{name: "bool", value: true},
		{name: "empty string", value: "  "},
		{name: "non-numeric string", value: "ten"},
		{name: "zero", value: float64(0)},
		{name: "negative", value: -1},
		{name: "zero string", value: "0"},
		{name: "negative string", value: "-2048"},
		{name: "NaN", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "too large for int64", value: tooLarge},
		{name: "uint64 overflow", value: uint64(math.MaxInt64) + 1},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := parseTargetBytes(testCase.value)
			if ok != testCase.ok {
				t.Fatalf("parseTargetBytes(%v) ok = %v, want %v", testCase.value, ok, testCase.ok)
			}
			if got != testCase.want {
				t.Fatalf("parseTargetBytes(%v) = %d, want %d", testCase.value, got, testCase.want)
			}
		})
	}
}

func TestEngineRequestedTargetBytesReadsTheSubmittedRequest(t *testing.T) {
	tool := &engineFakeTool{
		id: "tool.fake.target",
		single: func(_ context.Context, req models.JobRequest, _ func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
			return models.JobResultItem{
				InputPath:  firstEngineInput(req),
				OutputPath: "out.bin",
				Outputs:    []string{"out.bin"},
				Success:    true,
			}, nil
		},
	}
	engine := newTestEngine(t, newEngineResolver(t, tool), Options{})

	withTarget := submitTestJob(t, engine, models.JobRequest{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"input.bin"},
		Options:    map[string]any{"targetSizeBytes": float64(4096)},
	})
	if targetBytes, ok := engine.RequestedTargetBytes(withTarget.JobID); !ok || targetBytes != 4096 {
		t.Fatalf("RequestedTargetBytes = (%d, %v), want (4096, true)", targetBytes, ok)
	}

	withoutTarget := submitTestJob(t, engine, models.JobRequest{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"input.bin"},
	})
	if targetBytes, ok := engine.RequestedTargetBytes(withoutTarget.JobID); ok || targetBytes != 0 {
		t.Fatalf("RequestedTargetBytes without option = (%d, %v), want (0, false)", targetBytes, ok)
	}

	if targetBytes, ok := engine.RequestedTargetBytes("missing"); ok || targetBytes != 0 {
		t.Fatalf("RequestedTargetBytes on unknown job = (%d, %v), want (0, false)", targetBytes, ok)
	}
}
