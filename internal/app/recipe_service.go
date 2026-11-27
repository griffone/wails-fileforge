package app

import (
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/recipes"
)

// RecipeService exposes the built-in outcome recipes and their runs.
type RecipeService struct {
	service *recipes.Service
	runtime *Runtime
}

// ListRecipes lists the built-in outcome recipes. Email PDF is first.
func (s *RecipeService) ListRecipes() models.ListRecipesResponse {
	return models.ListRecipesResponse{
		Success: true,
		Message: "recipes listed successfully",
		Recipes: s.service.List(),
	}
}

// PlanRecipe validates a recipe request and resolves its final output
// without running any tool.
func (s *RecipeService) PlanRecipe(req models.RecipeRequest) models.RecipePlanResponse {
	return s.service.Plan(req)
}

// RunRecipe starts a recipe chain in the background and returns its job id.
func (s *RecipeService) RunRecipe(req models.RecipeRequest) models.RunRecipeResponse {
	return s.service.Run(s.runtime.ContextOrBackground(), req)
}

// GetRecipeStatus returns the per-step snapshot of a recipe run.
func (s *RecipeService) GetRecipeStatus(jobID string) models.RecipeStatusResponse {
	return s.service.Status(jobID)
}

// ListRecipeRuns returns every tracked recipe run, newest first. The result
// is always a slice so the frontend can render an empty activity list.
func (s *RecipeService) ListRecipeRuns(limit int) []models.RecipeResult {
	runs := s.service.ListRuns(limit)
	if runs == nil {
		return []models.RecipeResult{}
	}

	return runs
}

// CancelRecipe requests cancellation of a running recipe.
func (s *RecipeService) CancelRecipe(jobID string) models.RecipeStatusResponse {
	return s.service.Cancel(jobID)
}

// SaveUserRecipe creates or updates a duplicate-and-edit recipe. The overrides
// are validated against the base recipe schema before they are persisted.
func (s *RecipeService) SaveUserRecipe(req models.SaveUserRecipeRequest) models.UserRecipeResponse {
	return s.service.SaveUserRecipe(req)
}

// RenameUserRecipe changes the display name of a saved user recipe.
func (s *RecipeService) RenameUserRecipe(id, name string) models.UserRecipeResponse {
	return s.service.RenameUserRecipe(id, name)
}

// DeleteUserRecipe permanently removes a saved user recipe. Run history is a
// separate store and is unaffected.
func (s *RecipeService) DeleteUserRecipe(id string) models.UserRecipeResponse {
	return s.service.DeleteUserRecipe(id)
}

// ExportUserRecipes writes the selected user recipes to one shareable file.
func (s *RecipeService) ExportUserRecipes(path string, ids []string) models.ExportUserRecipesResponse {
	return s.service.ExportUserRecipes(path, ids)
}

// ImportUserRecipes validates and applies a shared recipe file, reporting
// imported, skipped and invalid entries without partially applying a bad one.
func (s *RecipeService) ImportUserRecipes(path string, overwrite bool) models.ImportUserRecipesResponse {
	return s.service.ImportUserRecipes(path, overwrite)
}
