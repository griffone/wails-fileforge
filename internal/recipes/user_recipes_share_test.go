package recipes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

// newShareTestService builds an isolated recipe service with its own user
// catalog. Export and import never touch job history.
func newShareTestService(t *testing.T) (*Service, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config", "user_recipes.json")
	service := NewService(registry.NewRegistry(), nil, nil)
	service.SetUserRecipesPath(path)

	return service, path
}

func saveShareRecipe(t *testing.T, service *Service, name string, options map[string]any) models.Recipe {
	t.Helper()

	response := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         name,
		BaseRecipeID: RecipeIDEmailPDF,
		Options:      options,
	})
	if !response.Success || response.Recipe == nil {
		t.Fatalf("save %q: %+v", name, response)
	}

	return *response.Recipe
}

func writeExportFile(t *testing.T, path string, payload userRecipeExportFile) {
	t.Helper()

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("encode export fixture: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write export fixture: %v", err)
	}
}

func recipeNames(recipes []models.Recipe) []string {
	names := make([]string, 0, len(recipes))
	for _, recipe := range recipes {
		if recipe.UserDefined {
			names = append(names, recipe.Name)
		}
	}

	return names
}

func TestExportImportUserRecipesRoundTrip(t *testing.T) {
	source, _ := newShareTestService(t)

	small := saveShareRecipe(t, source, "Small email", map[string]any{
		emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte),
	})
	large := saveShareRecipe(t, source, "Large email", map[string]any{
		emailPDFTargetOptionKey: 8 * int64(bytesPerMegabyte),
	})

	file := filepath.Join(t.TempDir(), "shared", "recipes.json")
	exported := source.ExportUserRecipes(file, []string{small.ID, large.ID})
	if !exported.Success || exported.Exported != 2 || exported.Path != file {
		t.Fatalf("export failed: %+v", exported)
	}

	var onDisk userRecipeExportFile
	if err := jsonstoreLoad(file, &onDisk); err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if onDisk.FormatVersion != userRecipeExportFormatVersion {
		t.Fatalf("expected format version %d, got %d", userRecipeExportFormatVersion, onDisk.FormatVersion)
	}
	if onDisk.ExportedAt == "" || len(onDisk.Recipes) != 2 {
		t.Fatalf("unexpected export payload: %+v", onDisk)
	}
	if onDisk.Recipes[0].BaseRecipeID != RecipeIDEmailPDF || onDisk.Recipes[0].Options == nil {
		t.Fatalf("expected base and options in the payload, got %+v", onDisk.Recipes[0])
	}

	target, _ := newShareTestService(t)
	imported := target.ImportUserRecipes(file, false)
	if !imported.Success || imported.Imported != 2 || imported.Skipped != 0 || len(imported.Invalid) != 0 {
		t.Fatalf("expected a clean import, got %+v", imported)
	}

	names := recipeNames(target.List())
	if len(names) != 2 {
		t.Fatalf("expected two imported recipes, got %+v", names)
	}

	// Importing the same file again must not duplicate anything.
	second := target.ImportUserRecipes(file, false)
	if !second.Success || second.Imported != 0 || second.Skipped != 2 || len(second.Invalid) != 0 {
		t.Fatalf("expected the second import to skip both, got %+v", second)
	}
	if names = recipeNames(target.List()); len(names) != 2 {
		t.Fatalf("expected the catalog to stay at two recipes, got %+v", names)
	}

	// Overwrite replaces in place, so the catalog still holds one entry each.
	overwritten := target.ImportUserRecipes(file, true)
	if !overwritten.Success || overwritten.Imported != 2 || overwritten.Skipped != 0 {
		t.Fatalf("expected overwrite to replace both, got %+v", overwritten)
	}
	if names = recipeNames(target.List()); len(names) != 2 {
		t.Fatalf("expected overwrite to keep a single entry per name, got %+v", names)
	}
}

func TestImportUserRecipesOverwriteUpdatesOptions(t *testing.T) {
	source, _ := newShareTestService(t)
	saved := saveShareRecipe(t, source, "Tuned email", map[string]any{
		emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte),
	})

	file := filepath.Join(t.TempDir(), "recipes.json")
	if exported := source.ExportUserRecipes(file, []string{saved.ID}); !exported.Success {
		t.Fatalf("export failed: %+v", exported)
	}

	target, _ := newShareTestService(t)
	if imported := target.ImportUserRecipes(file, false); !imported.Success || imported.Imported != 1 {
		t.Fatalf("first import failed: %+v", imported)
	}
	original := target.List()
	if len(original) != 6 {
		t.Fatalf("expected five built-ins plus one user recipe, got %d", len(original))
	}

	var payload userRecipeExportFile
	if err := jsonstoreLoad(file, &payload); err != nil {
		t.Fatalf("read export: %v", err)
	}
	payload.Recipes[0].Options[emailPDFTargetOptionKey] = 4 * int64(bytesPerMegabyte)
	writeExportFile(t, file, payload)

	replaced := target.ImportUserRecipes(file, true)
	if !replaced.Success || replaced.Imported != 1 || replaced.Skipped != 0 {
		t.Fatalf("overwrite import failed: %+v", replaced)
	}

	listed := findListedRecipe(target.List(), saved.ID)
	if listed == nil {
		// IDs are local to each store; find the user recipe by name instead.
		for _, recipe := range target.List() {
			if recipe.UserDefined {
				listed = &recipe
				break
			}
		}
	}
	if listed == nil {
		t.Fatal("expected the overwritten recipe in the catalog")
	}
	got, ok := numericOption(listed.OptionOverrides[emailPDFTargetOptionKey])
	if !ok || int64(got) != 4*bytesPerMegabyte {
		t.Fatalf("expected the overwritten target, got %v", listed.OptionOverrides)
	}
	if names := recipeNames(target.List()); len(names) != 1 {
		t.Fatalf("expected overwrite to keep a single entry, got %+v", names)
	}
}

func TestImportUserRecipesReportsInvalidEntriesAndKeepsValidOnes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recipes.json")
	writeExportFile(t, file, userRecipeExportFile{
		FormatVersion: userRecipeExportFormatVersion,
		ExportedAt:    "2026-01-01T00:00:00Z",
		Recipes: []exportedUserRecipe{
			{
				Name:         "Valid email",
				BaseRecipeID: RecipeIDEmailPDF,
				Options:      map[string]any{emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte)},
			},
			{
				Name:         "Missing base",
				BaseRecipeID: "recipe.removed",
				Options:      map[string]any{},
			},
			{
				Name:         "Unknown option",
				BaseRecipeID: RecipeIDEmailPDF,
				Options:      map[string]any{"notAnOption": 1},
			},
			{
				Name:         "Out of range",
				BaseRecipeID: RecipeIDEmailPDF,
				Options:      map[string]any{emailPDFTargetOptionKey: 0},
			},
		},
	})

	service, _ := newShareTestService(t)
	response := service.ImportUserRecipes(file, false)
	if !response.Success {
		t.Fatalf("expected a completed import, got %+v", response)
	}
	if response.Imported != 1 || response.Skipped != 0 {
		t.Fatalf("expected one imported and no skipped, got %+v", response)
	}
	if len(response.Invalid) != 3 {
		t.Fatalf("expected three invalid entries, got %+v", response.Invalid)
	}

	reasons := make(map[string]string, len(response.Invalid))
	for _, issue := range response.Invalid {
		reasons[issue.Name] = issue.Reason
	}
	if !strings.Contains(reasons["Missing base"], "recipe.removed") {
		t.Fatalf("expected the missing base in the reason, got %q", reasons["Missing base"])
	}
	if !strings.Contains(reasons["Unknown option"], "notAnOption") {
		t.Fatalf("expected the unknown key in the reason, got %q", reasons["Unknown option"])
	}
	if !strings.Contains(reasons["Out of range"], emailPDFTargetOptionKey) {
		t.Fatalf("expected the ranged option in the reason, got %q", reasons["Out of range"])
	}

	names := recipeNames(service.List())
	if len(names) != 1 || names[0] != "Valid email" {
		t.Fatalf("expected only the valid entry to be imported, got %+v", names)
	}
}

func TestImportUserRecipesUnsupportedVersionReportsEveryEntry(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recipes.json")
	writeExportFile(t, file, userRecipeExportFile{
		FormatVersion: userRecipeExportFormatVersion + 99,
		Recipes: []exportedUserRecipe{
			{Name: "First", BaseRecipeID: RecipeIDEmailPDF, Options: map[string]any{}},
			{Name: "Second", BaseRecipeID: RecipeIDEmailPDF, Options: map[string]any{}},
		},
	})

	service, _ := newShareTestService(t)
	response := service.ImportUserRecipes(file, false)
	if !response.Success || response.Imported != 0 {
		t.Fatalf("expected nothing imported, got %+v", response)
	}
	if len(response.Invalid) != 2 {
		t.Fatalf("expected both entries reported as invalid, got %+v", response.Invalid)
	}
	for _, issue := range response.Invalid {
		if !strings.Contains(issue.Reason, "version") {
			t.Fatalf("expected a version reason, got %+v", issue)
		}
	}
	if names := recipeNames(service.List()); len(names) != 0 {
		t.Fatalf("expected an untouched store, got %+v", names)
	}
}

func TestImportUserRecipesHandlesDuplicateNamesDeterministically(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recipes.json")
	writeExportFile(t, file, userRecipeExportFile{
		FormatVersion: userRecipeExportFormatVersion,
		Recipes: []exportedUserRecipe{
			{Name: "Same name", BaseRecipeID: RecipeIDEmailPDF, Options: map[string]any{emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte)}},
			{Name: "same NAME", BaseRecipeID: RecipeIDEmailPDF, Options: map[string]any{emailPDFTargetOptionKey: 4 * int64(bytesPerMegabyte)}},
		},
	})

	service, _ := newShareTestService(t)
	response := service.ImportUserRecipes(file, false)
	if !response.Success || response.Imported != 1 || response.Skipped != 1 {
		t.Fatalf("expected first wins and second skips, got %+v", response)
	}
	if names := recipeNames(service.List()); len(names) != 1 {
		t.Fatalf("expected a single entry, got %+v", names)
	}

	overwriteService, _ := newShareTestService(t)
	overwritten := overwriteService.ImportUserRecipes(file, true)
	if !overwritten.Success || overwritten.Imported != 2 || overwritten.Skipped != 0 {
		t.Fatalf("expected overwrite to apply both in order, got %+v", overwritten)
	}
	names := recipeNames(overwriteService.List())
	if len(names) != 1 {
		t.Fatalf("expected a single surviving entry, got %+v", names)
	}
	for _, recipe := range overwriteService.List() {
		if !recipe.UserDefined {
			continue
		}
		got, ok := numericOption(recipe.OptionOverrides[emailPDFTargetOptionKey])
		if !ok || int64(got) != 4*bytesPerMegabyte {
			t.Fatalf("expected the later duplicate to win, got %v", recipe.OptionOverrides)
		}
	}
}

func TestExportUserRecipesRejectsUnknownAndEmptySelections(t *testing.T) {
	service, _ := newShareTestService(t)
	saved := saveShareRecipe(t, service, "Known", map[string]any{})

	file := filepath.Join(t.TempDir(), "recipes.json")
	unknown := service.ExportUserRecipes(file, []string{saved.ID, "recipe.user.missing"})
	if unknown.Success {
		t.Fatalf("expected an unknown id to fail the export, got %+v", unknown)
	}
	if unknown.Error == nil || !strings.Contains(unknown.Message, "recipe.user.missing") {
		t.Fatalf("expected the missing id in the error, got %+v", unknown)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("expected no file written for a failed export, got %v", err)
	}

	empty := service.ExportUserRecipes(file, nil)
	if empty.Success || empty.Error == nil || empty.Error.DetailCode != "RECIPE_EXPORT_EMPTY" {
		t.Fatalf("expected an empty selection to fail, got %+v", empty)
	}
}

func TestImportUserRecipesCorruptFileLeavesStoreIntact(t *testing.T) {
	service, path := newShareTestService(t)
	saved := saveShareRecipe(t, service, "Survivor", map[string]any{})

	file := filepath.Join(t.TempDir(), "recipes.json")
	if err := os.WriteFile(file, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	response := service.ImportUserRecipes(file, false)
	if response.Success || response.Error == nil || response.Error.DetailCode != "RECIPE_IMPORT_INVALID_FILE" {
		t.Fatalf("expected a clean invalid-file failure, got %+v", response)
	}
	if listed := findListedRecipe(service.List(), saved.ID); listed == nil {
		t.Fatalf("expected the existing recipe to survive, got %+v", service.List())
	}

	var persisted persistedUserRecipes
	if err := jsonstoreLoad(path, &persisted); err != nil {
		t.Fatalf("store is no longer readable: %v", err)
	}
	if len(persisted.Recipes) != 1 || persisted.Recipes[0].Name != "Survivor" {
		t.Fatalf("expected the store to stay valid, got %+v", persisted.Recipes)
	}
}

// jsonstoreLoad reads a JSON file through the same decoder the store uses.
func jsonstoreLoad(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}
