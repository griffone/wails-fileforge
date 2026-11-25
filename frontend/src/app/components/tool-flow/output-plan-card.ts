import { Component, EventEmitter, Input, Output, inject } from '@angular/core';
import {
  Folder,
  FolderOpen,
  LucideAngularModule,
  TriangleAlert,
} from 'lucide-angular';
import {
  PlannedOutput,
  PlanJobResponse,
} from '../../services/wails';
import { I18nService } from '../../services/i18n.service';
import { describeToolError, localizeToolError, ToolErrorCopy } from '../../services/tool-error';
import { fileDir, fileName, formatCount } from '../../utils/format';

const VISIBLE_OUTPUT_LIMIT = 4;

@Component({
  selector: 'app-output-plan-card',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './output-plan-card.html',
  styleUrl: './output-plan-card.css',
})
export class OutputPlanCard {
  @Input() plan: PlanJobResponse | null = null;
  @Input() loading = false;
  /** Optional destination copy; falls back to the translated default. */
  @Input() destinationLabel = '';
  @Input() isDefaultDestination = true;
  /** How many planned outputs stay listed before the `+ N more` row. */
  @Input() visibleLimit = VISIBLE_OUTPUT_LIMIT;
  /**
   * Base name derived from document metadata. Empty hides the row: pages
   * without a name field never pass it, and pages that do pass it only until
   * the user makes a choice.
   */
  @Input() suggestedName = '';

  @Output() changeDestination = new EventEmitter<void>();
  /** Emitted when the user accepts the suggested base name. */
  @Output() applySuggestedName = new EventEmitter<string>();

  readonly i18n = inject(I18nService);

  readonly folderIcon = FolderOpen;
  readonly outputFolderIcon = Folder;
  readonly alertIcon = TriangleAlert;

  visibleOutputs(): PlannedOutput[] {
    return (this.plan?.outputs ?? []).slice(0, this.visibleLimit);
  }

  remainingCount(): number {
    const total = this.plan?.outputs?.length ?? 0;
    return Math.max(0, total - this.visibleLimit);
  }

  hasAdjusted(): boolean {
    return (this.plan?.outputs ?? []).some((output) => output.adjusted);
  }

  planWarnings(): string[] {
    const warnings = [...(this.plan?.warnings ?? [])];
    for (const output of this.plan?.outputs ?? []) {
      for (const warning of output.warnings ?? []) {
        if (!warnings.includes(warning)) {
          warnings.push(warning);
        }
      }
    }
    return warnings;
  }

  errorCopy(): ToolErrorCopy | null {
    const error = this.plan?.error;
    if (!error) {
      return null;
    }
    return localizeToolError(
      (key) => this.i18n.t(key),
      describeToolError(error),
    );
  }

  isDirectory(output: PlannedOutput): boolean {
    return output.outputKind === 'directory';
  }

  /** Emits the suggestion only when it has content; empty means "hidden". */
  applySuggestion(): void {
    const name = this.suggestedName.trim();
    if (name) {
      this.applySuggestedName.emit(name);
    }
  }

  outputName(output: PlannedOutput): string {
    return fileName(output.outputPath);
  }

  outputDirectory(output: PlannedOutput): string {
    return fileDir(output.outputPath);
  }

  /** `N files` when the plan knows how many files the folder will contain. */
  outputCountLabel(output: PlannedOutput): string {
    const count = output.outputCount ?? 0;
    return count > 0
      ? formatCount(
          count,
          this.i18n.t('common.fileOne'),
          this.i18n.t('common.fileMany'),
        )
      : '';
  }

  /**
   * A single planned folder is the whole story, so it gets the primary line
   * instead of a one-item list that repeats the destination.
   */
  primaryDirectory(): PlannedOutput | null {
    const outputs = this.plan?.outputs ?? [];
    return outputs.length === 1 && this.isDirectory(outputs[0])
      ? outputs[0]
      : null;
  }

  listedOutputs(): PlannedOutput[] {
    return this.primaryDirectory() ? [] : this.visibleOutputs();
  }
}
