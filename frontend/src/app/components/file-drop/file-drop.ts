import {
  Component,
  ElementRef,
  EventEmitter,
  inject,
  Input,
  NgZone,
  OnDestroy,
  OnInit,
  Output,
  ViewChild,
} from '@angular/core';
import { Events } from '@wailsio/runtime';
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

interface NativeDropPayload {
  droppedFiles?: unknown;
  files?: unknown;
  dropTargetDetails?: { id?: unknown } | null;
}

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
export class FileDrop implements OnInit, OnDestroy {
  private static nextHintId = 0;
  private static nextDropId = 0;

  @Input() accept = '';
  @Input() multiple = true;
  @Input() ariaLabel = 'File drop zone';
  /** Optional headline; defaults to "Drop files here". */
  @Input() title = 'Drop files here';
  /** Optional copy that replaces the derived supported-format hint. */
  @Input() hint = '';
  /** When true, clicking asks the host to open a native file dialog instead. */
  @Input() nativePicker = false;

  @Output() filesSelected = new EventEmitter<File[]>();
  /** Emitted when nativePicker is enabled and the user clicks the drop zone. */
  @Output() browseRequested = new EventEmitter<void>();
  /** Native OS drops (Wails) and browser drops with a resolvable path. */
  @Output() pathsSelected = new EventEmitter<string[]>();

  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  readonly uploadIcon = CloudUpload;
  readonly hintId = `file-drop-hint-${(FileDrop.nextHintId += 1)}`;
  readonly dropId = `ff-drop-${(FileDrop.nextDropId += 1)}`;

  dragOver = false;

  private readonly zone = inject(NgZone);
  private nativeDropUnsubscribe: (() => void) | null = null;

  ngOnInit(): void {
    try {
      this.nativeDropUnsubscribe = Events.On(
        'common:WindowFilesDropped',
        (event: unknown) =>
          this.zone.run(() => this.handleNativeDrop(event)),
      );
    } catch {
      this.nativeDropUnsubscribe = null;
    }
  }

  ngOnDestroy(): void {
    try {
      this.nativeDropUnsubscribe?.();
    } catch {
      // ignore
    }
    this.nativeDropUnsubscribe = null;
  }

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
    if (this.nativePicker) {
      this.browseRequested.emit();
      return;
    }

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
    this.emitPaths(files);
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
    this.emitPaths(files);
  }

  /**
   * Handles the Wails `common:WindowFilesDropped` event. The webview only
   * reports real OS paths for native drops, which is what the backend needs.
   */
  handleNativeDrop(event: unknown): void {
    const eventData = (event as { data?: unknown } | null)?.data ?? event;
    if (!eventData || typeof eventData !== 'object') {
      return;
    }

    const data = eventData as NativeDropPayload;
    const rawFiles = data.droppedFiles ?? data.files;
    if (!Array.isArray(rawFiles)) {
      return;
    }

    const target = data.dropTargetDetails;
    const targetId = typeof target?.id === 'string' ? target.id : '';
    if (targetId && targetId !== this.dropId) {
      return;
    }

    const paths = rawFiles.filter(
      (path): path is string =>
        typeof path === 'string' && path.trim().length > 0,
    );
    if (paths.length === 0) {
      return;
    }

    this.dragOver = false;
    this.pathsSelected.emit(paths);
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

  /**
   * In the Wails webview some File objects carry an absolute `path`, which is
   * what the backend needs. Plain browser drops have no path, so this emits
   * nothing and `filesSelected` remains the only signal.
   */
  private emitPaths(files: FileList): void {
    const paths: string[] = [];
    for (let i = 0; i < files.length; i += 1) {
      const file = files.item(i) as (File & { path?: string }) | null;
      if (file && typeof file.path === 'string' && file.path.trim()) {
        paths.push(file.path);
      }
    }
    if (paths.length > 0) {
      this.pathsSelected.emit(paths);
    }
  }
}
