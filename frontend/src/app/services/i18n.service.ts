import { DOCUMENT } from '@angular/common';
import {
  Injectable,
  Signal,
  WritableSignal,
  effect,
  inject,
  signal,
} from '@angular/core';

import { LOCALES, detectLocale, en, es } from '../i18n';
import type {
  Dictionary,
  DictionaryValue,
  Locale,
  TranslationKey,
  TranslationParams,
} from '../i18n';
import { PreferencesService } from './preferences.service';

/** Separator used when a translated array is rendered through `t()`/`tOr()`. */
const LIST_SEPARATOR = ' · ';

function isStringArray(value: DictionaryValue): value is readonly string[] {
  return Array.isArray(value);
}

/**
 * Runtime translation service.
 *
 * `t()` reads the locale signal, so every template that calls it re-renders
 * when the language changes. English is the source of truth: missing Spanish
 * entries fall back to English, then to the raw key.
 */
@Injectable({
  providedIn: 'root',
})
export class I18nService {
  private readonly preferences = inject(PreferencesService);
  private readonly document = inject(DOCUMENT);

  private readonly localeState: WritableSignal<Locale> = signal(
    this.preferences.language() || detectLocale(),
  );

  readonly locale: Signal<Locale> = this.localeState.asReadonly();
  readonly locales = LOCALES;

  constructor() {
    effect(() => {
      const root = this.document?.documentElement;
      if (root) {
        root.lang = this.localeState();
      }
    });
  }

  setLocale(locale: Locale): void {
    this.localeState.set(locale);
    this.preferences.setLanguage(locale);
  }

  toggle(): void {
    this.setLocale(this.localeState() === 'en' ? 'es' : 'en');
  }

  t(key: TranslationKey, params?: TranslationParams): string {
    const value = this.value(key);
    return this.interpolate(this.stringify(value), params);
  }

  /**
   * Translates an id-keyed overlay with an inline fallback.
   *
   * Used by config-driven surfaces (tools, recipes, categories) where the
   * canonical English string lives in `config/` and the translation key is
   * derived at runtime. Resolution order: active dictionary, English
   * dictionary, caller fallback. Array values are joined with ' · '.
   */
  tOr(key: string, fallback: string, params?: TranslationParams): string {
    const dictionary = this.dictionary();
    const value = dictionary[key] ?? (en as Dictionary)[key] ?? fallback;

    return this.interpolate(this.stringify(value), params);
  }

  /**
   * Resolves a key to a list of strings.
   *
   * Array values are copied, string values become a single-item list and
   * missing keys fall back to English, then to the caller fallback.
   */
  tList(key: TranslationKey, fallback: readonly string[]): string[] {
    const dictionary = this.dictionary();
    const value: DictionaryValue | undefined =
      dictionary[key] ?? (en as Dictionary)[key];

    if (value === undefined) {
      return [...fallback];
    }

    return isStringArray(value) ? [...value] : [value];
  }

  private value(key: TranslationKey): DictionaryValue {
    const dictionary = this.dictionary();
    return dictionary[key] ?? (en as Dictionary)[key] ?? key;
  }

  private stringify(value: DictionaryValue): string {
    return isStringArray(value) ? value.join(LIST_SEPARATOR) : value;
  }

  private dictionary(): Dictionary {
    return this.localeState() === 'es' ? es : en;
  }

  private interpolate(template: string, params?: TranslationParams): string {
    if (!params) {
      return template;
    }

    return template.replace(/\{(\w+)\}/g, (match: string, name: string) => {
      const value = params[name];
      return value === undefined ? match : String(value);
    });
  }
}
