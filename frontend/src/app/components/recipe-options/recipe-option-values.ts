import type { TranslationKey } from '../../i18n';
import type { RecipeOptionField, RecipeOptionChoice } from '../../services/wails';

/**
 * One editable option value as shown to the user: integer fields hold the
 * display number (already divided by `scale`), boolean fields hold the checkbox
 * state and enum/text fields hold the raw string.
 */
export type RecipeOptionValue = string | boolean;

/** Canonical values sent in `RecipeRequest.Options`; integer keys keep bytes. */
export type RecipeOptionValues = Record<string, unknown>;

export interface RecipeOptionValidation {
  key: TranslationKey;
  params: Record<string, string | number>;
}

/** Scale converts canonical units to display units; 0/undefined means 1. */
export function optionScale(field: RecipeOptionField): number {
  return field.scale && field.scale > 0 ? field.scale : 1;
}

/** Converts a canonical bound (bytes) to the display unit shown to the user. */
export function optionDisplayBound(
  field: RecipeOptionField,
  bound: number | null | undefined,
): number | null {
  if (bound === null || bound === undefined) {
    return null;
  }

  return bound / optionScale(field);
}

/**
 * Display value for one field from a canonical value (schema default or saved
 * override). Integers are rendered in the declared unit, rounded to two
 * decimals like the duplicate dialog.
 */
export function optionInitialValue(
  field: RecipeOptionField,
  raw: unknown,
): RecipeOptionValue {
  if (field.kind === 'boolean') {
    return raw === true;
  }

  if (field.kind === 'integer') {
    const numeric = typeof raw === 'number' ? raw : Number(raw);
    if (!Number.isFinite(numeric)) {
      return '';
    }

    return String(Number((numeric / optionScale(field)).toFixed(2)));
  }

  return raw == null ? '' : String(raw);
}

/** Initial display values for a whole schema, optionally from saved overrides. */
export function optionInitialValues(
  fields: readonly RecipeOptionField[],
  overrides?: Record<string, unknown> | null,
): Record<string, RecipeOptionValue> {
  const values: Record<string, RecipeOptionValue> = {};
  for (const field of fields) {
    values[field.key] = optionInitialValue(
      field,
      overrides?.[field.key] ?? field.default,
    );
  }

  return values;
}

/**
 * Canonical value for one field. Integer display values are multiplied by the
 * scale and rounded to whole canonical units; an empty or non-finite integer
 * has no canonical value (null).
 */
export function optionCanonicalValue(
  field: RecipeOptionField,
  value: RecipeOptionValue | undefined,
): unknown {
  if (field.kind === 'boolean') {
    return value === true;
  }

  if (field.kind === 'integer') {
    const text = typeof value === 'string' ? value.trim() : '';
    if (!text) {
      return null;
    }
    const numeric = Number(text);
    if (!Number.isFinite(numeric)) {
      return null;
    }

    return Math.round(numeric * optionScale(field));
  }

  return String(value ?? '').trim();
}

/**
 * Request options for a declared schema. Only declared keys are emitted.
 * Booleans stay out of the payload unless they are true or the schema default
 * is true, so a default-off checkbox never sends an option the caller did not
 * choose.
 */
export function optionRequestValues(
  fields: readonly RecipeOptionField[],
  values: Record<string, RecipeOptionValue>,
): RecipeOptionValues {
  const options: RecipeOptionValues = {};
  for (const field of fields) {
    const value = values[field.key];

    if (field.kind === 'boolean') {
      const checked = typeof value === 'boolean' ? value : field.default === true;
      if (checked || field.default === true) {
        options[field.key] = checked;
      }
      continue;
    }

    const canonical = optionCanonicalValue(field, value);
    if (canonical === null) {
      continue;
    }
    options[field.key] = canonical;
  }

  return options;
}

/**
 * Validates one display value the same way the backend validates canonical
 * options: whole numbers inside the declared bounds, declared enum values and
 * non-empty text. Returns the copy key and params; the caller localizes it.
 */
export function optionValidationError(
  field: RecipeOptionField,
  value: RecipeOptionValue | undefined,
  label: string,
  choiceLabel?: (choice: RecipeOptionChoice) => string,
): RecipeOptionValidation | null {
  switch (field.kind) {
    case 'integer': {
      const text = typeof value === 'string' ? value.trim() : '';
      if (!text) {
        return {
          key: 'recipeRun.option.error.required',
          params: { label },
        };
      }

      const numeric = Number(text);
      if (!Number.isFinite(numeric) || !Number.isInteger(numeric)) {
        return {
          key: 'recipeRun.option.error.integer',
          params: { label, unit: unitSuffix(field.unit) },
        };
      }

      const minimum = optionDisplayBound(field, field.min);
      const maximum = optionDisplayBound(field, field.max);
      if (minimum !== null && numeric < minimum) {
        return {
          key: 'recipeRun.option.error.range',
          params: rangeParams(label, minimum, maximum, field.unit),
        };
      }
      if (maximum !== null && numeric > maximum) {
        return {
          key: 'recipeRun.option.error.range',
          params: rangeParams(label, minimum, maximum, field.unit),
        };
      }

      return null;
    }

    case 'enum': {
      const selected = String(value ?? '').trim();
      const allowed = (field.values ?? []).some(
        (choice) => choice.value === selected,
      );
      if (!allowed) {
        return {
          key: 'recipeRun.option.error.enum',
          params: {
            label,
            values: (field.values ?? [])
              .map((choice) => choiceLabel?.(choice) ?? choice.label)
              .join(', '),
          },
        };
      }

      return null;
    }

    case 'text': {
      return String(value ?? '').trim()
        ? null
        : { key: 'recipeRun.option.error.required', params: { label } };
    }

    default:
      return null;
  }
}

function unitSuffix(unit: string | undefined): string {
  return unit ? ` (${unit})` : '';
}

function rangeParams(
  label: string,
  minimum: number | null,
  maximum: number | null,
  unit: string | undefined,
): Record<string, string | number> {
  return {
    label,
    min: minimum !== null ? String(minimum) : '',
    max: maximum !== null ? String(maximum) : '',
    unit: unit ?? '',
  };
}
