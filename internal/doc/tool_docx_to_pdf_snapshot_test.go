package doc

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"

	"github.com/jung-kurt/gofpdf"
)

type snapshotRunner struct {
	t        *testing.T
	plans    map[string]snapshotPlan
	invoked  int
	fallback bool
}

type snapshotPlan struct {
	title string
	pages int
}

func (r *snapshotRunner) Run(_ context.Context, name string, args ...string) (engine.CommandResult, error) {
	r.invoked++
	if name != "libreoffice" {
		r.fallback = true
		return engine.CommandResult{}, fmt.Errorf("unexpected fallback invocation")
	}

	input := lastArg(args)
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	plan, ok := r.plans[base]
	if !ok {
		return engine.CommandResult{}, fmt.Errorf("no snapshot plan for input %s", input)
	}

	outDir := ""
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--outdir" {
			outDir = args[i+1]
			break
		}
	}
	if outDir == "" {
		return engine.CommandResult{}, fmt.Errorf("missing --outdir")
	}

	output := filepath.Join(outDir, base+".pdf")
	if err := createDeterministicPDF(output, plan.title, plan.pages); err != nil {
		return engine.CommandResult{}, err
	}

	return engine.CommandResult{}, nil
}

func TestDOCXToPDFSnapshots_PerPageTolerance2Percent(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm is required for per-page snapshot assertions")
	}

	tmpDir := t.TempDir()
	inputsDir := filepath.Join(tmpDir, "fixtures")
	outputsDir := filepath.Join(tmpDir, "outputs")
	expectedDir := filepath.Join(tmpDir, "expected")
	if err := os.MkdirAll(inputsDir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
		t.Fatalf("mkdir outputs: %v", err)
	}
	if err := os.MkdirAll(expectedDir, 0o755); err != nil {
		t.Fatalf("mkdir expected: %v", err)
	}

	dataset := []snapshotPlan{
		{title: "fixture_simple", pages: 1},
		{title: "fixture_medium", pages: 2},
		{title: "fixture_complex_tables", pages: 2},
		{title: "fixture_complex_fonts", pages: 1},
		{title: "fixture_complex_layout", pages: 3},
	}

	plans := make(map[string]snapshotPlan, len(dataset))
	inputPaths := make([]string, 0, len(dataset))
	for _, item := range dataset {
		base := item.title
		plans[base] = item
		inputPath := filepath.Join(inputsDir, base+".docx")
		if err := createMinimalDOCX(inputPath, false); err != nil {
			t.Fatalf("create docx fixture %s: %v", base, err)
		}
		inputPaths = append(inputPaths, inputPath)

		expectedPDF := filepath.Join(expectedDir, base+".pdf")
		if err := createDeterministicPDF(expectedPDF, item.title, item.pages); err != nil {
			t.Fatalf("create expected pdf %s: %v", base, err)
		}
	}

	runner := &snapshotRunner{t: t, plans: plans}
	tool := NewDOCXToPDFToolWithDeps(fakeDOCXProbe{}, runner)

	for _, inputPath := range inputPaths {
		item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
			ToolID:     ToolIDDocDOCXToPDFV1,
			Mode:       "single",
			InputPaths: []string{inputPath},
			OutputDir:  outputsDir,
		})
		if jobErr != nil || !item.Success {
			t.Fatalf("unexpected conversion failure for %s: item=%+v err=%+v", inputPath, item, jobErr)
		}

		base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		expectedPDF := filepath.Join(expectedDir, base+".pdf")
		assertPDFPerPageTolerance(t, expectedPDF, item.OutputPath, 0.02)
	}

	if runner.fallback {
		t.Fatalf("snapshot runner should not invoke fallback engine in baseline test")
	}
	if runner.invoked != len(dataset) {
		t.Fatalf("expected %d invocations, got %d", len(dataset), runner.invoked)
	}
}

func assertPDFPerPageTolerance(t *testing.T, expectedPDF, actualPDF string, tolerance float64) {
	t.Helper()

	expectedPNGs := rasterizePDFToPNGs(t, expectedPDF)
	actualPNGs := rasterizePDFToPNGs(t, actualPDF)

	if len(expectedPNGs) != len(actualPNGs) {
		t.Fatalf("page count mismatch expected=%d actual=%d", len(expectedPNGs), len(actualPNGs))
	}

	for i := range expectedPNGs {
		diffRatio := diffPNGBytesRatio(t, expectedPNGs[i], actualPNGs[i])
		if diffRatio > tolerance {
			t.Fatalf("page %d diff ratio %.4f exceeds tolerance %.4f", i+1, diffRatio, tolerance)
		}
	}
}

func rasterizePDFToPNGs(t *testing.T, pdfPath string) [][]byte {
	t.Helper()

	tmpDir := t.TempDir()
	prefix := filepath.Join(tmpDir, "page")

	cmd := exec.Command("pdftoppm", "-png", pdfPath, prefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pdftoppm failed: %v (%s)", err, string(out))
	}

	entries, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		t.Fatalf("glob rasterized pages: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("no pages rasterized for %s", pdfPath)
	}

	pages := make([][]byte, 0, len(entries))
	for _, entry := range entries {
		b, readErr := os.ReadFile(entry)
		if readErr != nil {
			t.Fatalf("read rasterized page %s: %v", entry, readErr)
		}
		pages = append(pages, b)
	}

	return pages
}

func diffPNGBytesRatio(t *testing.T, expectedPNG, actualPNG []byte) float64 {
	t.Helper()

	expectedImg, err := png.Decode(bytes.NewReader(expectedPNG))
	if err != nil {
		t.Fatalf("decode expected png: %v", err)
	}
	actualImg, err := png.Decode(bytes.NewReader(actualPNG))
	if err != nil {
		t.Fatalf("decode actual png: %v", err)
	}

	bounds := expectedImg.Bounds()
	if !bounds.Eq(actualImg.Bounds()) {
		t.Fatalf("png bounds mismatch expected=%v actual=%v", bounds, actualImg.Bounds())
	}

	total := bounds.Dx() * bounds.Dy()
	if total == 0 {
		return 0
	}

	diffPixels := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if !sameRGBA(expectedImg.At(x, y), actualImg.At(x, y)) {
				diffPixels++
			}
		}
	}

	return float64(diffPixels) / float64(total)
}

func sameRGBA(left, right color.Color) bool {
	lr, lg, lb, la := left.RGBA()
	rr, rg, rb, ra := right.RGBA()
	return lr == rr && lg == rg && lb == rb && la == ra
}

func createDeterministicPDF(outputPath, title string, pages int) error {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetCreator("FileForge Snapshot", true)
	pdf.SetTitle(title, true)

	for page := 1; page <= pages; page++ {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "", 14)
		pdf.CellFormat(0, 10, fmt.Sprintf("%s - page %d", title, page), "", 1, "L", false, 0, "")
		pdf.SetFont("Courier", "", 10)
		for i := 0; i < 20; i++ {
			pdf.CellFormat(0, 6, fmt.Sprintf("line %02d content block for %s", i+1, title), "", 1, "L", false, 0, "")
		}
	}

	return pdf.OutputFileAndClose(outputPath)
}

func createMinimalDOCX(path string, protected bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	if _, err := zw.Create("[Content_Types].xml"); err != nil {
		return err
	}
	if _, err := zw.Create("word/document.xml"); err != nil {
		return err
	}
	settingsWriter, err := zw.Create("word/settings.xml")
	if err != nil {
		return err
	}

	settings := `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"></w:settings>`
	if protected {
		settings = `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:documentProtection w:edit="readOnly"/></w:settings>`
	}
	if _, err := settingsWriter.Write([]byte(settings)); err != nil {
		return err
	}

	return zw.Close()
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}
