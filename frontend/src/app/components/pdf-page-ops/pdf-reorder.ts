import { Component, computed, signal } from '@angular/core';
import {
  ArrowUpDown,
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
import { PDF_REORDER_COPY } from './pdf-page-ops-copy';
import {
  REORDER_REVERSE_KEYWORD,
  defaultReorderOptions,
  normalizeReorderOptions,
} from './pdf-page-ops-config';
import { PdfPageOpsToolPage, pdfPageOpsDeps } from './pdf-page-ops-tool-page';
import {
  formatPageList,
  validateReorderOrder,
} from './pdf-page-ops-validation';

const PDF_REORDER_TOOL_ID = 'tool.pdf.reorder';

/**
 * Rewrite the page order from an explicit spec such as `3,1,2,4-6`, or from
 * the `reverse` shortcut.
 *
 * The spec must list every page exactly once. The client mirrors the backend
 * grammar and coverage rules, names the missing or repeated pages, and never
 * sends an invalid spec: the form explains it instead.
 */
@Component({
  selector: 'app-pdf-reorder',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-reorder.html',
  styleUrl: './pdf-page-ops.css',
})
export class PdfReorder extends PdfPageOpsToolPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly reorderIcon = ArrowUpDown;
  readonly reverseKeyword = REORDER_REVERSE_KEYWORD;

  readonly order = signal('');

  private readonly validation = computed(() =>
    validateReorderOrder(this.order(), this.knownPageCounts()),
  );

  readonly orderError = computed(() =>
    this.order().trim() === ''
      ? ''
      : this.reorderMessage(this.validation()),
  );

  readonly missingPages = computed<number[]>(() => {
    const issue = this.validation().issue;
    return issue?.kind === 'coverage' ? issue.missing : [];
  });

  readonly duplicatePages = computed<number[]>(() => {
    const issue = this.validation().issue;
    return issue?.kind === 'coverage' ? issue.duplicates : [];
  });

  readonly missingList = computed(() => formatPageList(this.missingPages()));
  readonly duplicateList = computed(() =>
    formatPageList(this.duplicatePages()),
  );

  constructor() {
    super(pdfPageOpsDeps(), {
      toolId: PDF_REORDER_TOOL_ID,
      route: '/pdf-reorder',
      copy: PDF_REORDER_COPY,
      verbPast: 'reordered',
      toolLabel: 'PDF',
      optionsSchemaVersion: 1,
    });
  }

  outputBadge(): string {
    const order = this.order().trim();
    if (!order) {
      return this.i18n.t('pdfReorder.badge.empty');
    }

    return order.toLowerCase() === REORDER_REVERSE_KEYWORD
      ? this.i18n.t('pdfReorder.badge.reverse')
      : this.i18n.t('pdfReorder.badge.custom');
  }

  onOrderInput(event: Event): void {
    const value = (event.target as HTMLInputElement | null)?.value ?? '';
    this.order.set(value);
    this.actionMessage.set('');

    if (this.validation().issue) {
      this.clearPlanForOptions();
      return;
    }

    this.refreshPlanForOptions();
  }

  /** Fills the field with the backend's `reverse` shortcut. */
  useReverse(): void {
    if (this.order().toLowerCase() === REORDER_REVERSE_KEYWORD) {
      return;
    }

    this.order.set(REORDER_REVERSE_KEYWORD);
    this.actionMessage.set('');
    this.refreshPlanForOptions();
  }

  protected override toolBlocked(): boolean {
    return this.validation().issue !== null;
  }

  protected override extraHelper(): string {
    if (this.order().trim() === '') {
      return this.i18n.t('pdfReorder.helper.order');
    }

    return this.orderError();
  }

  protected defaultOptions(): Record<string, unknown> {
    return { ...defaultReorderOptions() };
  }

  protected normalizeOptions(
    stored: Record<string, unknown>,
  ): Partial<Record<string, unknown>> {
    return normalizeReorderOptions(stored);
  }

  protected applyOptions(options: Record<string, unknown>): void {
    const restored = normalizeReorderOptions(options);
    this.order.set(restored.order ?? '');
  }

  protected currentOptions(): Record<string, unknown> {
    return { order: this.order().trim() };
  }

  protected buildOptions(): Record<string, unknown> {
    return { order: this.order().trim() };
  }
}
