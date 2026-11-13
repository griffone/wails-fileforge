import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FolderOpen, LucideAngularModule, TriangleAlert } from 'lucide-angular';
import {
  PlannedOutputV1,
  PlanJobResponseV1,
} from '../../services/wails';
import { describeToolError, ToolErrorCopy } from '../../services/tool-error';
import { fileDir, fileName } from '../../utils/format';

const VISIBLE_OUTPUT_LIMIT = 4;

@Component({
  selector: 'app-output-plan-card',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './output-plan-card.html',
  styleUrl: './output-plan-card.css',
})
export class OutputPlanCard {
  @Input() plan: PlanJobResponseV1 | null = null;
  @Input() loading = false;
  @Input() destinationLabel = 'Next to the original';
  @Input() isDefaultDestination = true;

  @Output() changeDestination = new EventEmitter<void>();

  readonly folderIcon = FolderOpen;
  readonly alertIcon = TriangleAlert;

  visibleOutputs(): PlannedOutputV1[] {
    return (this.plan?.outputs ?? []).slice(0, VISIBLE_OUTPUT_LIMIT);
  }

  remainingCount(): number {
    const total = this.plan?.outputs?.length ?? 0;
    return Math.max(0, total - VISIBLE_OUTPUT_LIMIT);
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
    return this.plan?.error ? describeToolError(this.plan.error) : null;
  }

  outputName(output: PlannedOutputV1): string {
    return fileName(output.outputPath);
  }

  outputDirectory(output: PlannedOutputV1): string {
    return fileDir(output.outputPath);
  }
}
