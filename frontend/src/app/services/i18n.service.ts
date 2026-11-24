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
import type { Dictionary, Locale, TranslationKey, TranslationParams } from '../i18n';
import { PreferencesService } from './preferences.service';

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
    const dictionary = this.dictionary();
    const template = dictionary[key] ?? en[key] ?? key;

    return this.interpolate(template, params);
  }

  /**
   * Translates an id-keyed overlay with an inline fallback.
   *
   * Used by config-driven surfaces (tools, recipes, categories) where the
   * canonical English string lives in `config/` and the translation key is
   * derived at runtime. Resolution order: active dictionary, English
   * dictionary, caller fallback.
   */
  tOr(key: string, fallback: string, params?: TranslationParams): string {
    const dictionary = this.dictionary();
    const template = dictionary[key] ?? (en as Dictionary)[key] ?? fallback;

    return this.interpolate(template, params);
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
