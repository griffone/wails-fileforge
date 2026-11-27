import { Component, computed, inject, signal } from '@angular/core';
import { NavigationEnd, Router } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { AppWindow, LucideAngularModule } from 'lucide-angular';
import { filter, map } from 'rxjs';

import { I18nService } from '../../services/i18n.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { WindowLauncher } from '../../services/window-launcher.service';

/**
 * "Open in new window" for a tool page.
 *
 * The component resolves the tool from the live route and renders nothing when
 * the route is not a tool page, so it can live in a page head without each page
 * deciding whether the action makes sense. Two windows of the same tool are
 * independent: each loads its own Angular instance, so nothing in this
 * component is shared between them.
 */
@Component({
  selector: 'app-open-window-action',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './open-window-action.html',
  styleUrl: './open-window-action.css',
})
export class OpenWindowAction {
  private readonly router = inject(Router);
  private readonly registry = inject(ToolRegistryService);
  private readonly launcher = inject(WindowLauncher);
  readonly i18n = inject(I18nService);

  readonly windowIcon = AppWindow;

  private readonly url = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      map((event) => event.urlAfterRedirects),
    ),
    { initialValue: this.router.url },
  );

  /** The current route when it is a tool page, otherwise null. */
  readonly tool = computed(() => {
    const tool = this.registry.findByRoute(this.url());

    return tool && this.registry.isTool(tool) ? tool : null;
  });

  readonly busy = signal(false);

  async open(): Promise<void> {
    const tool = this.tool();
    if (!tool || this.busy()) {
      return;
    }

    this.busy.set(true);
    try {
      await this.launcher.open(tool.route);
    } finally {
      this.busy.set(false);
    }
  }
}
