import { CommonModule } from '@angular/common';
import { Component, EventEmitter, Input, Output } from '@angular/core';
import { FormGroup, ReactiveFormsModule } from '@angular/forms';
import { JobResultItemV1, JobResultV1 } from '../../services/wails';

export interface ExecutionPanelOption {
  value: string;
  label: string;
}

export type ExecutionPanelFieldType = 'text' | 'textarea' | 'select';

export interface ExecutionPanelField {
  controlName: string;
  label: string;
  type: ExecutionPanelFieldType;
  placeholder?: string;
  helpText?: string;
  options?: ExecutionPanelOption[];
  visibleModes?: string[];
}

@Component({
  selector: 'app-tool-execution-panel',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule],
  templateUrl: './tool-execution-panel.html',
  styleUrl: './tool-execution-panel.css',
})
export class ToolExecutionPanel {
  @Input() title = 'Tool Execution';
  @Input() subtitle = '';
  @Input() form: FormGroup = new FormGroup({});
  @Input() fields: ExecutionPanelField[] = [];
  @Input() currentMode = 'single';

  @Input() validationMessage = '';
  @Input() submitMessage = '';
  @Input() statusMessage = '';
  @Input() isSubmitting = false;
  @Input() isPolling = false;
  @Input() canCancel = false;
  @Input() jobResult: JobResultV1 | null = null;

  @Input() runButtonText = 'Run Job';
  @Input() submittingButtonText = 'Submitting...';

  @Output() validate = new EventEmitter<void>();
  @Output() run = new EventEmitter<void>();
  @Output() cancel = new EventEmitter<void>();

  shouldRender(field: ExecutionPanelField): boolean {
    if (!field.visibleModes || field.visibleModes.length === 0) {
      return true;
    }

    return field.visibleModes.includes(this.currentMode);
  }

  progressPercent(): number {
    if (!this.jobResult || this.jobResult.progress.total <= 0) {
      return 0;
    }

    return Math.round((this.jobResult.progress.current / this.jobResult.progress.total) * 100);
  }

  itemOutputs(item: JobResultItemV1): string[] {
    if (item.outputs && item.outputs.length > 0) {
      return item.outputs;
    }

    if (item.outputPath) {
      return [item.outputPath];
    }

    return [];
  }
}
