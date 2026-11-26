import { Injectable, inject } from '@angular/core';

import { RECIPES, RECIPE_NAV, RECIPE_NAV_GROUP, type RecipeDefinition } from '../config/recipes';
import {
  HOME_TOOL,
  NAVIGATION,
  NAV_GROUPS,
  SYSTEM_TOOL,
  TOOLS,
  TOOL_CATEGORIES,
  type NavGroup,
  type ToolCategory,
  type ToolCategoryDefinition,
  type ToolDefinition,
  normalizeToolRoute,
} from '../config/tools';
import type { TranslationKey } from '../i18n';
import { I18nService } from './i18n.service';

/**
 * Read-only view over the workbench tool model.
 *
 * The definitions live in `config/tools.ts`; this service is the single entry
 * point used by the shell, the command palette, home and the recent-tools
 * history so every surface agrees on routes, labels and grouping.
 */
@Injectable({
  providedIn: 'root',
})
export class ToolRegistryService {
  private readonly i18n = inject(I18nService);

  /** The twenty-seven conversion tools, without Home or System Status. */
  readonly tools: readonly ToolDefinition[] = TOOLS;

  /** Home + twenty-seven tools + System Status + Automations + Duplicates + Settings (32 entries). */
  readonly navigation: readonly ToolDefinition[] = NAVIGATION;

  /** Navigation grouped for the sidebar rail, with Recipes after Workspace. */
  readonly navGroups: readonly NavGroup[] = NAV_GROUPS.flatMap((group) =>
    group.id === 'workspace' ? [group, RECIPE_NAV_GROUP] : [group],
  );

  /** The five curated recipes shaped as navigation destinations. */
  readonly recipeNav: readonly ToolDefinition[] = RECIPE_NAV;

  /** The curated recipes themselves, kept out of the tool registry. */
  readonly recipes: readonly RecipeDefinition[] = RECIPES;

  readonly categories: readonly ToolCategoryDefinition[] = TOOL_CATEGORIES;

  readonly home: ToolDefinition = HOME_TOOL;

  readonly system: ToolDefinition = SYSTEM_TOOL;

  /** Resolves any router URL to its definition, or null when unknown. */
  findByRoute(route: string | null | undefined): ToolDefinition | null {
    if (!route) {
      return null;
    }

    const target = normalizeToolRoute(route);
    return this.navigation.find((item) => item.route === target) ?? null;
  }

  /** Resolves a recipe by its route, or null when the route is not a recipe. */
  findRecipeByRoute(route: string | null | undefined): RecipeDefinition | null {
    if (!route) {
      return null;
    }

    const target = normalizeToolRoute(route);
    return this.recipes.find((recipe) => recipe.route === target) ?? null;
  }

  /** Resolves a definition by its stable id, or null when unknown. */
  findById(id: string): ToolDefinition | null {
    return this.navigation.find((item) => item.id === id) ?? null;
  }

  categoryLabel(id: ToolCategory): string {
    const fallback = this.categories.find((category) => category.id === id)?.label ?? '';
    return this.i18n.tOr(`categories.${id}`, fallback);
  }

  /** Translated group heading; recipes use their own category key. */
  groupLabel(group: NavGroup): string {
    return group.id === 'recipes'
      ? this.i18n.tOr('categories.recipes', group.label)
      : this.categoryLabel(group.id);
  }

  /** Shared label for the recipes group in search and browse surfaces. */
  recipesLabel(): string {
    return this.i18n.tOr('categories.recipes', 'Recipes');
  }

  toolName(tool: ToolDefinition): string {
    return this.i18n.tOr(`tools.${tool.id}.name`, tool.name);
  }

  navName(tool: ToolDefinition): string {
    return this.i18n.tOr(`tools.${tool.id}.navName`, tool.navName);
  }

  toolDescription(tool: ToolDefinition): string {
    return this.i18n.tOr(`tools.${tool.id}.description`, tool.description);
  }

  recipeName(recipe: RecipeDefinition): string {
    return this.i18n.tOr(`tools.${recipe.id}.name`, recipe.fallback.name);
  }

  recipeDescription(recipe: RecipeDefinition): string {
    return this.i18n.tOr(`tools.${recipe.id}.description`, recipe.fallback.description);
  }

  /** Localized input badge for a recipe card, e.g. `1+ PDFs`. */
  recipeInputLabel(recipe: RecipeDefinition): string {
    return this.i18n.tOr(
      `recipes.${this.recipeSlug(recipe)}.inputLabel`,
      recipe.inputLabel,
    );
  }

  /** Localized highlight list for a recipe card. */
  recipeHighlights(recipe: RecipeDefinition): string[] {
    return this.i18n.tList(
      `recipes.${this.recipeSlug(recipe)}.highlights` as TranslationKey,
      recipe.fallback.highlights ?? [],
    );
  }

  /** True when the definition is a real tool (not Home or System Status). */
  isTool(definition: ToolDefinition): boolean {
    return this.tools.some((tool) => tool.route === definition.route);
  }

  private recipeSlug(recipe: RecipeDefinition): string {
    return recipe.id.replace(/^recipe\./, '');
  }
}
