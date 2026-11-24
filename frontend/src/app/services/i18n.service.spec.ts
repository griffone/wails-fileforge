import { TestBed } from '@angular/core/testing';

import type { TranslationKey } from '../i18n';
import { I18nService } from './i18n.service';
import { PREFERENCES_STORAGE_KEY } from './preferences.service';

describe('I18nService', () => {
  function storedPreferences(): Record<string, unknown> {
    return JSON.parse(
      localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}',
    ) as Record<string, unknown>;
  }

  beforeEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    document.documentElement.lang = 'en';
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({});
  });

  afterEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    document.documentElement.lang = 'en';
  });

  it('starts in English and translates with the source dictionary', () => {
    const service = TestBed.inject(I18nService);

    expect(service.locale()).toBe('en');
    expect(service.t('settings.language.title')).toBe('Language');
  });

  it('switches to Spanish immediately, persists it and updates the document language', () => {
    const service = TestBed.inject(I18nService);

    service.setLocale('es');
    TestBed.tick();

    expect(service.locale()).toBe('es');
    expect(service.t('settings.language.title')).toBe('Idioma');
    expect(storedPreferences()['language']).toBe('es');
    expect(document.documentElement.lang).toBe('es');
  });

  it('switches back to English', () => {
    const service = TestBed.inject(I18nService);
    service.setLocale('es');
    TestBed.tick();

    service.setLocale('en');
    TestBed.tick();

    expect(service.locale()).toBe('en');
    expect(service.t('settings.language.title')).toBe('Language');
    expect(storedPreferences()['language']).toBe('en');
    expect(document.documentElement.lang).toBe('en');
  });

  it('toggles between both locales', () => {
    const service = TestBed.inject(I18nService);

    service.toggle();
    expect(service.locale()).toBe('es');

    service.toggle();
    expect(service.locale()).toBe('en');
  });

  it('interpolates named parameters in both languages', () => {
    const service = TestBed.inject(I18nService);

    expect(
      service.t('shell.locale.toggle', { language: 'Español' }),
    ).toBe('Switch language. Current: Español');

    service.setLocale('es');

    expect(
      service.t('shell.locale.toggle', { language: 'English' }),
    ).toBe('Cambiar idioma. Actual: English');
  });

  it('joins array translations with a middle dot through t()', () => {
    const service = TestBed.inject(I18nService);

    expect(service.t('recipes.email_pdf.highlights')).toBe(
      'Merge when needed · Target size · Verified + undo',
    );

    service.setLocale('es');

    expect(service.t('recipes.email_pdf.highlights')).toBe(
      'Unir si hace falta · Tamaño objetivo · Verificado + deshacer',
    );
  });

  it('joins array translations through tOr()', () => {
    const service = TestBed.inject(I18nService);

    service.setLocale('es');

    expect(service.tOr('recipes.web_image_set.highlights', 'Fallback')).toBe(
      'Sin ampliación · WebP + JPEG · ZIP + srcset',
    );
  });

  it('tList returns array translations as an independent copy', () => {
    const service = TestBed.inject(I18nService);

    const highlights = service.tList('recipes.clean_scan.highlights', ['Fallback']);

    expect(highlights).toEqual(['Local OCR', 'Searchable PDF', 'Text sidecar']);

    highlights.push('mutated');

    expect(service.tList('recipes.clean_scan.highlights', [])).toEqual([
      'Local OCR',
      'Searchable PDF',
      'Text sidecar',
    ]);
  });

  it('tList switches with the locale and keeps missing keys on English', () => {
    const service = TestBed.inject(I18nService);

    service.setLocale('es');

    expect(service.tList('recipes.clean_scan.highlights', [])).toEqual([
      'OCR local',
      'PDF buscable',
      'Texto adjunto',
    ]);

    service.setLocale('en');

    expect(
      service.tList('recipes.missing.highlights' as TranslationKey, ['Inline']),
    ).toEqual(['Inline']);
  });

  it('tList wraps string translations and copies the caller fallback', () => {
    const service = TestBed.inject(I18nService);

    expect(service.tList('home.footer.stay', ['Fallback'])).toEqual([
      'Your files stay on this machine.',
    ]);

    expect(
      service.tList('home.missing.key' as TranslationKey, ['Inline', 'Fallback']),
    ).toEqual(['Inline', 'Fallback']);
  });

  it('leaves unknown placeholders untouched', () => {
    const service = TestBed.inject(I18nService);

    expect(service.t('shell.locale.toggle', { other: 'x' })).toBe(
      'Switch language. Current: {language}',
    );
  });

  it('falls back to the raw key when a translation is missing', () => {
    const service = TestBed.inject(I18nService);
    const missing = 'shell.missing.key' as TranslationKey;

    service.setLocale('es');

    expect(service.t(missing)).toBe('shell.missing.key');
  });

  it('tOr resolves known keys through the Spanish dictionary', () => {
    const service = TestBed.inject(I18nService);

    service.setLocale('es');

    expect(service.tOr('tools.tool.pdf.merge.name', 'PDF Merge')).toBe('Unir PDF');
  });

  it('tOr resolves known keys through the English dictionary', () => {
    const service = TestBed.inject(I18nService);

    expect(service.tOr('tools.tool.pdf.merge.name', 'Fallback')).toBe('PDF Merge');
  });

  it('tOr falls back to the call-site English string when the key is missing', () => {
    const service = TestBed.inject(I18nService);

    service.setLocale('es');

    expect(service.tOr('tools.unknown.tool.name', 'English source label')).toBe(
      'English source label',
    );
  });

  it('tOr interpolates parameters', () => {
    const service = TestBed.inject(I18nService);

    service.setLocale('es');

    expect(service.tOr('palette.empty.title', 'No matches for “{query}”', { query: 'unir' })).toBe(
      'Sin resultados para «unir»',
    );
  });
});
