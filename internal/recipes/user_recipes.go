package recipes

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/jsonstore"
)

const (
	// userRecipesFileName is the persisted user recipe catalog written next to
	// the run snapshot.
	userRecipesFileName = "user_recipes.json"

	// userRecipesVersion versions the on-disk payload so a future shape change
	// can migrate instead of guessing.
	userRecipesVersion = 1

	// userRecipeIDPrefix namespaces generated ids so they can never collide
	// with the curated recipe.* ids.
	userRecipeIDPrefix = "recipe.user."

	// maxUserRecipeNameLength keeps names usable in menus and cards.
	maxUserRecipeNameLength = 80
)

// userRecipe is the persisted form of a user-defined recipe: an override set
// applied on top of a built-in base recipe.
type userRecipe struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	BaseRecipeID string         `json:"baseRecipeId"`
	Options      map[string]any `json:"options"`
	CreatedAt    int64          `json:"createdAt"`
}

// persistedUserRecipes versions the on-disk user recipe catalog.
type persistedUserRecipes struct {
	Version int          `json:"version"`
	Recipes []userRecipe `json:"recipes"`
}

// DefaultUserRecipesPath returns the persisted user recipe location:
// os.UserConfigDir()/fileforge/user_recipes.json, with a repo-local fallback
// when the platform config directory is unavailable.
func DefaultUserRecipesPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(".", ".fileforge", userRecipesFileName)
	}

	return filepath.Join(configDir, "fileforge", userRecipesFileName)
}

// SetUserRecipesPath configures the user recipe store. It is called once at
// app wiring time. An empty path keeps user recipes in memory only, which is
// what headless tests want.
func (s *Service) SetUserRecipesPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}

	s.userMu.Lock()
	s.userPath = path
	s.userStore = jsonstore.New(path, jsonstore.Options{
		Debounce: jsonstore.DefaultDebounce,
		OnError: func(err error) {
			log.Printf("recipes.user.persistence.write.failed path=%s err=%v", path, err)
		},
	})
	s.userMu.Unlock()

	s.loadUserRecipes(path)
}

// loadUserRecipes reads the persisted catalog. A missing, corrupt or
// unknown-version file starts empty and is logged, so startup never fails.
func (s *Service) loadUserRecipes(path string) {
	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("recipes.user.persistence.load.failed path=%s err=%v", path, err)
		}
		return
	}
	if info.Size() == 0 {
		return
	}

	var persisted persistedUserRecipes
	if err := jsonstore.Load(path, &persisted); err != nil {
		log.Printf("recipes.user.persistence.load.failed path=%s err=%v", path, err)
		return
	}
	if persisted.Version != userRecipesVersion {
		log.Printf("recipes.user.persistence.unsupported.version path=%s version=%d", path, persisted.Version)
		return
	}

	recipes := make([]userRecipe, 0, len(persisted.Recipes))
	for _, recipe := range persisted.Recipes {
		normalized, ok := normalizeStoredUserRecipe(recipe)
		if !ok {
			log.Printf("recipes.user.persistence.skip.invalid id=%q base=%q", recipe.ID, recipe.BaseRecipeID)
			continue
		}
		recipes = append(recipes, normalized)
	}

	s.userMu.Lock()
	s.userRecipes = recipes
	s.userMu.Unlock()
}

// normalizeStoredUserRecipe trims the persisted fields and drops entries that
// cannot be addressed later.
func normalizeStoredUserRecipe(recipe userRecipe) (userRecipe, bool) {
	recipe.ID = strings.TrimSpace(recipe.ID)
	recipe.Name = strings.TrimSpace(recipe.Name)
	recipe.BaseRecipeID = strings.TrimSpace(recipe.BaseRecipeID)
	if recipe.ID == "" || recipe.Name == "" || recipe.BaseRecipeID == "" {
		return userRecipe{}, false
	}
	if recipe.Options == nil {
		recipe.Options = map[string]any{}
	}

	return recipe, true
}

// userRecipeByID returns a copy of one stored user recipe.
func (s *Service) userRecipeByID(id string) (userRecipe, bool) {
	trimmed := strings.TrimSpace(id)

	s.userMu.RLock()
	defer s.userMu.RUnlock()

	index := s.findUserRecipeIndexLocked(trimmed)
	if index < 0 {
		return userRecipe{}, false
	}

	return cloneUserRecipe(s.userRecipes[index]), true
}

func (s *Service) findUserRecipeIndexLocked(id string) int {
	for index, recipe := range s.userRecipes {
		if recipe.ID == id {
			return index
		}
	}

	return -1
}

// SaveUserRecipe creates or updates a user recipe after validating its
// overrides against the base recipe schema.
func (s *Service) SaveUserRecipe(req models.SaveUserRecipeRequest) models.UserRecipeResponse {
	name, nameErr := validateUserRecipeName(req.Name)
	if nameErr != nil {
		return userRecipeFailure(nameErr)
	}

	existingID := strings.TrimSpace(req.ID)
	var existing userRecipe
	if existingID != "" {
		stored, found := s.userRecipeByID(existingID)
		if !found {
			return userRecipeFailure(newRecipeError(
				"RECIPE_USER_NOT_FOUND",
				fmt.Sprintf("User recipe '%s' was not found.", existingID),
				map[string]any{"recipeId": existingID},
			))
		}
		existing = stored
	}

	baseID := strings.TrimSpace(req.BaseRecipeID)
	if existingID != "" {
		// Editing never moves a recipe to another base; the stored base wins.
		baseID = existing.BaseRecipeID
	}

	base, baseErr := s.resolveBaseRecipe(baseID)
	if baseErr != nil {
		return userRecipeFailure(baseErr)
	}
	if len(base.OptionSchema) == 0 {
		return userRecipeFailure(newRecipeError(
			"RECIPE_NOT_DUPLICABLE",
			fmt.Sprintf("Recipe '%s' has no editable options, so it cannot be saved as a user recipe.", base.Name),
			map[string]any{"baseRecipeId": base.ID},
		))
	}

	options, optionsErr := validateRecipeOptions(base, req.Options)
	if optionsErr != nil {
		return userRecipeFailure(optionsErr)
	}

	recipe := userRecipe{
		ID:           existingID,
		Name:         name,
		BaseRecipeID: base.ID,
		Options:      options,
		CreatedAt:    existing.CreatedAt,
	}
	if recipe.ID == "" {
		recipe.ID = newUserRecipeID()
	}
	if recipe.CreatedAt == 0 {
		recipe.CreatedAt = time.Now().UnixMilli()
	}

	if err := s.persistUserRecipe(recipe); err != nil {
		return userRecipeFailure(newRecipeError(
			"RECIPE_USER_SAVE_FAILED",
			fmt.Sprintf("Could not save the user recipe: %v", err),
			map[string]any{"recipeId": recipe.ID},
		))
	}

	return models.UserRecipeResponse{
		Success: true,
		Message: "user recipe saved",
		Recipe:  userRecipeModel(base, recipe),
	}
}

// RenameUserRecipe changes the display name of a stored user recipe.
func (s *Service) RenameUserRecipe(id, name string) models.UserRecipeResponse {
	trimmedID := strings.TrimSpace(id)
	recipe, found := s.userRecipeByID(trimmedID)
	if !found {
		return userRecipeFailure(newRecipeError(
			"RECIPE_USER_NOT_FOUND",
			fmt.Sprintf("User recipe '%s' was not found.", trimmedID),
			map[string]any{"recipeId": trimmedID},
		))
	}

	trimmedName, nameErr := validateUserRecipeName(name)
	if nameErr != nil {
		return userRecipeFailure(nameErr)
	}
	recipe.Name = trimmedName

	if err := s.persistUserRecipe(recipe); err != nil {
		return userRecipeFailure(newRecipeError(
			"RECIPE_USER_SAVE_FAILED",
			fmt.Sprintf("Could not rename the user recipe: %v", err),
			map[string]any{"recipeId": recipe.ID},
		))
	}

	base, _ := s.resolveBaseRecipe(recipe.BaseRecipeID)
	return models.UserRecipeResponse{
		Success: true,
		Message: "user recipe renamed",
		Recipe:  userRecipeModel(base, recipe),
	}
}

// DeleteUserRecipe permanently removes a stored user recipe. Run history is a
// separate store and is never touched here.
func (s *Service) DeleteUserRecipe(id string) models.UserRecipeResponse {
	trimmedID := strings.TrimSpace(id)
	if _, found := s.userRecipeByID(trimmedID); !found {
		return userRecipeFailure(newRecipeError(
			"RECIPE_USER_NOT_FOUND",
			fmt.Sprintf("User recipe '%s' was not found.", trimmedID),
			map[string]any{"recipeId": trimmedID},
		))
	}

	if err := s.deleteUserRecipeRecord(trimmedID); err != nil {
		return userRecipeFailure(newRecipeError(
			"RECIPE_USER_DELETE_FAILED",
			fmt.Sprintf("Could not delete the user recipe: %v", err),
			map[string]any{"recipeId": trimmedID},
		))
	}

	return models.UserRecipeResponse{
		Success: true,
		Message: "user recipe deleted",
	}
}

// persistUserRecipe inserts or updates a recipe and writes the catalog
// synchronously. The previous slice is restored when the write fails so memory
// and disk never disagree.
func (s *Service) persistUserRecipe(recipe userRecipe) error {
	s.userMu.Lock()
	defer s.userMu.Unlock()

	previous := append([]userRecipe(nil), s.userRecipes...)
	next := make([]userRecipe, 0, len(s.userRecipes)+1)
	for _, candidate := range s.userRecipes {
		if candidate.ID == recipe.ID {
			continue
		}
		next = append(next, candidate)
	}
	next = append(next, cloneUserRecipe(recipe))

	s.userRecipes = next
	if err := s.writeUserRecipesLocked(); err != nil {
		s.userRecipes = previous
		return err
	}

	return nil
}

// deleteUserRecipeRecord removes a recipe and writes the catalog
// synchronously. Deletion is permanent: nothing is kept as a tombstone.
func (s *Service) deleteUserRecipeRecord(id string) error {
	s.userMu.Lock()
	defer s.userMu.Unlock()

	previous := append([]userRecipe(nil), s.userRecipes...)
	next := make([]userRecipe, 0, len(s.userRecipes))
	for _, candidate := range s.userRecipes {
		if candidate.ID == id {
			continue
		}
		next = append(next, candidate)
	}

	s.userRecipes = next
	if err := s.writeUserRecipesLocked(); err != nil {
		s.userRecipes = previous
		return err
	}

	return nil
}

func (s *Service) writeUserRecipesLocked() error {
	if s.userStore == nil {
		return nil
	}

	payload, err := json.MarshalIndent(persistedUserRecipes{
		Version: userRecipesVersion,
		Recipes: s.userRecipes,
	}, "", "  ")
	if err != nil {
		return err
	}

	return s.userStore.SaveNow(payload)
}

// userRecipeModels returns every stored recipe as a public Recipe, newest
// first. A recipe whose base recipe disappeared is still listed with its base
// id; resolving it later fails with a clear error instead of crashing.
func (s *Service) userRecipeModels() []models.Recipe {
	s.userMu.RLock()
	recipes := append([]userRecipe(nil), s.userRecipes...)
	s.userMu.RUnlock()

	sort.SliceStable(recipes, func(i, j int) bool {
		if recipes[i].CreatedAt != recipes[j].CreatedAt {
			return recipes[i].CreatedAt > recipes[j].CreatedAt
		}
		return recipes[i].ID > recipes[j].ID
	})

	result := make([]models.Recipe, 0, len(recipes))
	for _, recipe := range recipes {
		base, _ := s.resolveBaseRecipe(recipe.BaseRecipeID)
		result = append(result, *userRecipeModel(base, recipe))
	}

	return result
}

// userRecipeModel clones the base metadata and applies the user identity and
// overrides. A missing base yields a minimal definition that still identifies
// the recipe so the UI can show it and plan can report the failure.
func userRecipeModel(base *models.Recipe, recipe userRecipe) *models.Recipe {
	if base == nil {
		return &models.Recipe{
			ID:              recipe.ID,
			Name:            recipe.Name,
			UserDefined:     true,
			BaseRecipeID:    recipe.BaseRecipeID,
			OptionOverrides: cloneOptions(recipe.Options),
			Duplicable:      false,
			Steps:           []models.RecipeStep{},
		}
	}

	model := *base
	model.ID = recipe.ID
	model.Name = recipe.Name
	model.UserDefined = true
	model.BaseRecipeID = recipe.BaseRecipeID
	model.OptionOverrides = cloneOptions(recipe.Options)
	model.Duplicable = len(base.OptionSchema) > 0

	return &model
}

// resolveBaseRecipe resolves a built-in recipe id or the base id of a stored
// user recipe. Only built-in definitions carry a runnable chain.
func (s *Service) resolveBaseRecipe(id string) (*models.Recipe, *models.JobError) {
	trimmed := strings.TrimSpace(id)

	if builtin := recipeByID(trimmed); builtin != nil {
		return builtin, nil
	}

	if stored, found := s.userRecipeByID(trimmed); found {
		if builtin := recipeByID(stored.BaseRecipeID); builtin != nil {
			return builtin, nil
		}
		return nil, newRecipeError(
			"RECIPE_BASE_NOT_FOUND",
			fmt.Sprintf("User recipe '%s' references base recipe '%s', which is no longer available. Delete or recreate it.", stored.Name, stored.BaseRecipeID),
			map[string]any{"recipeId": stored.ID, "baseRecipeId": stored.BaseRecipeID},
		)
	}

	return nil, newRecipeError(
		"RECIPE_BASE_NOT_FOUND",
		fmt.Sprintf("Base recipe '%s' was not found.", trimmed),
		map[string]any{"baseRecipeId": trimmed},
	)
}

// validateUserRecipeName trims and bounds the display name.
func validateUserRecipeName(raw string) (string, *models.JobError) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", newRecipeError("RECIPE_USER_NAME_REQUIRED", "Give the recipe a name before saving.", nil)
	}
	if len([]rune(name)) > maxUserRecipeNameLength {
		return "", newRecipeError(
			"RECIPE_USER_NAME_TOO_LONG",
			fmt.Sprintf("The recipe name must be %d characters or fewer.", maxUserRecipeNameLength),
			map[string]any{"maxLength": maxUserRecipeNameLength},
		)
	}

	return name, nil
}

// validateRecipeOptions checks every override against the declared schema and
// returns the canonical values (integer values stay whole numbers, enum values
// must be declared, unknown keys are rejected).
func validateRecipeOptions(recipe *models.Recipe, overrides map[string]any) (map[string]any, *models.JobError) {
	schema := make(map[string]models.RecipeOptionField, len(recipe.OptionSchema))
	for _, field := range recipe.OptionSchema {
		schema[field.Key] = field
	}

	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	normalized := make(map[string]any, len(overrides))
	for _, key := range keys {
		field, declared := schema[key]
		if !declared {
			return nil, newRecipeError(
				"RECIPE_OPTION_UNKNOWN",
				fmt.Sprintf("Option '%s' is not supported by recipe '%s'. Supported options: %s.", key, recipe.Name, optionKeyList(recipe.OptionSchema)),
				map[string]any{"option": key, "recipeId": recipe.ID},
			)
		}

		value, valueErr := validateRecipeOptionValue(field, overrides[key])
		if valueErr != nil {
			return nil, valueErr
		}
		normalized[key] = value
	}

	return normalized, nil
}

func validateRecipeOptionValue(field models.RecipeOptionField, raw any) (any, *models.JobError) {
	switch field.Kind {
	case models.RecipeOptionInteger:
		value, ok := numericOption(raw)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
			return nil, newRecipeError(
				"RECIPE_OPTION_INVALID_TYPE",
				fmt.Sprintf("Option '%s' must be a whole number%s.", field.Key, optionUnitSuffix(field)),
				map[string]any{"option": field.Key},
			)
		}
		if field.Min != nil && value < *field.Min {
			return nil, optionRangeError(field, *field.Min)
		}
		if field.Max != nil && value > *field.Max {
			return nil, optionRangeError(field, *field.Max)
		}
		return int64(value), nil

	case models.RecipeOptionBoolean:
		value, ok := raw.(bool)
		if !ok {
			return nil, newRecipeError(
				"RECIPE_OPTION_INVALID_TYPE",
				fmt.Sprintf("Option '%s' must be true or false.", field.Key),
				map[string]any{"option": field.Key},
			)
		}
		return value, nil

	case models.RecipeOptionEnum:
		value, ok := raw.(string)
		if !ok {
			return nil, newRecipeError(
				"RECIPE_OPTION_INVALID_VALUE",
				fmt.Sprintf("Option '%s' must be one of: %s.", field.Key, optionValueList(field.Values)),
				map[string]any{"option": field.Key},
			)
		}
		value = strings.TrimSpace(value)
		for _, choice := range field.Values {
			if choice.Value == value {
				return value, nil
			}
		}
		return nil, newRecipeError(
			"RECIPE_OPTION_INVALID_VALUE",
			fmt.Sprintf("Option '%s' must be one of: %s.", field.Key, optionValueList(field.Values)),
			map[string]any{"option": field.Key, "value": value},
		)

	case models.RecipeOptionText:
		value, ok := raw.(string)
		if !ok {
			return nil, newRecipeError(
				"RECIPE_OPTION_INVALID_TYPE",
				fmt.Sprintf("Option '%s' must be text.", field.Key),
				map[string]any{"option": field.Key},
			)
		}
		return strings.TrimSpace(value), nil

	default:
		return nil, newRecipeError(
			"RECIPE_INVALID_DEFINITION",
			fmt.Sprintf("Option '%s' declares an unsupported type.", field.Key),
			map[string]any{"option": field.Key},
		)
	}
}

func optionRangeError(field models.RecipeOptionField, bound float64) *models.JobError {
	message := fmt.Sprintf("Option '%s' is out of range", field.Key)
	if field.Min != nil {
		message = fmt.Sprintf("Option '%s' must be at least %s", field.Key, formatOptionBound(field, *field.Min))
	}
	if field.Max != nil {
		message = fmt.Sprintf("Option '%s' must be at most %s", field.Key, formatOptionBound(field, *field.Max))
	}
	if field.Min != nil && field.Max != nil {
		message = fmt.Sprintf("Option '%s' must be between %s and %s", field.Key, formatOptionBound(field, *field.Min), formatOptionBound(field, *field.Max))
	}

	return newRecipeError("RECIPE_OPTION_OUT_OF_RANGE", message+".", map[string]any{"option": field.Key, "bound": bound})
}

func formatOptionBound(field models.RecipeOptionField, value float64) string {
	display := value
	if field.Scale > 0 {
		display = value / field.Scale
	}

	formatted := fmt.Sprintf("%g", display)
	if strings.TrimSpace(field.Unit) == "" {
		return formatted
	}

	return formatted + " " + field.Unit
}

func optionUnitSuffix(field models.RecipeOptionField) string {
	if strings.TrimSpace(field.Unit) == "" {
		return ""
	}

	return " in " + field.Unit
}

func optionKeyList(schema []models.RecipeOptionField) string {
	if len(schema) == 0 {
		return "(none)"
	}

	keys := make([]string, 0, len(schema))
	for _, field := range schema {
		keys = append(keys, field.Key)
	}

	return strings.Join(keys, ", ")
}

func optionValueList(values []models.RecipeOptionChoice) string {
	if len(values) == 0 {
		return "(none)"
	}

	options := make([]string, 0, len(values))
	for _, choice := range values {
		options = append(options, choice.Value)
	}

	return strings.Join(options, ", ")
}

func newUserRecipeID() string {
	return fmt.Sprintf("%s%d", userRecipeIDPrefix, time.Now().UnixNano())
}

func cloneUserRecipe(recipe userRecipe) userRecipe {
	cloned := recipe
	cloned.Options = cloneOptions(recipe.Options)

	return cloned
}

func cloneOptions(options map[string]any) map[string]any {
	if options == nil {
		return nil
	}

	cloned := make(map[string]any, len(options))
	for key, value := range options {
		cloned[key] = value
	}

	return cloned
}

func userRecipeFailure(jobErr *models.JobError) models.UserRecipeResponse {
	message := "The user recipe could not be saved."
	if jobErr != nil && strings.TrimSpace(jobErr.Message) != "" {
		message = jobErr.Message
	}

	return models.UserRecipeResponse{
		Success: false,
		Message: message,
		Error:   jobErr,
	}
}
