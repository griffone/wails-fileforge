import { TestBed } from '@angular/core/testing';

import { THEME_STORAGE_KEY, ThemeService } from './theme.service';

describe('ThemeService', () => {
  const storageKey = THEME_STORAGE_KEY;

  function resetThemeState(): void {
    try {
      localStorage.removeItem(storageKey);
    } catch {
      // Storage is available in Karma; guard keeps the spec resilient.
    }
    document.documentElement.removeAttribute('data-theme');
  }

  beforeEach(() => {
    resetThemeState();
    TestBed.configureTestingModule({});
  });

  afterEach(() => {
    resetThemeState();
  });

  it('applies an explicit theme to the document and persists it', () => {
    const service = TestBed.inject(ThemeService);

    service.set('light');

    expect(service.theme()).toBe('light');
    expect(service.isDark()).toBeFalse();
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(localStorage.getItem(storageKey)).toBe('light');
  });

  it('toggles between light and dark', () => {
    const service = TestBed.inject(ThemeService);
    service.set('dark');

    service.toggle();

    expect(service.theme()).toBe('light');
    expect(service.isDark()).toBeFalse();
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(localStorage.getItem(storageKey)).toBe('light');

    service.toggle();

    expect(service.theme()).toBe('dark');
    expect(service.isDark()).toBeTrue();
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(localStorage.getItem(storageKey)).toBe('dark');
  });

  it('adopts the theme written by the boot script', () => {
    document.documentElement.setAttribute('data-theme', 'light');

    const service = TestBed.inject(ThemeService);

    expect(service.theme()).toBe('light');
    expect(service.isDark()).toBeFalse();
  });

  it('falls back to the persisted theme when no attribute is present', () => {
    localStorage.setItem(storageKey, 'light');

    const service = TestBed.inject(ThemeService);

    expect(service.theme()).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });
});
