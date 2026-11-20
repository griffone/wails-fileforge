package engine

import (
	"archive/zip"
	"bufio"
	"context"
	_ "embed"
	"encoding/xml"
	"errors"
	"fileforge-desktop/internal/platform/execx"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"fileforge-desktop/internal/platform/fsx"
)

// PDF to DOCX conversion runs pdf2docx inside the managed virtual environment.
// The embedded bridge streams pdf2docx log lines, the engine maps them to
// progress and publishes the generated DOCX atomically.
const (
	PDFToDOCXStageParse = "docx.parse"
	PDFToDOCXStageWrite = "docx.write"

	PDFToDOCXErrorRuntimeUnavailable = "PDF_DOCX_RUNTIME_UNAVAILABLE"
	PDFToDOCXErrorInvalidInput       = "PDF_DOCX_INVALID_INPUT"
	PDFToDOCXErrorConversionFailed   = "PDF_DOCX_CONVERSION_FAILED"
	PDFToDOCXErrorOutputFailed       = "PDF_DOCX_OUTPUT_FAILED"
	PDFToDOCXErrorCancelled          = "PDF_DOCX_CANCELLED"
)

const (
	pdfToDOCXTempDirPattern    = "fileforge-pdf2docx-*"
	pdfToDOCXTempFilePattern   = ".fileforge-docx-*.docx"
	pdfToDOCXWriteCheckPattern = ".fileforge-docx-writecheck-*.tmp"
	pdfToDOCXBridgeFileName    = "pdf2docx_bridge.py"
	pdfToDOCXRootElement       = "word/document.xml"

	// pdfToDOCXStderrMaxLines and pdfToDOCXStderrMaxBytes bound the stderr tail
	// copied into an error, so a chatty interpreter never floods the response.
	pdfToDOCXStderrMaxLines  = 60
	pdfToDOCXStderrMaxBytes  = 8 * 1024
	pdfToDOCXScannerMaxBytes = 1024 * 1024
)

// pdfToDOCXBridgeSource is the Python bridge written to the conversion
// workspace on every run.
//
//go:embed pdf2docx_bridge.py
var pdfToDOCXBridgeSource []byte

// pdfToDOCXPageLinePattern matches the pdf2docx page progress line,
// "(n/total) Page n", after the ANSI sequences were stripped.
var pdfToDOCXPageLinePattern = regexp.MustCompile(`\((\d+)/(\d+)\)\s*Page\b`)

// pdfToDOCXANSIPattern matches the color escape sequences pdf2docx writes
// around its stage banners.
var pdfToDOCXANSIPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// PDFToDOCXRequest describes one PDF to DOCX conversion.
type PDFToDOCXRequest struct {
	InputPath  string
	OutputPath string
	PythonPath string
}

// PDFToDOCXProgressEvent is one conversion update. Percent is 0-100.
type PDFToDOCXProgressEvent struct {
	Stage   string
	Percent int
	Message string
}

// PDFToDOCXStats reports what the generated DOCX contains. It is a structural
// summary, not a fidelity score.
type PDFToDOCXStats struct {
	Paragraphs     int
	Runs           int
	BoldRuns       int
	Images         int
	Tables         int
	TextCharacters int
}

// PDFToDOCXResult reports the published DOCX and its structural stats.
type PDFToDOCXResult struct {
	OutputPath string
	Stats      PDFToDOCXStats
}

// PDFToDOCXError is a typed conversion failure with a stable code the tool maps
// to a canonical job error.
type PDFToDOCXError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *PDFToDOCXError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *PDFToDOCXError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// DOCXCommandRunner runs the PDF to DOCX bridge and streams its stdout lines.
// The returned string is the bounded stderr tail, which explains a failure.
type DOCXCommandRunner interface {
	RunDOCX(ctx context.Context, dir, name string, args []string, onLine func(string)) (string, error)
}

// ExecDOCXCommandRunner runs real interpreters with exec.CommandContext, so
// context cancellation kills the converter.
type ExecDOCXCommandRunner struct{}

func (r *ExecDOCXCommandRunner) RunDOCX(ctx context.Context, dir, name string, args []string, onLine func(string)) (string, error) {
	command := execx.CommandContext(ctx, name, args...)
	command.Dir = dir

	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	command.Stdout = stdoutWriter
	command.Stderr = stderrWriter

	if startErr := command.Start(); startErr != nil {
		_ = stdoutWriter.Close()
		_ = stderrWriter.Close()
		_ = stdoutReader.Close()
		_ = stderrReader.Close()

		return "", startErr
	}

	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		scanner := bufio.NewScanner(stdoutReader)
		scanner.Buffer(make([]byte, 0, 64*1024), pdfToDOCXScannerMaxBytes)
		for scanner.Scan() {
			if onLine != nil {
				onLine(scanner.Text())
			}
		}
		_ = stdoutReader.Close()
	}()

	tail := &pdfToDOCXStderrTail{maxLines: pdfToDOCXStderrMaxLines, maxBytes: pdfToDOCXStderrMaxBytes}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderrReader)
		scanner.Buffer(make([]byte, 0, 64*1024), pdfToDOCXScannerMaxBytes)
		for scanner.Scan() {
			tail.add(scanner.Text())
		}
		_ = stderrReader.Close()
	}()

	waitErr := command.Wait()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	<-stdoutDone
	<-stderrDone

	if waitErr != nil {
		return tail.String(), fmt.Errorf("%s failed: %w", filepath.Base(name), waitErr)
	}

	return tail.String(), nil
}

// pdfToDOCXStderrTail keeps the last lines of stderr and caps the rendered
// text, so a long traceback is reported by its tail.
type pdfToDOCXStderrTail struct {
	lines    []string
	maxLines int
	maxBytes int
}

func (t *pdfToDOCXStderrTail) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.maxLines {
		t.lines = t.lines[len(t.lines)-t.maxLines:]
	}
}

func (t *pdfToDOCXStderrTail) String() string {
	text := strings.TrimSpace(strings.Join(t.lines, "\n"))
	if len(text) <= t.maxBytes {
		return text
	}

	start := len(text) - t.maxBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}

	return strings.TrimSpace(text[start:])
}

// ValidatePDFToDOCXRequest checks the input, the destination and the managed
// interpreter without running the converter.
func ValidatePDFToDOCXRequest(req PDFToDOCXRequest) *PDFToDOCXError {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return &PDFToDOCXError{Code: PDFToDOCXErrorInvalidInput, Message: "inputPath is required"}
	}
	if !isPDFPath(inputPath) {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorInvalidInput,
			Message: fmt.Sprintf("input file must be .pdf: %s", inputPath),
			Details: map[string]any{"inputPath": inputPath},
		}
	}

	inputInfo, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &PDFToDOCXError{
				Code:    PDFToDOCXErrorInvalidInput,
				Message: fmt.Sprintf("input PDF was not found: %s", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath},
				Cause:   err,
			}
		}

		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorInvalidInput,
			Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)),
			Details: map[string]any{"inputPath": inputPath},
			Cause:   err,
		}
	}
	if inputInfo.IsDir() {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorInvalidInput,
			Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath)),
			Details: map[string]any{"inputPath": inputPath},
		}
	}
	if !inputInfo.Mode().IsRegular() {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorInvalidInput,
			Message: fmt.Sprintf("input path is not a regular file: %s", filepath.Base(inputPath)),
			Details: map[string]any{"inputPath": inputPath},
		}
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &PDFToDOCXError{Code: PDFToDOCXErrorInvalidInput, Message: "outputPath is required"}
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".docx") {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorInvalidInput,
			Message: "outputPath must use .docx extension",
			Details: map[string]any{"outputPath": outputPath},
		}
	}
	if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPath) {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorInvalidInput,
			Message: fmt.Sprintf("outputPath collides with input path: %s", inputPath),
			Details: map[string]any{"inputPath": inputPath, "outputPath": outputPath},
		}
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output file already exists: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output path is not accessible: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
			Cause:   statErr,
		}
	}

	if dirErr := validatePDFToDOCXOutputDir(outputPath); dirErr != nil {
		return dirErr
	}

	pythonPath := strings.TrimSpace(req.PythonPath)
	if pythonPath == "" {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorRuntimeUnavailable,
			Message: "the PDF to DOCX engine is not installed",
		}
	}
	pythonInfo, pythonErr := os.Stat(pythonPath)
	if pythonErr != nil {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorRuntimeUnavailable,
			Message: fmt.Sprintf("the PDF to DOCX Python interpreter was not found: %s", pythonPath),
			Details: map[string]any{"pythonPath": pythonPath},
			Cause:   pythonErr,
		}
	}
	if pythonInfo.IsDir() || !pythonInfo.Mode().IsRegular() {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorRuntimeUnavailable,
			Message: fmt.Sprintf("the PDF to DOCX Python interpreter is not a file: %s", pythonPath),
			Details: map[string]any{"pythonPath": pythonPath},
		}
	}

	return nil
}

// validatePDFToDOCXOutputDir checks that the destination directory exists, is a
// directory and accepts writes, so a successful run can be promised.
func validatePDFToDOCXOutputDir(outputPath string) *PDFToDOCXError {
	dir := filepath.Dir(outputPath)
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return &PDFToDOCXError{
				Code:    PDFToDOCXErrorOutputFailed,
				Message: fmt.Sprintf("output directory does not exist: %s", dir),
				Details: map[string]any{"outputDir": dir},
				Cause:   statErr,
			}
		}

		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output directory is not accessible: %s", dir),
			Details: map[string]any{"outputDir": dir},
			Cause:   statErr,
		}
	}
	if !info.IsDir() {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output path parent is not a directory: %s", dir),
			Details: map[string]any{"outputDir": dir},
		}
	}

	probe, probeErr := os.CreateTemp(dir, pdfToDOCXWriteCheckPattern)
	if probeErr != nil {
		return &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output directory is not writable: %s", dir),
			Details: map[string]any{"outputDir": dir},
			Cause:   probeErr,
		}
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	return nil
}

// ConvertPDFToDOCX validates the request, runs the embedded bridge, validates
// the generated OpenXML package and publishes the DOCX atomically. onProgress
// may be nil.
func ConvertPDFToDOCX(ctx context.Context, runner DOCXCommandRunner, req PDFToDOCXRequest, onProgress func(PDFToDOCXProgressEvent)) (PDFToDOCXResult, *PDFToDOCXError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return PDFToDOCXResult{}, cancelledPDFToDOCXError("PDF to DOCX canceled", ctx.Err())
	}

	if validationErr := ValidatePDFToDOCXRequest(req); validationErr != nil {
		return PDFToDOCXResult{}, validationErr
	}

	if runner == nil {
		runner = &ExecDOCXCommandRunner{}
	}

	inputPath := strings.TrimSpace(req.InputPath)
	outputPath := strings.TrimSpace(req.OutputPath)
	pythonPath := strings.TrimSpace(req.PythonPath)

	// The bridge runs with the workspace as its working directory, so every
	// path handed to it must be absolute.
	runInputPath := absoluteOrOriginal(inputPath)
	runOutputPath := absoluteOrOriginal(outputPath)

	workspace, workspaceErr := os.MkdirTemp("", pdfToDOCXTempDirPattern)
	if workspaceErr != nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorConversionFailed,
			Message: "could not create the PDF to DOCX workspace",
			Cause:   workspaceErr,
		}
	}
	defer func() {
		_ = os.RemoveAll(workspace)
	}()

	bridgePath := filepath.Join(workspace, pdfToDOCXBridgeFileName)
	if writeErr := os.WriteFile(bridgePath, pdfToDOCXBridgeSource, 0o644); writeErr != nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorConversionFailed,
			Message: "could not write the PDF to DOCX bridge",
			Cause:   writeErr,
		}
	}

	// pdf2docx overwrites the destination; keeping a pre-created empty file
	// reserves the random name next to the final output.
	tempFile, tempErr := os.CreateTemp(filepath.Dir(runOutputPath), pdfToDOCXTempFilePattern)
	if tempErr != nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("failed to create the output file in: %s", filepath.Dir(runOutputPath)),
			Details: map[string]any{"outputPath": outputPath, "outputDir": filepath.Dir(outputPath)},
			Cause:   tempErr,
		}
	}
	tempOutputPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempOutputPath)
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: "failed to prepare the output file",
			Cause:   closeErr,
		}
	}
	defer func() {
		_ = os.Remove(tempOutputPath)
	}()

	parser := newPDFToDOCXProgressParser()
	onLine := func(line string) {
		event, ok := parser.observe(line)
		if !ok || onProgress == nil {
			return
		}
		onProgress(event)
	}

	stderrTail, runErr := runner.RunDOCX(ctx, workspace, pythonPath, []string{bridgePath, runInputPath, tempOutputPath}, onLine)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return PDFToDOCXResult{}, cancelledPDFToDOCXError("PDF to DOCX canceled", ctxErr)
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			return PDFToDOCXResult{}, cancelledPDFToDOCXError("PDF to DOCX canceled", runErr)
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return PDFToDOCXResult{}, &PDFToDOCXError{
				Code:    PDFToDOCXErrorRuntimeUnavailable,
				Message: fmt.Sprintf("the PDF to DOCX Python interpreter could not run: %s", pythonPath),
				Details: map[string]any{"pythonPath": pythonPath},
				Cause:   runErr,
			}
		}

		details := map[string]any{"inputPath": inputPath, "outputPath": outputPath}
		if stderrTail != "" {
			details["stderr"] = stderrTail
		}

		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorConversionFailed,
			Message: fmt.Sprintf("failed to convert %s to DOCX", filepath.Base(inputPath)),
			Details: details,
			Cause:   runErr,
		}
	}

	outputInfo, outputErr := os.Stat(tempOutputPath)
	if outputErr != nil || !outputInfo.Mode().IsRegular() || outputInfo.Size() == 0 {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: "the converter produced no DOCX output",
			Details: map[string]any{"inputPath": inputPath, "outputPath": outputPath},
			Cause:   outputErr,
		}
	}

	stats, statsErr := readPDFToDOCXStats(tempOutputPath)
	if statsErr != nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: "the generated DOCX is not a valid OpenXML document",
			Details: map[string]any{"outputPath": outputPath},
			Cause:   statsErr,
		}
	}

	if chmodErr := os.Chmod(tempOutputPath, 0o644); chmodErr != nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: "failed to publish the DOCX file",
			Details: map[string]any{"outputPath": outputPath},
			Cause:   chmodErr,
		}
	}

	if _, statErr := os.Stat(runOutputPath); statErr == nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output file already exists: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("output path is not accessible: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
			Cause:   statErr,
		}
	}

	if renameErr := os.Rename(tempOutputPath, runOutputPath); renameErr != nil {
		return PDFToDOCXResult{}, &PDFToDOCXError{
			Code:    PDFToDOCXErrorOutputFailed,
			Message: fmt.Sprintf("failed to publish the DOCX file: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
			Cause:   renameErr,
		}
	}

	if onProgress != nil {
		onProgress(PDFToDOCXProgressEvent{
			Stage:   PDFToDOCXStageWrite,
			Percent: 100,
			Message: "DOCX saved",
		})
	}

	return PDFToDOCXResult{OutputPath: outputPath, Stats: stats}, nil
}

// pdfToDOCXProgressParser turns bridge output lines into monotonic progress.
// It tracks the pdf2docx stage because the "(n/total) Page" line appears twice:
// once while parsing and once while creating pages.
type pdfToDOCXProgressParser struct {
	stage   string
	percent int
}

func newPDFToDOCXProgressParser() *pdfToDOCXProgressParser {
	return &pdfToDOCXProgressParser{stage: PDFToDOCXStageParse}
}

// observe returns the progress event for one stdout line. It reports false when
// the line carries no progress or the integer percent did not change.
func (p *pdfToDOCXProgressParser) observe(rawLine string) (PDFToDOCXProgressEvent, bool) {
	line := cleanPDFToDOCXLogLine(rawLine)
	if line == "" {
		return PDFToDOCXProgressEvent{}, false
	}

	stage := p.stage
	percent := 0
	message := ""

	switch {
	case strings.Contains(line, "Start to convert"):
		percent, message = 5, "Starting the conversion"
	case strings.Contains(line, "Opening document"):
		percent, message = 8, "Opening the PDF"
	case strings.Contains(line, "Analyzing document"):
		percent, message = 12, "Analyzing the document"
	case strings.Contains(line, "Parsing pages"):
		percent, message = 15, "Parsing pages"
	case strings.Contains(line, "Creating pages"):
		stage, percent, message = PDFToDOCXStageWrite, 55, "Creating pages"
	case strings.Contains(line, "Terminated"):
		stage, percent, message = PDFToDOCXStageWrite, 95, "Finalizing the DOCX"
	default:
		page, total, ok := parsePDFToDOCXPageLine(line)
		if !ok {
			return PDFToDOCXProgressEvent{}, false
		}

		switch p.stage {
		case PDFToDOCXStageParse:
			percent = 15 + (35*page)/total
			if percent > 50 {
				percent = 50
			}
			message = fmt.Sprintf("Parsing page %d of %d", page, total)
		case PDFToDOCXStageWrite:
			stage = PDFToDOCXStageWrite
			percent = 55 + (35*page)/total
			if percent > 90 {
				percent = 90
			}
			message = fmt.Sprintf("Creating page %d of %d", page, total)
		default:
			return PDFToDOCXProgressEvent{}, false
		}
	}

	p.stage = stage
	if percent <= p.percent || percent > 100 {
		return PDFToDOCXProgressEvent{}, false
	}
	p.percent = percent

	return PDFToDOCXProgressEvent{Stage: stage, Percent: percent, Message: message}, true
}

func cleanPDFToDOCXLogLine(line string) string {
	cleaned := pdfToDOCXANSIPattern.ReplaceAllString(line, "")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimPrefix(cleaned, "LOG ")

	return strings.TrimSpace(cleaned)
}

func parsePDFToDOCXPageLine(line string) (int, int, bool) {
	matches := pdfToDOCXPageLinePattern.FindStringSubmatch(line)
	if len(matches) != 3 {
		return 0, 0, false
	}

	page, pageErr := strconv.Atoi(matches[1])
	total, totalErr := strconv.Atoi(matches[2])
	if pageErr != nil || totalErr != nil || page < 1 || total < 1 || page > total {
		return 0, 0, false
	}

	return page, total, true
}

// readPDFToDOCXStats validates the OpenXML package and counts its structural
// elements. A DOCX without word/document.xml, without paragraphs or with an
// empty document part is rejected.
func readPDFToDOCXStats(path string) (PDFToDOCXStats, error) {
	stats := PDFToDOCXStats{}

	reader, openErr := zip.OpenReader(path)
	if openErr != nil {
		return stats, fmt.Errorf("open the DOCX package: %w", openErr)
	}
	defer func() {
		_ = reader.Close()
	}()

	var document *zip.File
	for _, file := range reader.File {
		if file.Name == pdfToDOCXRootElement {
			document = file
			break
		}
	}
	if document == nil {
		return stats, fmt.Errorf("%s is missing", pdfToDOCXRootElement)
	}

	documentReader, documentErr := document.Open()
	if documentErr != nil {
		return stats, fmt.Errorf("open %s: %w", pdfToDOCXRootElement, documentErr)
	}
	defer func() {
		_ = documentReader.Close()
	}()

	counter := &countingReader{reader: documentReader}
	decoder := xml.NewDecoder(counter)
	textDepth := 0

	for {
		token, tokenErr := decoder.Token()
		if errors.Is(tokenErr, io.EOF) {
			break
		}
		if tokenErr != nil {
			return stats, fmt.Errorf("parse %s: %w", pdfToDOCXRootElement, tokenErr)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "p":
				stats.Paragraphs++
			case "r":
				stats.Runs++
			case "b":
				if docxBoldEnabled(element) {
					stats.BoldRuns++
				}
			case "drawing":
				stats.Images++
			case "tbl":
				stats.Tables++
			case "t":
				textDepth++
			}
		case xml.EndElement:
			if element.Name.Local == "t" && textDepth > 0 {
				textDepth--
			}
		case xml.CharData:
			if textDepth > 0 {
				stats.TextCharacters += CountTextCharacters(string(element))
			}
		}
	}

	if counter.count == 0 {
		return PDFToDOCXStats{}, fmt.Errorf("%s is empty", pdfToDOCXRootElement)
	}
	if stats.Paragraphs == 0 {
		return PDFToDOCXStats{}, errors.New("the document has no paragraphs")
	}

	return stats, nil
}

// docxBoldEnabled reports whether a w:b element turns bold on. w:val="0" and
// w:val="false" explicitly disable it.
func docxBoldEnabled(element xml.StartElement) bool {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != "val" {
			continue
		}

		value := strings.ToLower(strings.TrimSpace(attribute.Value))

		return value != "0" && value != "false" && value != "off"
	}

	return true
}

// countingReader tracks how many bytes were consumed from a ZIP entry.
type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	read, err := r.reader.Read(buffer)
	r.count += int64(read)

	return read, err
}

func absoluteOrOriginal(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return absolute
}

func cancelledPDFToDOCXError(message string, cause error) *PDFToDOCXError {
	return &PDFToDOCXError{Code: PDFToDOCXErrorCancelled, Message: message, Cause: cause}
}
