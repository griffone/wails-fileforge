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
