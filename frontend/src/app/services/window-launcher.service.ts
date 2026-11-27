import { Injectable, inject } from '@angular/core';

import { I18nService } from './i18n.service';
import { ToastService } from './toast.service';
import { ToolWindowResponse, Wails } from './wails';

/**
 * Opens one tool in its own window.
 *
 * Route validation and de-duplication live in the backend: asking twice for the
 * same route focuses the window that already exists. This service only adds the
 * user-facing failure toast, so every caller (page head, command palette) gets
 * the same behaviour.
 */
@Injectable({
  providedIn: 'root',
})
export class WindowLauncher {
  private readonly wails = inject(Wails);
  private readonly i18n = inject(I18nService);
  private readonly toast = inject(ToastService);

  async open(route: string): Promise<ToolWindowResponse> {
    const response = await this.wails.openToolWindow(route);

    if (!response.success) {
      this.toast.push({
        tone: 'danger',
        title: this.i18n.t('window.open.failedTitle'),
        body: this.i18n.t('window.open.failed', { message: response.message }),
      });
    }

    return response;
  }
}
