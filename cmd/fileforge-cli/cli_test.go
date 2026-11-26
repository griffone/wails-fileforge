package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

// isolateEnvironment points the user config and data directories at a temp
// directory and restores PATH, which the managed toolchain prepends to.
func isolateEnvironment(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
	})
}

func TestParseRunFlagsRepeatableInputsOptionsAndJSON(t *testing.T) {
	flags, err := parseRunFlags("run tool", []string{
		"--input", "first.pdf",
		"--input", "second.pdf",
		"--out", "/tmp/exports",
		"--option", "quality=80",
		"--option", "compress=true",
		"--option", "strategy=ranges",
		"--json",
	})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	if !reflect.DeepEqual(flags.inputs, []string{"first.pdf", "second.pdf"}) {
		t.Fatalf("unexpected inputs: %#v", flags.inputs)
	}
	if flags.outDir != "/tmp/exports" {
		t.Fatalf("unexpected out dir: %q", flags.outDir)
	}
	if !flags.asJSON {
		t.Fatal("expected --json to be set")
	}
	if got := flags.options["quality"]; got != float64(80) {
		t.Fatalf("quality = %#v (%T)", got, got)
	}
	if got := flags.options["compress"]; got != true {
		t.Fatalf("compress = %#v (%T)", got, got)
	}
	if got := flags.options["strategy"]; got != "ranges" {
		t.Fatalf("strategy = %#v (%T)", got, got)
	}
}

func TestParseRunFlagsRejectsUnknownFlag(t *testing.T) {
	_, err := parseRunFlags("run tool", []string{"--input", "a.pdf", "--bogus"})
	if err == nil {
		t.Fatal("expected unknown flag to fail")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("expected the error to name the flag, got %v", err)
	}
}

func TestParseRunFlagsRequiresInput(t *testing.T) {
	_, err := parseRunFlags("run tool", []string{"--out", "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "--input") {
		t.Fatalf("expected a missing input error, got %v", err)
	}
}

func TestParseOptionsRejectsMalformedPair(t *testing.T) {
	_, err := parseOptions([]string{"quality"})
	if err == nil {
		t.Fatal("expected a key=value error")
	}
}

func TestParseOptionsDecodesJSONValues(t *testing.T) {
	options, err := parseOptions([]string{
		`inputPageRanges=["1-2","3"]`,
		`margins={"top":10,"right":5}`,
		`quality=80`,
		`pageSelection=1`,
		`name=A4`,
	})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}

	if got, ok := options["inputPageRanges"].([]any); !ok || len(got) != 2 {
		t.Fatalf("inputPageRanges = %#v", options["inputPageRanges"])
	}
	if _, ok := options["margins"].(map[string]any); !ok {
		t.Fatalf("margins = %#v", options["margins"])
	}
	if options["quality"] != float64(80) {
		t.Fatalf("quality = %#v (%T)", options["quality"], options["quality"])
	}
	// A string option keeps its text even when it looks like a number.
	if options["pageSelection"] != "1" {
		t.Fatalf("pageSelection = %#v (%T)", options["pageSelection"], options["pageSelection"])
	}
	if options["name"] != "A4" {
		t.Fatalf("name = %#v", options["name"])
	}
}

func TestExitCodeForStatusContract(t *testing.T) {
	cases := []struct {
		status string
		want   int
	}{
		{models.JobStatusSuccess, exitSuccess},
		{models.JobStatusPartialSuccess, exitJobFailed},
		{models.JobStatusFailed, exitJobFailed},
		{models.JobStatusCancelled, exitJobFailed},
		{models.JobStatusInterrupted, exitJobFailed},
		{models.JobStatusRunning, exitInternal},
	}

	for _, testCase := range cases {
		if got := exitCodeForStatus(testCase.status); got != testCase.want {
			t.Errorf("exitCodeForStatus(%q) = %d, want %d", testCase.status, got, testCase.want)
		}
	}
}

func TestUnknownToolOption(t *testing.T) {
	if unknown, keys, found := unknownToolOption("tool.pdf.merge", map[string]any{"inputPageRanges": []string{"1"}}); !found || unknown != "" || len(keys) == 0 {
		t.Fatalf("expected inputPageRanges to be allowed, got unknown=%q found=%v keys=%v", unknown, found, keys)
	}

	unknown, _, found := unknownToolOption("tool.pdf.merge", map[string]any{"nope": true})
	if !found || unknown != "nope" {
		t.Fatalf("expected nope to be unknown, got %q (found=%v)", unknown, found)
	}

	if _, _, found := unknownToolOption("tool.unknown", map[string]any{"nope": true}); found {
		t.Fatal("expected an uncatalogued tool to skip the CLI allowlist")
	}
}

func TestVideoTargetSizeOptionIsAllowed(t *testing.T) {
	for _, toolID := range []string{"tool.video.convert", "tool.video.trim", "tool.video.merge"} {
		if unknown, keys, found := unknownToolOption(toolID, map[string]any{"targetSizeBytes": int64(1024)}); !found || unknown != "" {
			t.Fatalf("%s: expected targetSizeBytes to be allowed, got unknown=%q found=%v keys=%v", toolID, unknown, found, keys)
		}
	}
}

func TestJSONReportShape(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "merged.pdf")
	if err := os.WriteFile(output, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := emitRun(&stdout, &stderr, true, runReport{
		JobID:   "job-1",
		Status:  models.JobStatusSuccess,
		Success: true,
		Outputs: outputEntries([]string{output}),
	})
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr in JSON mode, got %q", stderr.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if decoded["jobId"] != "job-1" || decoded["status"] != models.JobStatusSuccess || decoded["success"] != true {
		t.Fatalf("unexpected report: %#v", decoded)
	}
	outputs, ok := decoded["outputs"].([]any)
	if !ok || len(outputs) != 1 {
		t.Fatalf("unexpected outputs: %#v", decoded["outputs"])
	}
	entry := outputs[0].(map[string]any)
	if entry["path"] != output || entry["sizeBytes"] != float64(5) {
		t.Fatalf("unexpected output entry: %#v", entry)
	}

	stdout.Reset()
	stderr.Reset()
	code = emitRun(&stdout, &stderr, true, runReport{
		JobID:   "job-2",
		Status:  models.JobStatusFailed,
		Outputs: []outputEntry{},
		Error:   models.NewCanonicalJobError("PDF_MERGE_FAILED", "merge failed", nil),
	})
	if code != exitJobFailed {
		t.Fatalf("expected exit 3, got %d", code)
	}
	decoded = nil
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode failure json: %v", err)
	}
	jobErr, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected a canonical error, got %#v", decoded["error"])
	}
	if jobErr["detail_code"] != "PDF_MERGE_FAILED" || jobErr["message"] != "merge failed" {
		t.Fatalf("unexpected error: %#v", jobErr)
	}
}

func TestEmitRunPartialExitsJobFailed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := emitRun(&stdout, &stderr, true, runReport{
		JobID:   "job-partial",
		Status:  models.JobStatusPartialSuccess,
		Success: false,
		Outputs: []outputEntry{},
		Error:   models.NewCanonicalJobError("EXECUTION_FAILED", "one item failed", nil),
	})
	if code != exitJobFailed {
		t.Fatalf("expected exit 3, got %d", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr in JSON mode, got %q", stderr.String())
	}
}

func TestRecipesListJSONIsOneObject(t *testing.T) {
	isolateEnvironment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"recipes", "list", "--json"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("expected one JSON object, got %q: %v", stdout.String(), err)
	}
	if _, ok := decoded["recipes"].([]any); !ok {
		t.Fatalf("expected a recipes array, got %#v", decoded["recipes"])
	}
}

func TestRunUnknownToolExitsValidation(t *testing.T) {
	isolateEnvironment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"run", "tool", "tool.does.not.exist", "--input", "whatever.pdf", "--json",
	}, &stdout, &stderr)
	if code != exitValidation {
		t.Fatalf("expected exit 2, got %d (stderr=%s)", code, stderr.String())
	}

	var decoded struct {
		Success bool `json:"success"`
		Error   *struct {
			DetailCode string `json:"detail_code"`
			Message    string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if decoded.Success || decoded.Error == nil || decoded.Error.DetailCode != "TOOL_NOT_FOUND" {
		t.Fatalf("unexpected report: %#v", decoded)
	}
}

func TestRunUnknownToolOptionExitsValidation(t *testing.T) {
	isolateEnvironment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"run", "tool", "tool.pdf.merge", "--input", "whatever.pdf", "--option", "nope=1", "--json",
	}, &stdout, &stderr)
	if code != exitValidation {
		t.Fatalf("expected exit 2, got %d (stderr=%s)", code, stderr.String())
	}

	var decoded struct {
		Error *struct {
			DetailCode string `json:"detail_code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if decoded.Error == nil || decoded.Error.DetailCode != "TOOL_OPTION_UNKNOWN" {
		t.Fatalf("unexpected error: %#v", decoded.Error)
	}
}

func TestToolsListJSONIsOneObject(t *testing.T) {
	isolateEnvironment(t)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"tools", "list", "--json"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("expected one JSON object, got %q: %v", stdout.String(), err)
	}
	if _, ok := decoded["tools"].([]any); !ok {
		t.Fatalf("expected a tools array, got %#v", decoded["tools"])
	}
}

func TestRunWithoutArgumentsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, &stdout, &stderr)
	if code != exitValidation {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Exit codes:") {
		t.Fatalf("expected the usage text with exit codes, got %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
}

func TestHelpPrintsUsageAndExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"help"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "run tool") {
		t.Fatalf("expected the usage text, got %q", stdout.String())
	}
}

func TestFlagErrHelpIsRecognized(t *testing.T) {
	_, err := parseRunFlags("run tool", []string{"-h"})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("expected flag.ErrHelp, got %v", err)
	}
}
