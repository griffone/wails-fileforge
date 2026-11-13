import { Component, EventEmitter, Input, Output } from '@angular/core';
import {
  ArrowDown,
  ArrowUp,
  File,
  Image,
  LucideAngularModule,
  Video,
  X,
} from 'lucide-angular';
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

@Component({
  selector: 'app-file-queue',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './file-queue.html',
  styleUrl: './file-queue.css',
})
export class FileQueue {
  @Input() items: FileQueueItem[] = [];
  @Input() emptyTitle = 'No files yet';
  @Input() emptyHint = 'Drop files above to get started.';
  /** Shows move-up/move-down controls for order-sensitive queues. */
  @Input() reorderable = false;

  @Output() remove = new EventEmitter<string>();
  @Output() moveUp = new EventEmitter<string>();
  @Output() moveDown = new EventEmitter<string>();

  readonly imageIcon = Image;
  readonly videoIcon = Video;
  readonly fileIcon = File;
  readonly removeIcon = X;
  readonly upIcon = ArrowUp;
  readonly downIcon = ArrowDown;

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
    return `Remove ${item.name}`;
  }
}
