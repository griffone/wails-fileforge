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

/**
 * localStorage keys owned by this service.
 *
 * - `ff-preferences`: the global user defaults (this document).
 * - `ff-tool-options:<toolId>`: the versioned last-used options for one tool,
 *   stored as `{ schemaVersion, values }`. Every tool owns its own key so a
 *   malformed document can never invalidate another tool's settings.
 *
 * The last-submitted request for the "run again" action lives in
 * `LastRequestService` under `ff-last-request:<toolId>`; it is deliberately
 * kept out of the option payload because it carries inputs and destinations.
 */
export const PREFERENCES_STORAGE_KEY = 'ff-preferences';

/** Prefix of the namespaced, per-tool option documents. */
export const TOOL_OPTIONS_STORAGE_PREFIX = 'ff-tool-options:';

/** Coerces a tool id into its storage key; empty ids are not persisted. */
export function toolOptionsStorageKey(toolId: string): string {
  const trimmed = typeof toolId === 'string' ? toolId.trim() : '';
  return trimmed ? `${TOOL_OPTIONS_STORAGE_PREFIX}${trimmed}` : '';
}

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

  /**
   * Reads one tool's last-used options.
   *
   * Returns `null` when the document is missing, unreadable, malformed, belongs
   * to a different `schemaVersion`, or does not carry a values object. Keys
   * outside `allowedKeys` are dropped, so a stale payload can never inject
   * unexpected option names.
   */
  readToolOptions(
    toolId: string,
    schemaVersion: number,
    allowedKeys: readonly string[],
  ): Record<string, unknown> | null {
    const key = toolOptionsStorageKey(toolId);
    if (!key) {
      return null;
    }

    const raw = this.readKey(key);
    if (!raw) {
      return null;
    }

    try {
      const parsed: unknown = JSON.parse(raw);
      if (!isRecord(parsed) || parsed['schemaVersion'] !== schemaVersion) {
        return null;
      }

      const values = parsed['values'];
      if (!isRecord(values)) {
        return null;
      }

      const result: Record<string, unknown> = {};
      for (const allowed of allowedKeys) {
        if (Object.prototype.hasOwnProperty.call(values, allowed)) {
          result[allowed] = values[allowed];
        }
      }
      return result;
    } catch {
      // Corrupted JSON means "no remembered options"; defaults apply.
      return null;
    }
  }

  /** Writes one tool's last-used options under its own namespaced key. */
  saveToolOptions(
    toolId: string,
    schemaVersion: number,
    values: Record<string, unknown>,
  ): void {
    const key = toolOptionsStorageKey(toolId);
    if (!key) {
      return;
    }

    try {
      this.writeKey(
        key,
        JSON.stringify({ schemaVersion, values: { ...values } }),
      );
    } catch {
      // Storage can be unavailable or the payload unserializable.
    }
  }

  /** Forgets one tool's options without touching any other tool or default. */
  clearToolOptions(toolId: string): void {
    const key = toolOptionsStorageKey(toolId);
    if (!key) {
      return;
    }

    try {
      this.document?.defaultView?.localStorage?.removeItem(key);
    } catch {
      // Storage can be unavailable; there is nothing else to clear.
    }
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
      this.writeKey(
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

  private writeKey(key: string, value: string): void {
    this.document?.defaultView?.localStorage?.setItem(key, value);
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

  private readKey(key: string): string | null {
    try {
      return this.document?.defaultView?.localStorage?.getItem(key) ?? null;
    } catch {
      return null;
    }
  }
}
