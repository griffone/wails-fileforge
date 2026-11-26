import { InjectionToken, Injectable, inject } from '@angular/core';
import { Clipboard } from '@wailsio/runtime';

/**
 * The slice of the Wails runtime clipboard the app uses. Exposed as a token so
 * tests can drive the runtime success and failure paths without touching the
 * module namespace.
 */
export interface RuntimeClipboard {
  SetText(text: string): Promise<void>;
}

export const RUNTIME_CLIPBOARD = new InjectionToken<RuntimeClipboard>(
  'RUNTIME_CLIPBOARD',
  {
    providedIn: 'root',
    factory: () => Clipboard,
  },
);

/**
 * Copies text through the Wails runtime first.
 *
 * WebView2 and WebKitGTK only expose `navigator.clipboard` in a secure
 * context, and Wails serves assets from its own scheme, so the browser API is
 * only a fallback for environments without the runtime (for example the
 * Angular dev-server preview). Returns false when both paths fail so callers
 * can show a real error instead of a silent no-op.
 */
@Injectable({ providedIn: 'root' })
export class ClipboardService {
  private readonly runtime = inject(RUNTIME_CLIPBOARD);

  async writeText(text: string): Promise<boolean> {
    const value = typeof text === 'string' ? text : '';
    if (value.length === 0) {
      return false;
    }

    try {
      await this.runtime.SetText(value);
      return true;
    } catch {
      // No runtime: fall through to the browser API for preview builds.
    }

    try {
      if (
        typeof navigator === 'undefined' ||
        !navigator.clipboard?.writeText
      ) {
        return false;
      }

      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      return false;
    }
  }
}
