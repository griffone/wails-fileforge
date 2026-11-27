import type {
  RecipeOptionField,
  RecipeOptionKind,
} from '../../services/wails';
import {
  optionCanonicalValue,
  optionDisplayBound,
  optionInitialValues,
  optionRequestValues,
  optionValidationError,
} from './recipe-option-values';

const MEGABYTE = 1024 * 1024;

const targetField: RecipeOptionField = {
  key: 'targetSizeBytes',
  label: 'Target size',
  kind: 'integer' as RecipeOptionKind,
  unit: 'MB',
  min: 1 * MEGABYTE,
  max: 100 * MEGABYTE,
  scale: MEGABYTE,
  default: 10 * MEGABYTE,
};

const sidecarField: RecipeOptionField = {
  key: 'writeSidecar',
  label: 'SHA-256 sidecar',
  kind: 'boolean' as RecipeOptionKind,
  default: false,
};

const alwaysOnField: RecipeOptionField = {
  key: 'keepOriginal',
  label: 'Keep original',
  kind: 'boolean' as RecipeOptionKind,
  default: true,
};

const presetField: RecipeOptionField = {
  key: 'variantPreset',
  label: 'Variant preset',
  kind: 'enum' as RecipeOptionKind,
  default: 'standard',
  values: [
    { value: 'standard', label: '1600 / 800 px' },
    { value: 'compact', label: '1200 / 600 px' },
  ],
};

const noteField: RecipeOptionField = {
  key: 'note',
  label: 'Note',
  kind: 'text' as RecipeOptionKind,
  default: '',
};

describe('recipe option values', () => {
  it('renders a byte default in the declared unit and converts back to bytes', () => {
    expect(optionInitialValues([targetField])['targetSizeBytes']).toBe('10');
    expect(optionDisplayBound(targetField, targetField.min)).toBe(1);
    expect(optionDisplayBound(targetField, targetField.max)).toBe(100);
    expect(optionCanonicalValue(targetField, '5')).toBe(5 * MEGABYTE);
  });

  it('omits a default-off boolean until it is ticked', () => {
    expect(optionRequestValues([sidecarField], {})).toEqual({});
    expect(optionRequestValues([sidecarField], { writeSidecar: false })).toEqual(
      {},
    );
    expect(optionRequestValues([sidecarField], { writeSidecar: true })).toEqual({
      writeSidecar: true,
    });
  });

  it('sends false for a default-on boolean that the user turned off', () => {
    expect(
      optionRequestValues([alwaysOnField], { keepOriginal: false }),
    ).toEqual({ keepOriginal: false });
  });

  it('sends enum and text values and never an undeclared key', () => {
    expect(
      optionRequestValues(
        [presetField, noteField],
        {
          variantPreset: 'compact',
          note: 'keep',
          undeclared: 'nope',
        } as Record<string, string>,
      ),
    ).toEqual({ variantPreset: 'compact', note: 'keep' });
  });

  it('mirrors the backend validation for integers, enums and text', () => {
    expect(optionValidationError(targetField, '0', 'Target size')?.key).toBe(
      'recipeRun.option.error.range',
    );
    expect(
      optionValidationError(targetField, '2.5', 'Target size')?.key,
    ).toBe('recipeRun.option.error.integer');
    expect(
      optionValidationError(targetField, '', 'Target size')?.key,
    ).toBe('recipeRun.option.error.required');
    expect(
      optionValidationError(presetField, 'huge', 'Variant preset')?.key,
    ).toBe('recipeRun.option.error.enum');
    expect(optionValidationError(noteField, '  ', 'Note')?.key).toBe(
      'recipeRun.option.error.required',
    );
    expect(optionValidationError(noteField, 'text', 'Note')).toBeNull();
  });
});
