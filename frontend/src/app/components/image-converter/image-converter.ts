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
  BatchConversionRequest,
  BatchConversionResult,
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
  batchResult: BatchConversionResult | null = null;
  supportedFormats = ['webp', 'jpeg', 'png', 'gif'];
  dragOver = false;

  // Multiple file mode
  isMultipleMode = false;
  selectedFiles: string[] = [];
  outputDirectory = '';

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.conversionForm = this.fb.group({
      inputPath: ['', Validators.required],
      outputPath: [''],
      format: ['webp', Validators.required],
      outputDir: [''],
      keepStructure: [false],
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
      if (this.isMultipleMode) {
        this.handleMultipleFilesDrop(files);
      } else {
        this.handleSingleFileDrop(files[0]);
      }
    }
  }

  private handleMultipleFilesDrop(files: FileList) {
    const imagePaths: string[] = [];
    for (const file of Array.from(files)) {
      if (this.isImageFile(file)) {
        imagePaths.push(this.getFilePath(file));
      }
    }
    this.selectedFiles = imagePaths;
  }

  private handleSingleFileDrop(file: File) {
    if (this.isImageFile(file)) {
      const filePath = this.getFilePath(file);
      this.conversionForm.patchValue({
        inputPath: filePath,
      });

      // If we only get the filename, show a warning
      if (!this.isFullPath(filePath)) {
        console.warn(
          'Drag and drop may not provide full file path. Consider using the file picker button.'
        );
      }
    }
  }

  isImageFile(file: File): boolean {
    return file.type.startsWith('image/');
  }

  /**
   * Extract the full file path from a File object in Wails context
   */
  private getFilePath(file: File): string {
    // In Wails, File objects have a path property with the full file path
    const fullPath = (file as any).path ?? file.name;
    console.log('File selected:', { name: file.name, fullPath });
    return fullPath;
  }

  /**
   * Check if the given path is a full path (not just a filename)
   */
  private isFullPath(path: string): boolean {
    if (!path || path.trim() === '') {
      return false;
    }

    // Check for common path patterns
    // Windows: C:\path\to\file or \\server\share\file or D:\file.ext
    // Unix-like: /path/to/file or ~/path/to/file or ./relative/path
    const windowsDrivePattern = /^[A-Za-z]:[/\\]/; // C:\ or C:/
    const windowsUNCPattern = /^\\\\[^\\]+\\/; // \\server\share
    const unixAbsolutePattern = /^\/[^/]/; // /path (not just /)
    const unixHomePattern = /^~[/\\]/; // ~/path
    const relativePattern = /^\.{1,2}[/\\]/; // ./path or ../path

    return (
      windowsDrivePattern.test(path) || // Windows drive letter
      windowsUNCPattern.test(path) || // Windows UNC path
      unixAbsolutePattern.test(path) || // Unix absolute path
      unixHomePattern.test(path) || // Home directory
      relativePattern.test(path) || // Relative path with ./
      (path.includes('/') && path.indexOf('/') > 0) || // Contains directory separator not at start
      (path.includes('\\') && path.indexOf('\\') > 0) // Contains Windows separator not at start
    );
  }

  async onFileSelected(event: any) {
    const file = event.target.files[0];
    if (file) {
      this.conversionForm.patchValue({
        inputPath: this.getFilePath(file),
      });
    }
  }

  async onMultipleFilesSelected(event: any) {
    const files = event.target.files as FileList;
    if (files && files.length > 0) {
      const imagePaths: string[] = [];
      Array.from(files).forEach((file) => {
        if (this.isImageFile(file)) {
          imagePaths.push(this.getFilePath(file));
        }
      });
      this.selectedFiles = imagePaths;
    }
  }

  async selectFile() {
    // Use native file dialog to get the full file path
    try {
      const filePath = await this.wailsService.openFileDialog();
      if (filePath) {
        this.conversionForm.patchValue({
          inputPath: filePath,
        });
      }
    } catch (error) {
      console.error('Error opening file dialog:', error);
      // Fallback to browser file input
      this.fileInput.nativeElement.click();
    }
  }

  toggleMode() {
    this.isMultipleMode = !this.isMultipleMode;
    this.resetForm();
  }

  async selectMultipleFiles() {
    try {
      const filePaths = await this.wailsService.openMultipleFilesDialog();
      if (filePaths && filePaths.length > 0) {
        this.selectedFiles = filePaths;
      }
    } catch (error) {
      console.error('Error opening multiple files dialog:', error);
      // Fallback to browser file input
      const input = document.createElement('input');
      input.type = 'file';
      input.multiple = true;
      input.accept = 'image/*';
      input.onchange = (e) => {
        const target = e.target as HTMLInputElement;
        if (target.files) {
          const paths = Array.from(target.files).map((file) =>
            this.getFilePath(file)
          );
          this.selectedFiles = paths;
        }
      };
      input.click();
    }
  }

  async selectOutputDirectory() {
    try {
      const dirPath = await this.wailsService.openDirectoryDialog();
      if (dirPath) {
        this.outputDirectory = dirPath;
        this.conversionForm.patchValue({
          outputDir: dirPath,
        });
      }
    } catch (error) {
      console.error('Error opening directory dialog:', error);
      // For now, let user enter manually
    }
  }

  removeFile(index: number) {
    this.selectedFiles.splice(index, 1);
  }

  async convertImage() {
    if (this.isMultipleMode) {
      await this.convertBatch();
    } else {
      await this.convertSingle();
    }
  }

  async convertSingle() {
    if (this.conversionForm.invalid) {
      return;
    }

    this.isConverting = true;
    this.result = null;

    const formValue = this.conversionForm.value;

    // Validate that we have a full path, not just a filename
    if (!this.isFullPath(formValue.inputPath)) {
      this.result = {
        success: false,
        message:
          'Please select a file using the file picker or enter the complete file path (not just filename)',
      };
      this.isConverting = false;
      return;
    }

    const request: ConversionRequest = {
      inputPath: formValue.inputPath,
      outputPath: formValue.outputPath,
      format: formValue.format,
      category: 'img',
      options: {},
    };

    console.log('Conversion request:', request);

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

  async convertBatch() {
    if (this.selectedFiles.length === 0) {
      this.batchResult = {
        success: false,
        message: 'Please select at least one file to convert',
        totalFiles: 0,
        successCount: 0,
        failureCount: 0,
        results: [],
      };
      return;
    }

    const formValue = this.conversionForm.value;

    if (!formValue.outputDir || formValue.outputDir.trim() === '') {
      this.batchResult = {
        success: false,
        message: 'Please select an output directory for batch conversion',
        totalFiles: 0,
        successCount: 0,
        failureCount: 0,
        results: [],
      };
      return;
    }

    this.isConverting = true;
    this.batchResult = null;

    const request: BatchConversionRequest = {
      inputPaths: this.selectedFiles,
      outputDir: formValue.outputDir,
      format: formValue.format,
      category: 'img',
      options: {},
      keepStructure: formValue.keepStructure ?? false,
    };

    console.log('Batch conversion request:', request);

    try {
      this.batchResult = await this.wailsService.convertBatch(request);
    } catch (error) {
      this.batchResult = {
        success: false,
        message: `Batch conversion failed: ${error}`,
        totalFiles: 0,
        successCount: 0,
        failureCount: 0,
        results: [],
      };
    } finally {
      this.isConverting = false;
    }
  }

  resetForm() {
    this.conversionForm.reset();
    this.conversionForm.patchValue({ format: 'webp' });
    this.result = null;
    this.batchResult = null;
    this.selectedFiles = [];
    this.outputDirectory = '';
  }
}
