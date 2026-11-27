import { Component, EventEmitter, Input, Output, inject } from '@angular/core';

import { I18nService } from '../../services/i18n.service';
import type {
  RecipeOptionChoice,
  RecipeOptionField,
} from '../../services/wails';
import {
  optionDisplayBound,
  type RecipeOptionValue,
} from './recipe-option-values';

export interface RecipeOptionChange {
  field: RecipeOptionField;
  value: RecipeOptionValue;
}

/**
 * Renders a recipe option schema with one control per declared kind: integer
 * (unit and scale aware), boolean, enum and text. It is a dumb view: values,
 * errors and canonical conversion stay with the page so the duplicate dialog
 * and the run page share exactly one rendering mechanism.
 */
@Component({
  selector: 'app-recipe-option-fields',
  standalone: true,
  templateUrl: './recipe-option-fields.html',
  styleUrl: './recipe-option-fields.css',
})
export class RecipeOptionFields {
  readonly i18n = inject(I18nService);

  @Input() fields: RecipeOptionField[] = [];
  @Input() values: Record<string, RecipeOptionValue> = {};
  /** Localized messages keyed by field key; empty means valid. */
  @Input() errors: Record<string, string> = {};
  @Input() idPrefix = 'recipe-option';

  @Output() valueChange = new EventEmitter<RecipeOptionChange>();

  optionId(field: RecipeOptionField): string {
    return `${this.idPrefix}-${field.key}`;
  }

  optionText(field: RecipeOptionField): string {
    const value = this.values[field.key];
    return typeof value === 'boolean' ? '' : (value ?? '');
  }

  optionChecked(field: RecipeOptionField): boolean {
    return this.values[field.key] === true;
  }

  optionMin(field: RecipeOptionField): number | null {
    return optionDisplayBound(field, field.min);
  }

  optionMax(field: RecipeOptionField): number | null {
    return optionDisplayBound(field, field.max);
  }

  optionLabel(field: RecipeOptionField): string {
    return this.i18n.tOr(`recipeRun.option.${field.key}`, field.label);
  }

  optionHelp(field: RecipeOptionField): string {
    return this.i18n.tOr(
      `recipeRun.option.help.${field.key}`,
      field.help ?? '',
    );
  }

  optionChoiceLabel(choice: RecipeOptionChoice): string {
    return this.i18n.tOr(`recipeRun.choice.${choice.value}`, choice.label);
  }

  onTextInput(field: RecipeOptionField, event: Event): void {
    const target = event.target as HTMLInputElement | HTMLSelectElement | null;
    this.valueChange.emit({ field, value: target?.value ?? '' });
  }

  onBooleanChange(field: RecipeOptionField, event: Event): void {
    const target = event.target as HTMLInputElement | null;
    this.valueChange.emit({ field, value: target?.checked === true });
  }
}
