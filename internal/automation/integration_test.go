package automation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"
)

// TestAutomationRunsRealEmailPDFRecipe exercises the full automation path with
// the real recipe service instead of a fake runner: fsnotify event, stability
// window, recipe plan/run/status, output verification and last-run persistence.
func TestAutomationRunsRealEmailPDFRecipe(t *testing.T) {
	reg := registry.NewRegistry()
	if err := reg.RegisterToolV2(pdf.NewMergeTool()); err != nil {
		t.Fatalf("register PDF merge: %v", err)
	}
	if err := reg.RegisterToolV2(pdf.NewOptimizeTool()); err != nil {
		t.Fatalf("register PDF optimize: %v", err)
	}

	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history_v1.json"))
	recipeService := recipes.NewService(reg, history, nil)
	store := NewStore(filepath.Join(t.TempDir(), "automation", "automation_rules_v1.json"))
	service := NewService(store, recipeService, nil)
	service.stabilityDelay = 50 * time.Millisecond
	service.statusPollInterval = 20 * time.Millisecond
	service.runTimeout = 30 * time.Second

	watchDir := t.TempDir()
	outputDir := t.TempDir()
	saved := service.SaveRule(models.AutomationRuleV1{
		Name:      "Email PDFs",
		Enabled:   true,
		WatchDir:  watchDir,
		RecipeID:  recipes.RecipeIDEmailPDF,
		OutputDir: outputDir,
	})
	if !saved.Success || saved.Rule == nil {
		t.Fatalf("expected the rule to save, got %+v", saved)
	}

	service.Start(context.Background())
	t.Cleanup(service.Shutdown)

	writeAutomationPDFFixture(t, filepath.Join(watchDir, "report.pdf"), 2)

	deadline := time.Now().Add(30 * time.Second)
	var rule models.AutomationRuleV1
	for time.Now().Before(deadline) {
		current, found := store.Get(saved.Rule.ID)
		if found && current.LastStatus != "" {
			rule = current
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if rule.LastStatus == "" {
		t.Fatal("timed out waiting for the automation run to finish")
	}
	if rule.LastStatus != models.JobStatusSuccess {
		t.Fatalf("expected a successful automation run, got %+v", rule)
	}
	if len(rule.LastOutputs) != 1 {
		t.Fatalf("expected one recorded output, got %+v", rule.LastOutputs)
	}

	output := rule.LastOutputs[0]
	if filepath.Dir(output) != outputDir {
		t.Fatalf("expected the output in %q, got %q", outputDir, output)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected a non-empty output, info=%v err=%v", info, err)
	}
}

func writeAutomationPDFFixture(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 12)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.Cell(40, 10, "Automation page")
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf fixture %s: %v", path, err)
	}
}
