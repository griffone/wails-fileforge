import { Component, OnInit, ElementRef, ViewChild } from '@angular/core';
import { CommonModule } from '@angular/common';
import {
  FormBuilder,
  FormGroup,
  Validators,
  ReactiveFormsModule,
} from '@angular/forms';
import { RouterModule } from '@angular/router';
import {
  LucideAngularModule,
  Upload,
  Download,
  CheckCircle,
  XCircle,
} from 'lucide-angular';
import {
  Wails,
  ConversionRequest,
  ConversionResult,
} from '../../services/wails';

@Component({
  selector: 'app-image-converter',
  standalone: true,
  imports: [
    CommonModule,
    ReactiveFormsModule,
    RouterModule,
    LucideAngularModule,
  ],
  templateUrl: './image-converter.html',
  styleUrls: ['./image-converter.css'],
})
export class ImageConverter implements OnInit {
  @ViewChild('fileInput') fileInput!: ElementRef<HTMLInputElement>;

  readonly UploadIcon = Upload;
  readonly DownloadIcon = Download;
  readonly CheckCircleIcon = CheckCircle;
  readonly XCircleIcon = XCircle;

  conversionForm: FormGroup;
  isConverting = false;
  result: ConversionResult | null = null;
  supportedFormats = ['webp', 'jpeg', 'png', 'gif'];
  dragOver = false;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.conversionForm = this.fb.group({
      inputPath: ['', Validators.required],
      outputPath: [''],
      format: ['webp', Validators.required],
    });
  }

  ngOnInit() {
    // Component initialization
  }

  onDragOver(event: DragEvent) {
    event.preventDefault();
    this.dragOver = true;
  }

  onDragLeave(event: DragEvent) {
    event.preventDefault();
    this.dragOver = false;
  }

  onDrop(event: DragEvent) {
    event.preventDefault();
    this.dragOver = false;

    const files = event.dataTransfer?.files;
    if (files && files.length > 0) {
      const file = files[0];
      if (this.isImageFile(file)) {
        this.conversionForm.patchValue({
          inputPath: file.name,
        });
      }
    }
  }

  isImageFile(file: File): boolean {
    return file.type.startsWith('image/');
  }

  async onFileSelected(event: any) {
    const file = event.target.files[0];
    if (file) {
      this.conversionForm.patchValue({
        inputPath: file.path ?? file.name,
      });
    }
  }

  selectFile() {
    this.fileInput.nativeElement.click();
  }

  async convertImage() {
    if (this.conversionForm.invalid) {
      return;
    }

    this.isConverting = true;
    this.result = null;

    const formValue = this.conversionForm.value;
    const request: ConversionRequest = {
      inputPath: formValue.inputPath,
      outputPath: formValue.outputPath,
      format: formValue.format,
      category: 'img',
    };

    try {
      this.result = await this.wailsService.convertFile(request);
    } catch (error) {
      this.result = {
        success: false,
        message: `Conversion failed: ${error}`,
      };
    } finally {
      this.isConverting = false;
    }
  }

  resetForm() {
    this.conversionForm.reset();
    this.conversionForm.patchValue({ format: 'webp' });
    this.result = null;
  }
}
