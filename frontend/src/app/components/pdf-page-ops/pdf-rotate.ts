import { Component, computed, signal } from '@angular/core';
import {
  FolderOpen,
  LucideAngularModule,
  RotateCw,
  ShieldCheck,
  TriangleAlert,
  X,
} from 'lucide-angular';

import { FileDrop } from '../file-drop/file-drop';
import { JobCard } from '../job-card/job-card';
import { FileQueue } from '../tool-flow/file-queue';
import { OutputPlanCard } from '../tool-flow/output-plan-card';
import { ResultCard } from '../tool-flow/result-card';
import { PDF_ROTATE_COPY } from './pdf-page-ops-copy';
import {
  DEFAULT_ROTATE_ANGLE,
  PDF_ROTATE_ANGLES,
  defaultRotateOptions,
  normalizeRotateOptions,
} from './pdf-page-ops-config';
import type { RotateAngle } from './pdf-page-ops-config';
import { PdfPageOpsToolPage, pdfPageOpsDeps } from './pdf-page-ops-tool-page';
import { validatePageSelection } from './pdf-page-ops-validation';

const PDF_ROTATE_TOOL_ID = 'tool.pdf.rotate';

/**
 * Rotate all pages or a selection clockwise by 90, 180 or 270 degrees.
 *
 * The page field is optional: an empty selection means every page, exactly as
 * the backend treats it.
 */
@Component({
  selector: 'app-pdf-rotate',
  standalone: true,
  imports: [
    LucideAngularModule,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-rotate.html',
  styleUrl: './pdf-page-ops.css',
})
export class PdfRotate extends PdfPageOpsToolPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly rotateIcon = RotateCw;

  readonly angles = PDF_ROTATE_ANGLES;

  readonly pageSelection = signal('');
  readonly angle = signal<RotateAngle>(DEFAULT_ROTATE_ANGLE);

  private readonly selection = computed(() =>
    validatePageSelection(this.pageSelection(), this.knownPageCounts()),
  );

  readonly pageError = computed(() =>
    this.pageSelectionMessage(this.selection()),
  );

  constructor() {
    super(pdfPageOpsDeps(), {
      toolId: PDF_ROTATE_TOOL_ID,
      route: '/pdf-rotate',
      copy: PDF_ROTATE_COPY,
      verbPast: 'rotated',
      toolLabel: 'PDF',
      optionsSchemaVersion: 1,
    });
  }

  angleLabel(angle: RotateAngle): string {
    return this.i18n.tOr(`pdfRotate.angle.${angle}.label`, `${angle}°`);
  }

  angleHint(angle: RotateAngle): string {
    return this.i18n.tOr(`pdfRotate.angle.${angle}.hint`, '');
  }

  outputBadge(): string {
    const angle = this.i18n.t('pdfPageOps.badge.angle', {
      angle: this.angle(),
    });
    const pages =
      this.pageSelection().trim() === ''
        ? this.i18n.t('pdfRotate.badge.pagesAll')
        : this.i18n.t('pdfRotate.badge.pagesSelected', {
            pages: this.pageSelection().trim(),
          });

    return this.i18n.t('pdfPageOps.badge.summary', { value: angle, detail: pages });
  }

  selectAngle(angle: RotateAngle): void {
    if (angle === this.angle()) {
      return;
    }

    this.angle.set(angle);
    this.actionMessage.set('');
    this.refreshPlanForOptions();
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
    return this.pageError();
  }

  protected defaultOptions(): Record<string, unknown> {
    return { ...defaultRotateOptions() };
  }

  protected normalizeOptions(
    stored: Record<string, unknown>,
  ): Partial<Record<string, unknown>> {
    return normalizeRotateOptions(stored);
  }

  protected applyOptions(options: Record<string, unknown>): void {
    const restored = normalizeRotateOptions(options);
    this.pageSelection.set(restored.pageSelection ?? '');
    this.angle.set(restored.angle ?? DEFAULT_ROTATE_ANGLE);
  }

  protected currentOptions(): Record<string, unknown> {
    return {
      pageSelection: this.pageSelection().trim(),
      angle: this.angle(),
    };
  }

  protected buildOptions(): Record<string, unknown> {
    return {
      pageSelection: this.pageSelection().trim(),
      angle: this.angle(),
    };
  }
}
