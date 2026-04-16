import { Component, EventEmitter, Input, Output, ViewChild, ElementRef } from '@angular/core';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'app-file-drop',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './file-drop.html',
  styleUrls: ['./file-drop.css'],
})
export class FileDrop {
  @Input() accept: string = '';
  @Input() multiple = true;
  @Input() ariaLabel = 'File drop zone';

  @Output() filesSelected = new EventEmitter<File[]>();

  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  dragOver = false;

  triggerFileInput(): void {
    this.fileInput?.nativeElement.click();
  }

  onFileInputChange(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const files = target?.files;
    if (!files || files.length === 0) return;
    this.emitFiles(files);
    if (target) target.value = '';
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = true;
  }

  onDragLeave(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = false;
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = false;
    const files = event.dataTransfer?.files;
    if (!files || files.length === 0) return;
    this.emitFiles(files);
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
