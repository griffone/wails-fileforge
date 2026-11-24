import {
  Component,
  ElementRef,
  HostListener,
  Injector,
  ViewChild,
  afterNextRender,
  computed,
  effect,
  inject,
} from '@angular/core';
import { Router } from '@angular/router';
import { ArrowRight, CloudUpload, FileSearch, LucideAngularModule, X } from 'lucide-angular';

import type { ToolDefinition } from '../../config/tools';
import { DropIntakeService } from '../../services/drop-intake.service';
import { GlobalDropService } from '../../services/global-drop.service';
import { I18nService } from '../../services/i18n.service';
import { ToolRegistryService } from '../../services/tool-registry.service';
import { formatCount } from '../../utils/format';
import { recommendDropTools, summarizeDropFiles } from './drop-recommendations';

/**
 * Shell-level drop intake. Shown when files are dropped outside a local drop
 * zone; recommends the tools that can open them and forwards the paths to the
 * chosen tool page.
 */
@Component({
  selector: 'app-global-drop',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './global-drop.html',
  styleUrl: './global-drop.css',
})
export class GlobalDrop {
  private readonly drop = inject(GlobalDropService);
  private readonly intake = inject(DropIntakeService);
  private readonly registry = inject(ToolRegistryService);
  private readonly router = inject(Router);
  private readonly injector = inject(Injector);

  readonly i18n = inject(I18nService);

  readonly open = this.drop.open;
  readonly paths = this.drop.paths;

  readonly closeIcon = X;
  readonly dropIcon = CloudUpload;
  readonly emptyIcon = FileSearch;
  readonly arrowIcon = ArrowRight;

  readonly summary = computed(() => summarizeDropFiles(this.paths()));
  readonly recommendations = computed(() =>
    recommendDropTools(this.paths(), this.registry.tools),
  );

  readonly fileCountLabel = computed(() =>
    formatCount(
      this.summary().total,
      this.i18n.t('common.fileOne'),
      this.i18n.t('common.fileMany'),
    ),
  );

  readonly typeLabel = computed(
    () => this.summary().extensions.join(' · ') || this.i18n.t('globalDrop.noFiles'),
  );

  toolName(tool: ToolDefinition): string {
    return this.registry.toolName(tool);
  }

  toolDescription(tool: ToolDefinition): string {
    return this.registry.toolDescription(tool);
  }

  @ViewChild('panel') private panel?: ElementRef<HTMLElement>;

  constructor() {
    effect(() => {
      if (!this.open()) {
        return;
      }

      afterNextRender(
        () => this.panel?.nativeElement.focus(),
        { injector: this.injector },
      );
    });
  }

  choose(tool: ToolDefinition): void {
    const paths = this.paths();
    if (paths.length === 0) {
      return;
    }

    this.intake.queue(tool.route, paths);
    this.drop.clear();
    void this.router.navigateByUrl(tool.route);
  }

  close(): void {
    this.drop.close();
  }

  onBackdropClick(event: MouseEvent): void {
    if (event.target === event.currentTarget) {
      this.close();
    }
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.open() || event.key !== 'Escape') {
      return;
    }

    event.preventDefault();
    event.stopPropagation();
    this.close();
  }
}
