import { DOCUMENT } from '@angular/common';
import { Injectable, Signal, WritableSignal, computed, inject, signal } from '@angular/core';

export type Theme = 'light' | 'dark';

/** localStorage key shared with the inline boot script in index.html. */
export const THEME_STORAGE_KEY = 'ff-theme';

const THEME_ATTRIBUTE = 'data-theme';
const LIGHT_SCHEME_QUERY = '(prefers-color-scheme: light)';

function isTheme(value: string | null | undefined): value is Theme {
  return value === 'light' || value === 'dark';
}

/**
 * Owns the active colour theme.
 *
 * The initial value comes from the attribute written by the inline script in
 * index.html, then from persisted storage, then from the OS preference. The
 * DOM attribute is kept in sync so CSS tokens switch without a re-render.
 */
@Injectable({
  providedIn: 'root',
})
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly themeState: WritableSignal<Theme>;

  readonly theme: Signal<Theme>;
  readonly isDark: Signal<boolean>;

  constructor() {
    const initial = this.resolveInitialTheme();
    this.themeState = signal<Theme>(initial);
    this.theme = this.themeState.asReadonly();
    this.isDark = computed(() => this.themeState() === 'dark');
    this.applyToDocument(initial);
  }

  set(theme: Theme): void {
    if (!isTheme(theme)) {
      return;
    }

    this.themeState.set(theme);
    this.applyToDocument(theme);
    this.persist(theme);
  }

  toggle(): void {
    this.set(this.themeState() === 'dark' ? 'light' : 'dark');
  }

  private resolveInitialTheme(): Theme {
    const attribute = this.rootElement()?.getAttribute(THEME_ATTRIBUTE);
    if (isTheme(attribute)) {
      return attribute;
    }

    const stored = this.readStoredTheme();
    if (isTheme(stored)) {
      return stored;
    }

    return this.prefersLightScheme() ? 'light' : 'dark';
  }

  private applyToDocument(theme: Theme): void {
    const root = this.rootElement();
    if (!root) {
      return;
    }

    root.dataset['theme'] = theme;
  }

  private persist(theme: Theme): void {
    try {
      this.document?.defaultView?.localStorage?.setItem(THEME_STORAGE_KEY, theme);
    } catch {
      // Storage can be unavailable (private mode, restricted webview): the
      // in-memory signal still drives the active theme.
    }
  }

  private readStoredTheme(): string | null {
    try {
      return this.document?.defaultView?.localStorage?.getItem(THEME_STORAGE_KEY) ?? null;
    } catch {
      return null;
    }
  }

  private prefersLightScheme(): boolean {
    try {
      const view = this.document?.defaultView;
      return view?.matchMedia?.(LIGHT_SCHEME_QUERY)?.matches === true;
    } catch {
      return false;
    }
  }

  private rootElement(): HTMLElement | null {
    return this.document?.documentElement ?? null;
  }
}
