package recipes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func newUserRecipeTestService(t *testing.T, web bool) (*Service, *jobhistory.Store, string) {
	t.Helper()

	var (
		service *Service
		history *jobhistory.Store
	)
	if web {
		service, history, _ = newWebImageSetTestService(t)
	} else {
		service, history, _ = newTestService(t)
	}

	path := filepath.Join(t.TempDir(), "config", "user_recipes.json")
	service.SetUserRecipesPath(path)

	return service, history, path
}

func findListedRecipe(recipes []models.Recipe, id string) *models.Recipe {
	for index := range recipes {
		if recipes[index].ID == id {
			return &recipes[index]
		}
	}

	return nil
}

func TestRecipeOptionSchemaIsDeclaredOnlyForSupportedRecipes(t *testing.T) {
	service := NewService(registry.NewRegistry(), nil, nil)

	recipes := service.List()
	if len(recipes) != 5 {
		t.Fatalf("expected 5 built-in recipes, got %d", len(recipes))
	}

	email := findListedRecipe(recipes, RecipeIDEmailPDF)
	if email == nil {
		t.Fatal("expected Email PDF in the catalog")
	}
	if !email.Duplicable || len(email.OptionSchema) != 1 {
		t.Fatalf("expected Email PDF to declare one duplicable option, got %+v", email)
	}
	field := email.OptionSchema[0]
	if field.Key != emailPDFTargetOptionKey || field.Kind != models.RecipeOptionInteger {
		t.Fatalf("unexpected Email PDF option: %+v", field)
	}
	if field.Unit != "MB" || field.Scale != bytesPerMegabyte {
		t.Fatalf("expected an MB field scaled to bytes, got %+v", field)
	}
	if field.Default != int64(emailPDFDefaultTargetBytes) && field.Default != emailPDFDefaultTargetBytes {
		t.Fatalf("expected the default to equal DefaultTargetSizeBytes, got %+v", field.Default)
	}

	web := findListedRecipe(recipes, RecipeIDWebImageSet)
	if web == nil {
		t.Fatal("expected Web image set in the catalog")
	}
	if !web.Duplicable || len(web.OptionSchema) != 1 {
		t.Fatalf("expected Web image set to declare one duplicable option, got %+v", web)
	}
	preset := web.OptionSchema[0]
	if preset.Key != webImageSetVariantPresetOptionKey || preset.Kind != models.RecipeOptionEnum {
		t.Fatalf("unexpected Web image set option: %+v", preset)
	}
	if preset.Default != webImageSetPresetStandard || len(preset.Values) < 3 {
		t.Fatalf("expected the standard preset plus alternatives, got %+v", preset)
	}

	for _, id := range []string{RecipeIDSocialClip, RecipeIDCleanScan, RecipeIDBurnedSubtitles} {
		recipe := findListedRecipe(recipes, id)
		if recipe == nil {
			t.Fatalf("expected %s in the catalog", id)
		}
		if len(recipe.OptionSchema) != 0 || recipe.Duplicable {
			t.Fatalf("expected %s to have no schema and not be duplicable, got %+v", id, recipe)
		}
	}
}

func TestValidateRecipeOptions(t *testing.T) {
	email := emailPDFRecipe()
	web := webImageSetRecipe()

	megabyte := int64(bytesPerMegabyte)
	valid := []struct {
		name     string
		recipe   models.Recipe
		options  map[string]any
		wantKey  string
		wantKind any
	}{
		{
			name:     "valid email target",
			recipe:   email,
			options:  map[string]any{emailPDFTargetOptionKey: 5 * megabyte},
			wantKey:  emailPDFTargetOptionKey,
			wantKind: int64(5 * megabyte),
		},
		{
			name:     "float whole email target",
			recipe:   email,
			options:  map[string]any{emailPDFTargetOptionKey: float64(3 * megabyte)},
			wantKey:  emailPDFTargetOptionKey,
			wantKind: int64(3 * megabyte),
		},
		{
			name:     "valid web preset",
			recipe:   web,
			options:  map[string]any{webImageSetVariantPresetOptionKey: webImageSetPresetCompact},
			wantKey:  webImageSetVariantPresetOptionKey,
			wantKind: webImageSetPresetCompact,
		},
	}

	for _, testCase := range valid {
		t.Run(testCase.name, func(t *testing.T) {
			normalized, jobErr := validateRecipeOptions(&testCase.recipe, testCase.options)
			if jobErr != nil {
				t.Fatalf("expected valid options, got %+v", jobErr)
			}
			if normalized[testCase.wantKey] != testCase.wantKind {
				t.Fatalf("expected %v, got %v", testCase.wantKind, normalized[testCase.wantKey])
			}
		})
	}

	invalid := []struct {
		name       string
		recipe     models.Recipe
		options    map[string]any
		wantDetail string
	}{
		{
			name:       "unknown key",
			recipe:     email,
			options:    map[string]any{"nope": 1},
			wantDetail: "RECIPE_OPTION_UNKNOWN",
		},
		{
			name:       "wrong integer type",
			recipe:     email,
			options:    map[string]any{emailPDFTargetOptionKey: "ten"},
			wantDetail: "RECIPE_OPTION_INVALID_TYPE",
		},
		{
			name:       "fractional integer",
			recipe:     email,
			options:    map[string]any{emailPDFTargetOptionKey: 1.5},
			wantDetail: "RECIPE_OPTION_INVALID_TYPE",
		},
		{
			name:       "below minimum",
			recipe:     email,
			options:    map[string]any{emailPDFTargetOptionKey: 0},
			wantDetail: "RECIPE_OPTION_OUT_OF_RANGE",
		},
		{
			name:       "above maximum",
			recipe:     email,
			options:    map[string]any{emailPDFTargetOptionKey: emailPDFTargetMaxBytes + 1},
			wantDetail: "RECIPE_OPTION_OUT_OF_RANGE",
		},
		{
			name:       "unknown enum value",
			recipe:     web,
			options:    map[string]any{webImageSetVariantPresetOptionKey: "gigantic"},
			wantDetail: "RECIPE_OPTION_INVALID_VALUE",
		},
		{
			name:       "wrong enum type",
			recipe:     web,
			options:    map[string]any{webImageSetVariantPresetOptionKey: 12},
			wantDetail: "RECIPE_OPTION_INVALID_VALUE",
		},
	}

	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			_, jobErr := validateRecipeOptions(&testCase.recipe, testCase.options)
			if jobErr == nil || jobErr.DetailCode != testCase.wantDetail {
				t.Fatalf("expected %s, got %+v", testCase.wantDetail, jobErr)
			}
			if jobErr.Code != models.CanonicalErrorCode(testCase.wantDetail) {
				t.Fatalf("expected a canonical validation error, got %+v", jobErr)
			}
		})
	}
}

func TestUserRecipeStoreRoundTrip(t *testing.T) {
	service, _, path := newUserRecipeTestService(t, false)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Small email",
		BaseRecipeID: RecipeIDEmailPDF,
		Options:      map[string]any{emailPDFTargetOptionKey: 5 * int64(bytesPerMegabyte)},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the recipe to save, got %+v", saved)
	}
	if !saved.Recipe.UserDefined || saved.Recipe.BaseRecipeID != RecipeIDEmailPDF {
		t.Fatalf("expected a user recipe based on Email PDF, got %+v", saved.Recipe)
	}

	restored := NewService(registry.NewRegistry(), nil, nil)
	restored.SetUserRecipesPath(path)

	listed := findListedRecipe(restored.List(), saved.Recipe.ID)
	if listed == nil {
		t.Fatalf("expected %s after reload, got %+v", saved.Recipe.ID, restored.List())
	}
	if !listed.UserDefined || listed.BaseRecipeID != RecipeIDEmailPDF {
		t.Fatalf("unexpected reloaded recipe: %+v", listed)
	}
	if listed.Name != "Small email" {
		t.Fatalf("expected the saved name, got %q", listed.Name)
	}
	got, ok := numericOption(listed.OptionOverrides[emailPDFTargetOptionKey])
	if !ok || int64(got) != int64(5*bytesPerMegabyte) {
		t.Fatalf("expected the saved override after reload, got %v", listed.OptionOverrides)
	}
	if !listed.Duplicable {
		t.Fatal("expected an Email PDF duplicate to stay duplicable")
	}
}

func TestUserRecipeCorruptOrUnknownVersionStartsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{name: "corrupt json", content: "{not-json"},
		{name: "unknown version", content: `{"version":99,"recipes":[{"id":"recipe.user.1","name":"X","baseRecipeId":"recipe.email_pdf","options":{},"createdAt":1}]}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "user_recipes.json")
			if err := os.WriteFile(path, []byte(testCase.content), 0o644); err != nil {
				t.Fatalf("write store: %v", err)
			}

			service := NewService(registry.NewRegistry(), nil, nil)
			service.SetUserRecipesPath(path)

			recipes := service.List()
			if len(recipes) != 5 {
				t.Fatalf("expected only the 5 built-ins, got %+v", recipes)
			}
			for _, recipe := range recipes {
				if recipe.UserDefined {
					t.Fatalf("did not expect a user recipe, got %+v", recipe)
				}
			}
		})
	}
}

func TestDuplicateEmailPDFPlanAndRunUseOverrides(t *testing.T) {
	service, _, _ := newUserRecipeTestService(t, false)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Email under 2 MB",
		BaseRecipeID: RecipeIDEmailPDF,
		Options:      map[string]any{emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte)},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the recipe to save, got %+v", saved)
	}
	userID := saved.Recipe.ID

	if listed := findListedRecipe(service.List(), userID); listed == nil {
		t.Fatalf("expected %s in the list", userID)
	}

	plan := service.Plan(models.RecipeRequest{RecipeID: userID, InputPaths: []string{input}})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil || plan.Recipe.ID != userID || !plan.Recipe.UserDefined {
		t.Fatalf("expected the user recipe in the plan, got %+v", plan.Recipe)
	}
	if len(plan.Outputs) != 1 || plan.Outputs[0].OutputPath != filepath.Join(dir, "report_email.pdf") {
		t.Fatalf("unexpected planned output: %+v", plan.Outputs)
	}
	if !warningsContain(plan.Warnings, "2.0 MB") {
		t.Fatalf("expected the two megabyte target in the plan warning, got %+v", plan.Warnings)
	}

	run := service.Run(context.Background(), models.RecipeRequest{RecipeID: userID, InputPaths: []string{input}})
	if !run.Success {
		t.Fatalf("expected the run to be submitted, got %+v", run)
	}
	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}
	if result.RecipeID != userID {
		t.Fatalf("expected the run to record the user recipe id, got %q", result.RecipeID)
	}
	if result.TargetSizeBytes != 2*bytesPerMegabyte {
		t.Fatalf("expected the 2 MB override to reach the builder, got %d", result.TargetSizeBytes)
	}
	if len(result.Outputs) != 1 {
		t.Fatalf("expected one output, got %+v", result.Outputs)
	}
	if _, err := os.Stat(result.Outputs[0]); err != nil {
		t.Fatalf("expected the output to exist: %v", err)
	}
}

func TestDuplicateEmailPDFWithoutEditsMatchesBuiltInPlan(t *testing.T) {
	service, _, _ := newUserRecipeTestService(t, false)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 2)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Email copy",
		BaseRecipeID: RecipeIDEmailPDF,
		Options:      map[string]any{},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the recipe to save, got %+v", saved)
	}

	builtIn := service.Plan(models.RecipeRequest{RecipeID: RecipeIDEmailPDF, InputPaths: []string{input}})
	duplicated := service.Plan(models.RecipeRequest{RecipeID: saved.Recipe.ID, InputPaths: []string{input}})
	if !builtIn.Success || !duplicated.Success {
		t.Fatalf("expected both plans to succeed, got %+v / %+v", builtIn.Error, duplicated.Error)
	}
	if len(builtIn.Outputs) != len(duplicated.Outputs) || builtIn.Outputs[0].OutputPath != duplicated.Outputs[0].OutputPath {
		t.Fatalf("expected the same output, got %+v / %+v", builtIn.Outputs, duplicated.Outputs)
	}
	if builtIn.Message != duplicated.Message {
		t.Fatalf("expected the same plan message, got %q / %q", builtIn.Message, duplicated.Message)
	}
	if len(builtIn.Warnings) != len(duplicated.Warnings) {
		t.Fatalf("expected the same warnings, got %+v / %+v", builtIn.Warnings, duplicated.Warnings)
	}
}

func TestDuplicateWebImageSetUsesPresetWidths(t *testing.T) {
	service, _, _ := newUserRecipeTestService(t, true)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeRecipePNG(t, input, 2000, 1200, false)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Compact web set",
		BaseRecipeID: RecipeIDWebImageSet,
		Options:      map[string]any{webImageSetVariantPresetOptionKey: webImageSetPresetCompact},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the recipe to save, got %+v", saved)
	}
	userID := saved.Recipe.ID

	plan := service.Plan(models.RecipeRequest{RecipeID: userID, InputPaths: []string{input}})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	want := []string{
		filepath.Join(dir, "photo-1200.webp"),
		filepath.Join(dir, "photo-1200.jpg"),
		filepath.Join(dir, "photo-600.webp"),
		filepath.Join(dir, "photo-600.jpg"),
		filepath.Join(dir, "photo-web-set.zip"),
		filepath.Join(dir, "photo-srcset.html"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		if plan.Outputs[index].OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, plan.Outputs[index].OutputPath)
		}
	}

	run := service.Run(context.Background(), models.RecipeRequest{RecipeID: userID, InputPaths: []string{input}})
	if !run.Success {
		t.Fatalf("expected the run to be submitted, got %+v", run)
	}
	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}
	if len(result.Outputs) != len(want) {
		t.Fatalf("expected %d outputs, got %+v", len(want), result.Outputs)
	}
	for index, wantPath := range want {
		if result.Outputs[index] != wantPath {
			t.Fatalf("run output %d: expected %q, got %q", index, wantPath, result.Outputs[index])
		}
		if _, err := os.Stat(wantPath); err != nil {
			t.Fatalf("expected %s to exist: %v", wantPath, err)
		}
	}
}

func TestUserRecipeWithRemovedBaseFailsPlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user_recipes.json")
	payload := persistedUserRecipes{
		Version: userRecipesVersion,
		Recipes: []userRecipe{{
			ID:           "recipe.user.stale",
			Name:         "Stale recipe",
			BaseRecipeID: "recipe.removed",
			Options:      map[string]any{},
			CreatedAt:    1,
		}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}

	service := NewService(registry.NewRegistry(), nil, nil)
	service.SetUserRecipesPath(path)

	list := service.List()
	if listed := findListedRecipe(list, "recipe.user.stale"); listed == nil {
		t.Fatalf("expected the stale recipe to stay listed, got %+v", list)
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	plan := service.Plan(models.RecipeRequest{RecipeID: "recipe.user.stale", InputPaths: []string{input}})
	if plan.Success {
		t.Fatalf("expected the plan to fail, got %+v", plan)
	}
	if plan.Error == nil || plan.Error.DetailCode != "RECIPE_BASE_NOT_FOUND" {
		t.Fatalf("expected RECIPE_BASE_NOT_FOUND, got %+v", plan.Error)
	}
	if !warningsContain([]string{plan.Message}, "no longer available") {
		t.Fatalf("expected an actionable message, got %q", plan.Message)
	}
}

func TestSaveUserRecipeRejectsBaseWithoutSchema(t *testing.T) {
	service, _, _ := newUserRecipeTestService(t, false)

	response := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Not allowed",
		BaseRecipeID: RecipeIDSocialClip,
		Options:      map[string]any{},
	})
	if response.Success {
		t.Fatalf("expected a no-schema base to be rejected, got %+v", response)
	}
	if response.Error == nil || response.Error.DetailCode != "RECIPE_NOT_DUPLICABLE" {
		t.Fatalf("expected RECIPE_NOT_DUPLICABLE, got %+v", response.Error)
	}
}

func TestUserRecipeWithUnknownStoredOptionFailsPlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user_recipes.json")
	payload := persistedUserRecipes{
		Version: userRecipesVersion,
		Recipes: []userRecipe{{
			ID:           "recipe.user.unknown.option",
			Name:         "Hand edited",
			BaseRecipeID: RecipeIDEmailPDF,
			Options:      map[string]any{"targetSizeMb": 5},
			CreatedAt:    1,
		}},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write store: %v", err)
	}

	service := NewService(registry.NewRegistry(), nil, nil)
	service.SetUserRecipesPath(path)

	plan := service.Plan(models.RecipeRequest{RecipeID: "recipe.user.unknown.option"})
	if plan.Success {
		t.Fatalf("expected plan to reject the unknown key, got %+v", plan)
	}
	if plan.Error == nil || plan.Error.DetailCode != "RECIPE_OPTION_UNKNOWN" {
		t.Fatalf("expected RECIPE_OPTION_UNKNOWN, got %+v", plan.Error)
	}
	if !warningsContain([]string{plan.Message}, "targetSizeBytes") {
		t.Fatalf("expected the supported keys in the message, got %q", plan.Message)
	}
}

func TestDeleteUserRecipeKeepsRunHistory(t *testing.T) {
	service, history, _ := newUserRecipeTestService(t, false)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Temporary email",
		BaseRecipeID: RecipeIDEmailPDF,
		Options:      map[string]any{emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte)},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the recipe to save, got %+v", saved)
	}

	run := service.Run(context.Background(), models.RecipeRequest{RecipeID: saved.Recipe.ID, InputPaths: []string{input}})
	if !run.Success {
		t.Fatalf("expected the run to be submitted, got %+v", run)
	}
	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}
	if entries := history.List(); len(entries) != 1 {
		t.Fatalf("expected one history entry after the run, got %+v", entries)
	}

	deleted := service.DeleteUserRecipe(saved.Recipe.ID)
	if !deleted.Success {
		t.Fatalf("expected the delete to succeed, got %+v", deleted)
	}
	if listed := findListedRecipe(service.List(), saved.Recipe.ID); listed != nil {
		t.Fatalf("expected the recipe to disappear, got %+v", listed)
	}
	if entries := history.List(); len(entries) != 1 || entries[0].JobID != run.JobID {
		t.Fatalf("expected run history to survive the delete, got %+v", entries)
	}
}

func TestRenameAndDeleteUserRecipe(t *testing.T) {
	service, _, _ := newUserRecipeTestService(t, false)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Before",
		BaseRecipeID: RecipeIDEmailPDF,
		Options:      map[string]any{emailPDFTargetOptionKey: 2 * int64(bytesPerMegabyte)},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the recipe to save, got %+v", saved)
	}

	renamed := service.RenameUserRecipe(saved.Recipe.ID, "After")
	if !renamed.Success || renamed.Recipe == nil || renamed.Recipe.Name != "After" {
		t.Fatalf("expected the rename to apply, got %+v", renamed)
	}
	if listed := findListedRecipe(service.List(), saved.Recipe.ID); listed == nil || listed.Name != "After" {
		t.Fatalf("expected the new name in the list, got %+v", listed)
	}

	if response := service.DeleteUserRecipe("recipe.user.missing"); response.Success {
		t.Fatalf("expected deleting a missing recipe to fail, got %+v", response)
	}
	if response := service.DeleteUserRecipe(saved.Recipe.ID); !response.Success {
		t.Fatalf("expected the delete to succeed, got %+v", response)
	}
}
