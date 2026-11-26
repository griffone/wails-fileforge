import { Component, computed, signal } from '@angular/core';
import {
  FileMinus,
  FolderOpen,
  LucideAngularModule,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { PDF_REMOVE_PAGES_COPY } from './pdf-page-ops-copy';
import {
  defaultPageSelectionOptions,
  normalizePageSelectionOptions,
} from './pdf-page-ops-config';
import { PdfPageOpsToolPage, pdfPageOpsDeps } from './pdf-page-ops-tool-page';
import { validatePageSelection } from './pdf-page-ops-validation';

const PDF_REMOVE_PAGES_TOOL_ID = 'tool.pdf.remove_pages';

/**
 * Delete selected pages and keep the rest.
 *
 * The selection is required, and a selection that covers every page of an
 * inspected PDF is refused here with the same reason the backend plan gives,
 * so an impossible removal never reaches the plan call.
 */
@Component({
  selector: 'app-pdf-remove-pages',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-remove-pages.html',
  styleUrl: './pdf-page-ops.css',
})
export class PdfRemovePages extends PdfPageOpsToolPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly removeIcon = FileMinus;

  readonly pageSelection = signal('');

  private readonly selection = computed(() =>
    validatePageSelection(this.pageSelection(), this.knownPageCounts(), {
      required: true,
      forbidAllPages: true,
    }),
  );

  readonly pageError = computed(() =>
    this.pageSelection().trim() === ''
      ? ''
      : this.pageSelectionMessage(this.selection()),
  );

  constructor() {
    super(pdfPageOpsDeps(), {
      toolId: PDF_REMOVE_PAGES_TOOL_ID,
      route: '/pdf-remove-pages',
      copy: PDF_REMOVE_PAGES_COPY,
      verbPast: 'with pages removed',
      toolLabel: 'PDF',
      optionsSchemaVersion: 1,
    });
  }

  outputBadge(): string {
    const pages = this.pageSelection().trim();
    return pages === ''
      ? this.i18n.t('pdfRemovePages.badge.none')
      : this.i18n.t('pdfRemovePages.badge.pages', { pages });
  }

  onPagesInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.pageSelection.set(value);
    this.actionMessage.set('');

    if (this.selection().issue) {
      this.clearPlanForOptions();
      return;
    }

    this.refreshPlanForOptions();
  }

  protected override toolBlocked(): boolean {
    return this.selection().issue !== null;
  }

  protected override extraHelper(): string {
    if (this.pageSelection().trim() === '') {
      return this.i18n.t('pdfRemovePages.helper.pages');
    }

    return this.pageError();
  }

  protected defaultOptions(): Record<string, unknown> {
    return { ...defaultPageSelectionOptions() };
  }

  protected normalizeOptions(
    stored: Record<string, unknown>,
  ): Partial<Record<string, unknown>> {
    return normalizePageSelectionOptions(stored);
  }

  protected applyOptions(options: Record<string, unknown>): void {
    const restored = normalizePageSelectionOptions(options);
    this.pageSelection.set(restored.pageSelection ?? '');
  }

  protected currentOptions(): Record<string, unknown> {
    return { pageSelection: this.pageSelection().trim() };
  }

  protected buildOptions(): Record<string, unknown> {
    return { pageSelection: this.pageSelection().trim() };
  }
}
