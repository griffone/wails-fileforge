import { DOCUMENT } from '@angular/common';
import {
  Injectable,
  Signal,
  WritableSignal,
  computed,
  inject,
  signal,
} from '@angular/core';

/** localStorage key holding every persisted user default. */
export const PREFERENCES_STORAGE_KEY = 'ff-preferences';

interface PreferencesState {
  defaultOutputDir: string;
}

function emptyState(): PreferencesState {
  return { defaultOutputDir: '' };
}

function normalizePath(value: unknown): string {
  return typeof value === 'string' ? value.trim() : '';
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

  constructor() {
    this.state = signal(this.load());
    this.defaultOutputDir = computed(() => this.state().defaultOutputDir);
  }

  setDefaultOutputDir(path: string): void {
    this.update({ defaultOutputDir: normalizePath(path) });
  }

  clearDefaultOutputDir(): void {
    this.update(emptyState());
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

      return { defaultOutputDir: normalizePath(parsed['defaultOutputDir']) };
    } catch {
      // Corrupted JSON is treated as an empty preference set.
      return emptyState();
    }
  }

  private persist(state: PreferencesState): void {
    try {
      this.document?.defaultView?.localStorage?.setItem(
        PREFERENCES_STORAGE_KEY,
        JSON.stringify({ defaultOutputDir: state.defaultOutputDir }),
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
