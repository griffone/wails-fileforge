import { DOCUMENT } from '@angular/common';
import {
  Injectable,
  Signal,
  WritableSignal,
  computed,
  inject,
  signal,
} from '@angular/core';

import type { Locale } from '../i18n';

/** localStorage key holding every persisted user default. */
export const PREFERENCES_STORAGE_KEY = 'ff-preferences';

interface PreferencesState {
  defaultOutputDir: string;
  whisperPath: string;
  whisperModelPath: string;
  language: Locale | '';
}

function emptyState(): PreferencesState {
  return {
    defaultOutputDir: '',
    whisperPath: '',
    whisperModelPath: '',
    language: '',
  };
}

function normalizePath(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
}

/** Only known locales survive; anything else means "use browser detection". */
function normalizeLocale(value: unknown): Locale | '' {
  return value === 'en' || value === 'es' ? value : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

/**
 * User defaults shared by the shell and every tool page.
 *
 * Values live in localStorage as a single JSON document. Reads and writes
 * tolerate missing, malformed or blocked storage: the in-memory signals keep
 * working even when persistence is unavailable.
 */
@Injectable({
  providedIn: 'root',
})
export class PreferencesService {
  private readonly document = inject(DOCUMENT);

  private readonly state: WritableSignal<PreferencesState>;

  /** Default folder seeded into new tool sessions; empty means next to the original. */
  readonly defaultOutputDir: Signal<string>;

  /** Whisper binary override; empty lets the backend resolve the installed default. */
  readonly whisperPath: Signal<string>;

  /** Whisper model override; empty lets the backend resolve the installed default. */
  readonly whisperModelPath: Signal<string>;

  /** Persisted interface language; empty means detect from the browser. */
  readonly language: Signal<Locale | ''>;

  constructor() {
    this.state = signal(this.load());
    this.defaultOutputDir = computed(() => this.state().defaultOutputDir);
    this.whisperPath = computed(() => this.state().whisperPath);
    this.whisperModelPath = computed(() => this.state().whisperModelPath);
    this.language = computed(() => this.state().language);
  }

  setDefaultOutputDir(path: string): void {
    this.update({
      ...this.state(),
      defaultOutputDir: normalizePath(path),
    });
  }

  clearDefaultOutputDir(): void {
    this.update({ ...this.state(), defaultOutputDir: '' });
  }

  setWhisperPath(path: string): void {
    this.update({ ...this.state(), whisperPath: normalizePath(path) });
  }

  setWhisperModelPath(path: string): void {
    this.update({ ...this.state(), whisperModelPath: normalizePath(path) });
  }

  setLanguage(locale: Locale): void {
    this.update({ ...this.state(), language: locale });
  }

  clearWhisperPaths(): void {
    this.update({ ...this.state(), whisperPath: '', whisperModelPath: '' });
  }

  private update(next: PreferencesState): void {
    this.state.set(next);
    this.persist(next);
  }

  private load(): PreferencesState {
    const raw = this.readStorage();
    if (!raw) {
      return emptyState();
    }

    try {
      const parsed: unknown = JSON.parse(raw);
      if (!isRecord(parsed)) {
        return emptyState();
      }

      return {
        defaultOutputDir: normalizePath(parsed['defaultOutputDir']),
        whisperPath: normalizePath(parsed['whisperPath']),
        whisperModelPath: normalizePath(parsed['whisperModelPath']),
        language: normalizeLocale(parsed['language']),
      };
    } catch {
      // Corrupted JSON is treated as an empty preference set.
      return emptyState();
    }
  }

  private persist(state: PreferencesState): void {
    try {
      this.document?.defaultView?.localStorage?.setItem(
        PREFERENCES_STORAGE_KEY,
        JSON.stringify({
          defaultOutputDir: state.defaultOutputDir,
          whisperPath: state.whisperPath,
          whisperModelPath: state.whisperModelPath,
          language: state.language,
        }),
      );
    } catch {
      // Storage can be unavailable; the in-memory preference still applies.
    }
  }

  private readStorage(): string | null {
    try {
      return (
        this.document?.defaultView?.localStorage?.getItem(
          PREFERENCES_STORAGE_KEY,
        ) ?? null
      );
    } catch {
      return null;
    }
  }
}
