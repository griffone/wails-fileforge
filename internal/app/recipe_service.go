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

// ListRecipesV1 lists the built-in outcome recipes. Email PDF is first.
func (s *RecipeService) ListRecipesV1() models.ListRecipesResponseV1 {
	return models.ListRecipesResponseV1{
		Success: true,
		Message: "recipes listed successfully",
		Recipes: s.service.List(),
	}
}

// PlanRecipeV1 validates a recipe request and resolves its final output
// without running any tool.
func (s *RecipeService) PlanRecipeV1(req models.RecipeRequestV1) models.RecipePlanResponseV1 {
	return s.service.Plan(req)
}

// RunRecipeV1 starts a recipe chain in the background and returns its job id.
func (s *RecipeService) RunRecipeV1(req models.RecipeRequestV1) models.RunRecipeResponseV1 {
	return s.service.Run(s.runtime.ContextOrBackground(), req)
}

// GetRecipeStatusV1 returns the per-step snapshot of a recipe run.
func (s *RecipeService) GetRecipeStatusV1(jobID string) models.RecipeStatusResponseV1 {
	return s.service.Status(jobID)
}

// ListRecipeRunsV1 returns every tracked recipe run, newest first. The result
// is always a slice so the frontend can render an empty activity list.
func (s *RecipeService) ListRecipeRunsV1(limit int) []models.RecipeResultV1 {
	runs := s.service.ListRuns(limit)
	if runs == nil {
		return []models.RecipeResultV1{}
	}

	return runs
}

// CancelRecipeV1 requests cancellation of a running recipe.
func (s *RecipeService) CancelRecipeV1(jobID string) models.RecipeStatusResponseV1 {
	return s.service.Cancel(jobID)
}
