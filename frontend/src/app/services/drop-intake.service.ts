import { Injectable, signal } from '@angular/core';

import { normalizeToolRoute } from '../config/tools';

interface PendingIntake {
  route: string;
  paths: string[];
}

/**
 * Hands the file paths from the global drop overlay to the tool page that is
 * about to render. Exactly one intake can be pending: choosing another tool in
 * the overlay replaces it.
 */
@Injectable({ providedIn: 'root' })
export class DropIntakeService {
  private readonly pending = signal<PendingIntake | null>(null);

  /** Queues paths for a tool route; blanks and duplicates are dropped. */
  queue(route: string, paths: readonly string[]): void {
    const target = normalizeToolRoute(route);
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

    if (cleaned.length === 0) {
      return;
    }

    this.pending.set({ route: target, paths: cleaned });
  }

  /**
   * Returns the queued paths and clears them when the route matches. Pages for
   * other routes keep the intake untouched so a late init cannot steal it.
   */
  take(route: string): string[] {
    const pending = this.pending();
    if (!pending) {
      return [];
    }

    if (pending.route !== normalizeToolRoute(route)) {
      return [];
    }

    this.pending.set(null);
    return [...pending.paths];
  }
}
