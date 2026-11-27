import { Component, EventEmitter, Input, Output, inject, signal } from '@angular/core';
import {
  ArrowDown,
  ArrowUp,
  Copy,
  ExternalLink,
  File,
  FolderOpen,
  GripVertical,
  Image,
  LucideAngularModule,
  Video,
  X,
} from 'lucide-angular';
import { I18nService } from '../../services/i18n.service';
import { ClipboardService } from '../../services/clipboard.service';
import { Wails } from '../../services/wails';
import { fileExtension } from '../../utils/format';
import { ContextMenu } from '../context-menu/context-menu';
import { ContextMenuItem } from '../context-menu/context-menu.types';
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

type ActionTone = 'success' | 'danger';

interface ActionMessage {
  tone: ActionTone;
  text: string;
}

/**
 * Ordered file queue shared by every tool page.
 *
 * Native HTML5 drag reorders the queue when `reorderable` is set; the up/down
 * buttons stay as the keyboard-accessible path. The component never owns the
 * order: it emits the requested move and the page reorders its own state, so
 * per-file metadata keyed by path follows the file.
 *
 * Right-click on a row (or Shift+F10 / the context-menu key on the focused row)
 * opens the shared context menu with Open, Reveal, Copy path and Remove. Remove
 * reuses the existing `remove` output; Open and Reveal call the same Wails path
 * actions the pages use, and copy goes through ClipboardService. A failed copy
 * is reported inline with the same banner tone the duplicates page uses.
 */
@Component({
  selector: 'app-file-queue',
  standalone: true,
  imports: [LucideAngularModule, ContextMenu],
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

  private readonly clipboard = inject(ClipboardService);
  private readonly wails = inject(Wails);

  readonly i18n = inject(I18nService);

  readonly imageIcon = Image;
  readonly videoIcon = Video;
  readonly fileIcon = File;
  readonly removeIcon = X;
  readonly upIcon = ArrowUp;
  readonly downIcon = ArrowDown;
  readonly dragIcon = GripVertical;
  readonly openIcon = ExternalLink;
  readonly revealIcon = FolderOpen;
  readonly copyIcon = Copy;

  /** Path being dragged, so a row removed mid-drag cannot move another one. */
  readonly draggingPath = signal<string | null>(null);
  /** Insertion slot 0..items.length under the pointer, or null. */
  readonly dropIndex = signal<number | null>(null);

  /** Context-menu state; one menu instance serves every row. */
  readonly menuOpen = signal(false);
  readonly menuX = signal(0);
  readonly menuY = signal(0);
  readonly menuPath = signal('');
  readonly menuOpener = signal<HTMLElement | null>(null);
  /** Inline feedback for a failed open, reveal or copy. */
  readonly actionMessage = signal<ActionMessage | null>(null);

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

  /** True while `path` is still in the queue; a removed row disables actions. */
  private rowExists(path: string): boolean {
    return path.length > 0 && this.items.some((item) => item.path === path);
  }

  /** Items for the row currently targeted by the menu. */
  menuItems(): ContextMenuItem[] {
    const path = this.menuPath();
    const available = this.rowExists(path);

    return [
      {
        id: 'open',
        label: this.i18n.t('contextMenu.open'),
        icon: this.openIcon,
        disabled: !available,
      },
      {
        id: 'reveal',
        label: this.i18n.t('contextMenu.reveal'),
        icon: this.revealIcon,
        disabled: !available,
      },
      {
        id: 'copyPath',
        label: this.i18n.t('contextMenu.copyPath'),
        icon: this.copyIcon,
        disabled: !available,
      },
      {
        id: 'remove',
        label: this.i18n.t('contextMenu.remove'),
        icon: this.removeIcon,
        disabled: !available,
        danger: true,
      },
    ];
  }

  onRowContextMenu(event: MouseEvent, item: FileQueueItem): void {
    // Never let the browser menu, a row click or a page shortcut see this.
    event.preventDefault();
    event.stopPropagation();

    const row = event.currentTarget;
    if (row instanceof HTMLElement) {
      // Focus is not selection, but it keeps focus restore and Shift+F10
      // consistent after a mouse-driven open.
      row.focus();
    }

    this.openMenu(event.clientX, event.clientY, item, row);
  }

  onRowKeydown(event: KeyboardEvent, item: FileQueueItem): void {
    if (
      event.key !== 'ContextMenu' &&
      !(event.shiftKey && event.key === 'F10')
    ) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();

    const row = event.currentTarget;
    const rect =
      row instanceof HTMLElement ? row.getBoundingClientRect() : null;
    this.openMenu(rect ? rect.left : 0, rect ? rect.bottom : 0, item, row);
  }

  onMenuSelect(id: string): void {
    const path = this.menuPath();
    if (!this.rowExists(path)) {
      return;
    }

    switch (id) {
      case 'open':
        void this.runPathAction('open', path);
        return;
      case 'reveal':
        void this.runPathAction('reveal', path);
        return;
      case 'copyPath':
        void this.copyPath(path);
        return;
      case 'remove':
        this.remove.emit(path);
        return;
      default:
        return;
    }
  }

  onMenuClosed(): void {
    this.menuOpen.set(false);
    this.menuPath.set('');
    this.menuOpener.set(null);
  }

  private openMenu(
    x: number,
    y: number,
    item: FileQueueItem,
    opener: EventTarget | null,
  ): void {
    this.menuX.set(x);
    this.menuY.set(y);
    this.menuPath.set(item.path);
    this.menuOpener.set(opener instanceof HTMLElement ? opener : null);
    this.actionMessage.set(null);
    this.menuOpen.set(true);
  }

  private async runPathAction(
    action: 'open' | 'reveal',
    path: string,
  ): Promise<void> {
    let success = false;
    let message = '';

    try {
      const response =
        action === 'open'
          ? await this.wails.openPath(path)
          : await this.wails.revealInFolder(path);
      success = response.success;
      message = response.message ?? '';
    } catch {
      success = false;
    }

    if (!success) {
      this.actionMessage.set({
        tone: 'danger',
        text:
          message ||
          this.i18n.t(
            action === 'open'
              ? 'contextMenu.openFailed'
              : 'contextMenu.revealFailed',
          ),
      });
    }
  }

  private async copyPath(path: string): Promise<void> {
    const copied = await this.clipboard.writeText(path);
    this.actionMessage.set(
      copied
        ? { tone: 'success', text: this.i18n.t('contextMenu.copied') }
        : { tone: 'danger', text: this.i18n.t('contextMenu.copyFailed') },
    );
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
