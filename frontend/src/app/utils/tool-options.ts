import { PreferencesService } from '../services/preferences.service';

/**
 * Versioned, per-tool option memory.
 *
 * A tool page opts in by creating a store once and calling it in two places:
 *
 * ```ts
 * private readonly settings = createToolOptions(this.preferences, {
 *   toolId: TOOL_ID,
 *   schemaVersion: 1,
 *   defaults: () => ({ compress: true }),
 * });
 *
 * ngOnInit(): void { this.applyOptions(this.settings.restore()); }
 * // after a job is accepted:
 * this.settings.save(this.currentOptions());
 * ```
 *
 * `restore()` and `save()` never throw: a missing, corrupt or outdated payload
 * renders as the supplied defaults. `reset()` on a page should call
 * `restore()` again instead of assigning hard-coded defaults, so a completed
 * run does not wipe the remembered values while the form is still resettable.
 */
export interface ToolOptionsDefinition<T extends Record<string, unknown>> {
  /** Tool id; it becomes the suffix of the `ff-tool-options:` key. */
  toolId: string;
  /** Bump when the stored shape changes; older payloads fall back to defaults. */
  schemaVersion: number;
  /** Fresh defaults on every call, so nested objects are never shared. */
  defaults: () => T;
  /**
   * Maps a stored record onto known keys, coercing each value. Unknown keys
   * are already dropped by the preferences layer before this runs, and a
   * thrown error falls back to the defaults.
   */
  normalize?: (stored: Record<string, unknown>, defaults: T) => Partial<T>;
}

export interface ToolOptionsStore<T extends Record<string, unknown>> {
  /** Defaults merged with the remembered values (or plain defaults). */
  restore(): T;
  /** Persists the values under this tool's key and schema version. */
  save(values: T): void;
  /** Forgets the remembered values for this tool only. */
  clear(): void;
}

export function createToolOptions<T extends Record<string, unknown>>(
  preferences: PreferencesService,
  definition: ToolOptionsDefinition<T>,
): ToolOptionsStore<T> {
  return {
    restore(): T {
      const defaults = definition.defaults();
      const stored = preferences.readToolOptions(
        definition.toolId,
        definition.schemaVersion,
        Object.keys(defaults),
      );
      if (!stored) {
        return defaults;
      }

      let normalized: Partial<T>;
      try {
        normalized = definition.normalize
          ? definition.normalize(stored, defaults)
          : (stored as Partial<T>);
      } catch {
        return defaults;
      }

      const result = { ...defaults };
      for (const key of Object.keys(defaults) as Array<keyof T>) {
        const value = normalized[key];
        if (value !== undefined) {
          result[key] = value as T[keyof T];
        }
      }
      return result;
    },

    save(values: T): void {
      preferences.saveToolOptions(
        definition.toolId,
        definition.schemaVersion,
        values,
      );
    },

    clear(): void {
      preferences.clearToolOptions(definition.toolId);
    },
  };
}
