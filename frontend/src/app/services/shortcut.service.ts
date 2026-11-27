import { Injectable } from '@angular/core';

import {
  SHORTCUTS,
  ShortcutPlatform,
  detectShortcutPlatform,
  findShortcut,
  shouldIgnoreShortcutEvent,
} from '../config/shortcuts';

/**
 * A tool page that can honour the page-scope shortcuts.
 *
 * `ToolRunController` implements it, so every tool page gets the behaviour
 * without per-page wiring. Both methods answer whether the shortcut did
 * something; a false answer leaves the key untouched for the browser.
 */
export interface PageShortcutTarget {
  readonly toolId: string;
  requestOpenFiles(): boolean;
  requestRun(): boolean;
}

/**
 * Tracks the active tool page and dispatches page-scope shortcuts to it.
 *
 * Exactly one controller is active. Angular mounts the incoming route before
 * it destroys the outgoing one, so the newest registration is the page the user
 * just navigated to; registration order is a faithful proxy for focus, and an
 * outgoing page cannot steal the slot back because `unregisterPage` only clears
 * its own registration.
 */
@Injectable({ providedIn: 'root' })
export class ShortcutService {
  readonly definitions = SHORTCUTS;
  readonly platform: ShortcutPlatform = detectShortcutPlatform();

  private activePage: PageShortcutTarget | null = null;

  registerPage(target: PageShortcutTarget): void {
    this.activePage = target;
  }

  unregisterPage(target: PageShortcutTarget): void {
    if (this.activePage === target) {
      this.activePage = null;
    }
  }

  activePageTarget(): PageShortcutTarget | null {
    return this.activePage;
  }

  /** Runs a page-scope shortcut against the active page. */
  handlePageShortcut(
    event: KeyboardEvent,
    doc?: Document | null,
  ): boolean {
    const shortcut = findShortcut(event, this.platform, 'page');
    if (!shortcut || shouldIgnoreShortcutEvent(event, shortcut, doc)) {
      return false;
    }

    const page = this.activePage;
    if (!page) {
      return false;
    }

    let handled = false;
    if (shortcut.id === 'page.openFiles') {
      handled = page.requestOpenFiles();
    } else if (shortcut.id === 'page.run') {
      handled = page.requestRun();
    }

    if (handled) {
      event.preventDefault();
    }
    return handled;
  }
}
