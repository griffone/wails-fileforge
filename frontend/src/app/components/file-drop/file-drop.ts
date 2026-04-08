import { Component, EventEmitter, Input, Output } from '@angular/core';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'app-file-drop',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './file-drop.html',
  styleUrls: ['./file-drop.css'],
})
export class FileDrop {
  @Input() accept = '';
  @Input() multiple = true;
  @Output() filePaths = new EventEmitter<string[]>();

  onFilesSelected(files: FileList | null): void {
    if (!files || files.length === 0) {
      return;
    }

    const paths: string[] = [];
    for (let i = 0; i < files.length; i++) {
      const file = files.item(i);
      if (!file) continue;
      // Electron/Wails may provide `path` on File; fallback to name
      const nativePath = (file as File & { path?: string }).path;
      paths.push(nativePath ?? file.name);
    }

    this.filePaths.emit(paths);
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    const files = event.dataTransfer?.files ?? null;
    this.onFilesSelected(files);
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
  }
}
