import { DOCUMENT } from '@angular/common';
import { Injectable, OnDestroy, Signal, WritableSignal, inject, signal } from '@angular/core';
import { NavigationEnd, Router } from '@angular/router';
import { Subscription, filter } from 'rxjs';

import { ToolDefinition } from '../config/tools';
import { ToolRegistryService } from './tool-registry.service';

const STORAGE_KEY = 'ff-recent-tools';
const MAX_RECENT = 4;

/**
 * Tracks the last tools the user opened, for the home page and the palette.
 *
 * Only routes are persisted; definitions are resolved through the registry on
 * read so stale or renamed entries cannot break the list.
 */
@Injectable({
  providedIn: 'root',
})
export class RecentToolsService implements OnDestroy {
  private readonly router = inject(Router);
  private readonly registry = inject(ToolRegistryService);
  private readonly document = inject(DOCUMENT);

  private readonly recentState: WritableSignal<readonly ToolDefinition[]>;
  readonly recent: Signal<readonly ToolDefinition[]>;

  private readonly navigationSubscription: Subscription;

  constructor() {
    this.recentState = signal<readonly ToolDefinition[]>(this.loadRecent());
    this.recent = this.recentState.asReadonly();
    this.navigationSubscription = this.router.events
      .pipe(filter((event): event is NavigationEnd => event instanceof NavigationEnd))
      .subscribe((event) => this.record(event.urlAfterRedirects));
  }

  ngOnDestroy(): void {
    this.navigationSubscription.unsubscribe();
  }

  /** Records a visited route, ignoring Home, System Status and unknown routes. */
  record(route: string): void {
    const tool = this.registry.findByRoute(route);
    if (!tool || !this.registry.isTool(tool)) {
      return;
    }

    const current = this.recentState();
    if (current.length > 0 && current[0].route === tool.route) {
      return;
    }

    const next = [tool, ...current.filter((item) => item.route !== tool.route)].slice(0, MAX_RECENT);
    this.recentState.set(next);
    this.persist(next);
  }

  private persist(tools: readonly ToolDefinition[]): void {
    try {
      const routes = tools.map((tool) => tool.route);
      this.document?.defaultView?.localStorage?.setItem(STORAGE_KEY, JSON.stringify(routes));
    } catch {
      // Storage can be unavailable; the in-memory history still works.
    }
  }

  private loadRecent(): readonly ToolDefinition[] {
    const raw = this.readStorage();
    if (!raw) {
      return [];
    }

    try {
      const parsed: unknown = JSON.parse(raw);
      if (!Array.isArray(parsed)) {
        return [];
      }

      const resolved: ToolDefinition[] = [];
      for (const value of parsed) {
        if (typeof value !== 'string') {
          continue;
        }

        const tool = this.registry.findByRoute(value);
        if (!tool || !this.registry.isTool(tool)) {
          continue;
        }

        if (resolved.some((item) => item.route === tool.route)) {
          continue;
        }

        resolved.push(tool);
        if (resolved.length === MAX_RECENT) {
          break;
        }
      }

      return resolved;
    } catch {
      // Corrupted JSON is treated as an empty history.
      return [];
    }
  }

  private readStorage(): string | null {
    try {
      return this.document?.defaultView?.localStorage?.getItem(STORAGE_KEY) ?? null;
    } catch {
      return null;
    }
  }
}
