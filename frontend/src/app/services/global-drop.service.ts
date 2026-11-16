import { Injectable, signal } from '@angular/core';

/**
 * Shell-level state for the global drop overlay. The overlay is rendered once
 * by the app shell; any drop that is not owned by a local drop zone updates it.
 */
@Injectable({ providedIn: 'root' })
export class GlobalDropService {
  private readonly currentPaths = signal<string[]>([]);

  /** Files currently presented by the overlay. */
  readonly paths = this.currentPaths.asReadonly();

  /** Whether the overlay is visible. */
  readonly open = signal(false);

  /** Opens the overlay with a de-duplicated, trimmed file list. */
  show(paths: readonly string[]): void {
    const seen = new Set<string>();
    const cleaned: string[] = [];

    for (const raw of paths) {
      if (typeof raw !== 'string') {
        continue;
      }
      const path = raw.trim();
      if (!path || seen.has(path)) {
        continue;
      }
      seen.add(path);
      cleaned.push(path);
    }

    this.currentPaths.set(cleaned);
    this.open.set(true);
  }

  /** Hides the overlay but keeps the files for the next open. */
  close(): void {
    this.open.set(false);
  }

  /** Hides the overlay and forgets the files. */
  clear(): void {
    this.open.set(false);
    this.currentPaths.set([]);
  }
}
