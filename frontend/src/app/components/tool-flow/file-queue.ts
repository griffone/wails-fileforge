import { Component, EventEmitter, Input, Output, inject, signal } from '@angular/core';
import {
  ArrowDown,
  ArrowUp,
  File,
  GripVertical,
  Image,
  LucideAngularModule,
  Video,
  X,
} from 'lucide-angular';
import { I18nService } from '../../services/i18n.service';
import { fileExtension } from '../../utils/format';
import { FileQueueItem } from './tool-flow.types';

const IMAGE_EXTENSIONS = new Set([
  'png',
  'jpg',
  'jpeg',
  'webp',
  'gif',
  'bmp',
  'tif',
  'tiff',
  'avif',
  'svg',
]);

const VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm', 'avi', 'm4v']);

/** Private MIME type so OS file drops never look like a queue reorder. */
const QUEUE_DRAG_TYPE = 'application/x-fileforge-queue-path';

/** Where the dragged item lands once it is removed from its old slot. */
export interface FileQueueReorder {
  path: string;
  /** Final zero-based index in the queue after the move. */
  to: number;
}

/**
 * Ordered file queue shared by every tool page.
 *
 * Native HTML5 drag reorders the queue when `reorderable` is set; the up/down
 * buttons stay as the keyboard-accessible path. The component never owns the
 * order: it emits the requested move and the page reorders its own state, so
 * per-file metadata keyed by path follows the file.
 */
@Component({
  selector: 'app-file-queue',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './file-queue.html',
  styleUrl: './file-queue.css',
})
export class FileQueue {
  @Input() items: FileQueueItem[] = [];
  /** Optional empty-state copy; falls back to the translated defaults. */
  @Input() emptyTitle = '';
  @Input() emptyHint = '';
  /** Shows move-up/move-down controls and enables drag reordering. */
  @Input() reorderable = false;

  @Output() remove = new EventEmitter<string>();
  @Output() moveUp = new EventEmitter<string>();
  @Output() moveDown = new EventEmitter<string>();
  @Output() reorder = new EventEmitter<FileQueueReorder>();

  readonly i18n = inject(I18nService);

  readonly imageIcon = Image;
  readonly videoIcon = Video;
  readonly fileIcon = File;
  readonly removeIcon = X;
  readonly upIcon = ArrowUp;
  readonly downIcon = ArrowDown;
  readonly dragIcon = GripVertical;

  /** Path being dragged, so a row removed mid-drag cannot move another one. */
  readonly draggingPath = signal<string | null>(null);
  /** Insertion slot 0..items.length under the pointer, or null. */
  readonly dropIndex = signal<number | null>(null);

  itemIcon(item: FileQueueItem) {
    const extension = fileExtension(item.name).toLowerCase();
    if (IMAGE_EXTENSIONS.has(extension)) {
      return this.imageIcon;
    }
    if (VIDEO_EXTENSIONS.has(extension)) {
      return this.videoIcon;
    }
    return this.fileIcon;
  }

  removeLabel(item: FileQueueItem): string {
    return this.i18n.t('fileQueue.remove', { name: item.name });
  }

  dragLabel(item: FileQueueItem): string {
    return this.i18n.t('fileQueue.dragHandle', { name: item.name });
  }

  onDragStart(event: DragEvent, item: FileQueueItem): void {
    if (!this.reorderable) {
      return;
    }

    const transfer = event.dataTransfer;
    if (!transfer) {
      return;
    }

    // Stop any row-level click/select handler from seeing the drag.
    event.stopPropagation();
    transfer.effectAllowed = 'move';
    const payload = JSON.stringify({ path: item.path });
    transfer.setData(QUEUE_DRAG_TYPE, payload);
    transfer.setData('text/plain', payload);
    this.draggingPath.set(item.path);
    this.dropIndex.set(null);
  }

  onDragEnd(): void {
    this.resetDragState();
  }

  onRowDragOver(event: DragEvent, index: number): void {
    if (!this.isQueueDrag(event)) {
      return;
    }

    event.preventDefault();
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move';
    }
    this.dropIndex.set(this.insertionIndex(event, index));
  }

  onListDragLeave(event: DragEvent): void {
    if (!this.draggingPath()) {
      return;
    }

    const list = event.currentTarget as HTMLElement | null;
    const related = event.relatedTarget as Node | null;
    if (list && related && list.contains(related)) {
      return;
    }
    this.dropIndex.set(null);
  }

  onRowDrop(event: DragEvent, index: number): void {
    if (!this.isQueueDrag(event)) {
      return;
    }

    event.preventDefault();
    const path = this.draggingPath();
    const insertAt = this.dropIndex() ?? this.insertionIndex(event, index);
    this.resetDragState();
    if (!path) {
      return;
    }

    // Resolve the source against the current items: a row may have been
    // removed while the drag was in flight.
    const from = this.items.findIndex((item) => item.path === path);
    if (from < 0) {
      return;
    }

    const lastIndex = this.items.length - 1;
    let target = Math.min(Math.max(insertAt, 0), this.items.length);
    if (target > from) {
      target -= 1;
    }
    target = Math.min(Math.max(target, 0), lastIndex);
    if (target === from) {
      return;
    }

    this.reorder.emit({ path, to: target });
  }

  /** True only for a drag started by this queue, never an OS file drag. */
  private isQueueDrag(event: DragEvent): boolean {
    const transfer = event.dataTransfer;
    return (
      !!this.draggingPath() &&
      !!transfer &&
      Array.from(transfer.types).includes(QUEUE_DRAG_TYPE)
    );
  }

  private insertionIndex(event: DragEvent, index: number): number {
    const row = event.currentTarget as HTMLElement | null;
    const rect = row?.getBoundingClientRect();
    if (!rect || rect.height === 0) {
      return index;
    }
    const after = event.clientY > rect.top + rect.height / 2;
    return after ? index + 1 : index;
  }

  private resetDragState(): void {
    this.draggingPath.set(null);
    this.dropIndex.set(null);
  }
}
