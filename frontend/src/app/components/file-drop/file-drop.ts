import { Component, ElementRef, EventEmitter, Input, Output, ViewChild } from '@angular/core';
import { CloudUpload, LucideAngularModule } from 'lucide-angular';

const MIME_LABELS: Record<string, string> = {
  '*/*': 'any file type',
  '*': 'any file type',
  'image/*': 'images',
  'video/*': 'videos',
  'audio/*': 'audio files',
  'application/pdf': 'PDF',
  'application/msword': 'DOC',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'DOCX',
  'text/plain': 'TXT',
  'text/markdown': 'MD',
  'application/json': 'JSON',
};

@Component({
  selector: 'app-file-drop',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './file-drop.html',
  styleUrl: './file-drop.css',
  host: {
    '[class.is-dragging]': 'dragOver',
  },
})
export class FileDrop {
  private static nextHintId = 0;

  @Input() accept = '';
  @Input() multiple = true;
  @Input() ariaLabel = 'File drop zone';
  /** Optional copy that replaces the derived supported-format hint. */
  @Input() hint = '';

  @Output() filesSelected = new EventEmitter<File[]>();

  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  readonly uploadIcon = CloudUpload;
  readonly hintId = `file-drop-hint-${(FileDrop.nextHintId += 1)}`;

  dragOver = false;

  /** Hint line shown under the subtitle; custom copy wins over the accept list. */
  displayHint(): string {
    const custom = this.hint.trim();
    if (custom) {
      return custom;
    }

    return `Accepts ${this.formatsHint()}`;
  }

  /** Readable, de-duplicated list derived from the native accept attribute. */
  formatsHint(): string {
    const parts = this.accept
      .split(',')
      .map((part) => part.trim())
      .filter(Boolean);

    if (parts.length === 0) {
      return 'any file type';
    }

    const labels: string[] = [];
    for (const part of parts) {
      const label = this.formatLabel(part);
      if (label && !labels.includes(label)) {
        labels.push(label);
      }
    }

    return labels.length > 0 ? labels.join(' · ') : 'any file type';
  }

  triggerFileInput(): void {
    this.fileInput?.nativeElement.click();
  }

  onEnter(event: Event): void {
    event.preventDefault();
    this.triggerFileInput();
  }

  onSpace(event: Event): void {
    event.preventDefault();
    this.triggerFileInput();
  }

  onFileInputChange(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const files = target?.files;
    if (!files || files.length === 0) return;
    this.emitFiles(files);
    if (target) target.value = '';
  }

  onDragEnter(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = true;
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = true;
  }

  onDragLeave(event: DragEvent): void {
    event.preventDefault();
    const host = event.currentTarget as HTMLElement | null;
    const related = event.relatedTarget as Node | null;
    // Moving over an inner element fires dragleave on the surface; keep the state.
    if (host && related && host.contains(related)) {
      return;
    }
    this.dragOver = false;
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = false;
    const files = event.dataTransfer?.files;
    if (!files || files.length === 0) return;
    this.emitFiles(files);
  }

  private formatLabel(token: string): string {
    const value = token.toLowerCase();
    const mapped = MIME_LABELS[value];
    if (mapped) {
      return mapped;
    }

    if (value.startsWith('.')) {
      return value.slice(1).toUpperCase();
    }

    if (value.includes('/')) {
      const subtype = value.split('/')[1] ?? '';
      return subtype ? subtype.toUpperCase() : token;
    }

    return token.toUpperCase();
  }

  private emitFiles(files: FileList): void {
    const arr: File[] = [];
    for (let i = 0; i < files.length; i += 1) {
      const f = files.item(i);
      if (f) arr.push(f);
    }
    this.filesSelected.emit(arr);
  }
}
