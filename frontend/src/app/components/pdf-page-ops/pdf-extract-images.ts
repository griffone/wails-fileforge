import { Component, computed, signal } from '@angular/core';
import {
  FolderOpen,
  Images,
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
import { OpenWindowAction } from '../open-window-action/open-window-action';
import { fileName, fileDir, formatCount } from '../../utils/format';
import { PDF_EXTRACT_IMAGES_COPY } from './pdf-page-ops-copy';
import {
  defaultPageSelectionOptions,
  normalizePageSelectionOptions,
} from './pdf-page-ops-config';
import { PdfPageOpsToolPage, pdfPageOpsDeps } from './pdf-page-ops-tool-page';
import { validatePageSelection } from './pdf-page-ops-validation';

const PDF_EXTRACT_IMAGES_TOOL_ID = 'tool.pdf.extract_images';

/** One successful item rendered as `folder + image count`. */
export interface ExtractedFolderRow {
  path: string;
  name: string;
  directory: string;
  countLabel: string;
}

/**
 * Extract every embedded image of the selected pages into a folder.
 *
 * This tool writes a folder, not a PDF: the plan card shows the `<stem>_images`
 * destination, and the result panel reports the folder plus the image count the
 * backend verified. The shared controller is told not to inspect the outputs,
 * because a folder has no file size and its images are already verified.
 */
@Component({
  selector: 'app-pdf-extract-images',
  standalone: true,
  imports: [
    LucideAngularModule,
    OpenWindowAction,
    FileDrop,
    FileQueue,
    OutputPlanCard,
    ResultCard,
    JobCard,
  ],
  templateUrl: './pdf-extract-images.html',
  styleUrl: './pdf-page-ops.css',
})
export class PdfExtractImages extends PdfPageOpsToolPage {
  readonly shieldIcon = ShieldCheck;
  readonly folderIcon = FolderOpen;
  readonly closeIcon = X;
  readonly alertIcon = TriangleAlert;
  readonly imagesIcon = Images;

  readonly pageSelection = signal('');

  private readonly selection = computed(() =>
    validatePageSelection(this.pageSelection(), this.knownPageCounts()),
  );

  readonly pageError = computed(() =>
    this.pageSelectionMessage(this.selection()),
  );

  readonly extractedFolders = computed<ExtractedFolderRow[]>(() => {
    const rows: ExtractedFolderRow[] = [];
    for (const item of this.terminalJob()?.items ?? []) {
      if (!item.success) {
        continue;
      }

      const path = item.outputPath?.trim() ?? '';
      if (!path) {
        continue;
      }

      const count =
        detailNumber(item.details, 'imageCount') ?? item.outputCount ?? 0;
      rows.push({
        path,
        name: fileName(path),
        directory: fileDir(path),
        countLabel: formatCount(
          count,
          this.i18n.t('common.imageOne'),
          this.i18n.t('common.imageMany'),
        ),
      });
    }
    return rows;
  });

  readonly totalExtractedLabel = computed(() => {
    const total = (this.terminalJob()?.items ?? []).reduce((sum, item) => {
      if (!item.success) {
        return sum;
      }
      return (
        sum + (detailNumber(item.details, 'imageCount') ?? item.outputCount ?? 0)
      );
    }, 0);

    return formatCount(
      total,
      this.i18n.t('common.imageOne'),
      this.i18n.t('common.imageMany'),
    );
  });

  constructor() {
    super(pdfPageOpsDeps(), {
      toolId: PDF_EXTRACT_IMAGES_TOOL_ID,
      route: '/pdf-extract-images',
      copy: PDF_EXTRACT_IMAGES_COPY,
      verbPast: 'extracted',
      toolLabel: 'file',
      optionsSchemaVersion: 1,
    });
  }

  outputBadge(): string {
    return this.i18n.t('pdfExtractImages.badge');
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

  /**
   * The output is a folder; `inspectFiles` is for files, so nothing is read
   * back here. The result panel renders the verified image count instead.
   */
  protected override collectOutputPaths(): string[] {
    return [];
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

/** Reads a finite number from an item's details map, or null. */
function detailNumber(
  details: Record<string, unknown> | null | undefined,
  key: string,
): number | null {
  const value = details?.[key];
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}
