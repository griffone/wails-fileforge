import { Injectable } from '@angular/core';

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
  /** The sixteen conversion tools, without Home or System Status. */
  readonly tools: readonly ToolDefinition[] = TOOLS;

  /** Home + sixteen tools + System Status + Automations + Settings (20 entries). */
  readonly navigation: readonly ToolDefinition[] = NAVIGATION;

  /** Navigation grouped for the sidebar rail. */
  readonly navGroups: readonly NavGroup[] = NAV_GROUPS;

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

  /** Resolves a definition by its stable id, or null when unknown. */
  findById(id: string): ToolDefinition | null {
    return this.navigation.find((item) => item.id === id) ?? null;
  }

  categoryLabel(id: ToolCategory): string {
    return this.categories.find((category) => category.id === id)?.label ?? '';
  }

  /** True when the definition is a real tool (not Home or System Status). */
  isTool(definition: ToolDefinition): boolean {
    return this.tools.some((tool) => tool.route === definition.route);
  }
}
