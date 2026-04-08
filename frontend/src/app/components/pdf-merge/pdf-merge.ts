import { Component, ElementRef, OnDestroy, ViewChild } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { RouterLink } from '@angular/router';
import {
  JobErrorV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  Wails,
} from '../../services/wails';
import { FileDrop } from '../file-drop/file-drop';

const PDF_MERGE_TOOL_ID = 'tool.pdf.merge';
const POLLING_INTERVAL_MS = 1000;

@Component({
  selector: 'app-pdf-merge',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, RouterLink],
  templateUrl: './pdf-merge.html',
  styleUrl: './pdf-merge.css',
})
export class PdfMerge implements OnDestroy {
  @ViewChild('fileInput') fileInput?: ElementRef<HTMLInputElement>;

  readonly form;
  selectedInputPaths: string[] = [];

  validationMessage = '';
  submitMessage = '';
  statusMessage = '';
  jobResult: JobResultV1 | null = null;
  isSubmitting = false;
  isPolling = false;
  activeJobId = '';
  dragOver = false;
  draggedIndex: number | null = null;
  showRunOrderConfirmation = false;
  confirmedOrderSnapshot: string[] = [];
  confirmedOutputPath = '';

  private pollingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(
    private readonly fb: FormBuilder,
    private readonly wailsService: Wails
  ) {
    this.form = this.fb.nonNullable.group({
      outputPath: ['', Validators.required],
    });
  }

  ngOnDestroy(): void {
    this.stopPolling();
  }

  async validate(): Promise<void> {
    this.clearMessages();

    const localError = this.localValidationError();
    if (localError) {
      this.validationMessage = localError;
      return;
    }

    const request = this.buildRequest();
    const response = await this.wailsService.validateJobV1(request);
    if (!response.valid) {
      this.logSupportEvent('validate', response.error, 'ValidateJobV1 returned invalid request');
    }
    this.validationMessage = response.valid
      ? 'Validation OK. Ready to run merge.'
      : this.mapJobError(response.error, response.message);
  }

  async run(): Promise<void> {
    this.clearMessages();

    const localError = this.localValidationError();
    if (localError) {
      this.submitMessage = localError;
      return;
    }

    this.confirmedOrderSnapshot = [...this.selectedInputPaths];
    this.confirmedOutputPath = this.form.controls.outputPath.value.trim();
    this.showRunOrderConfirmation = true;
  }

  cancelRunOrderConfirmation(): void {
    this.showRunOrderConfirmation = false;
    this.submitMessage = 'Ejecución cancelada: no se confirmó el orden final.';
  }

  async confirmRunOrderAndExecute(): Promise<void> {
    if (!this.showRunOrderConfirmation) {
      return;
    }

    const localError = this.localValidationError();
    if (localError) {
      this.showRunOrderConfirmation = false;
      this.submitMessage = localError;
      return;
    }

    const request = this.buildRequestWithInputs(this.confirmedOrderSnapshot, this.confirmedOutputPath);
    this.showRunOrderConfirmation = false;

    await this.executeRunRequest(request);
  }

  private async executeRunRequest(request: JobRequestV1): Promise<void> {
    this.clearMessages();

    this.isSubmitting = true;
    this.submitMessage = '';
    this.statusMessage = '';
    this.jobResult = null;

    const validation = await this.wailsService.validateJobV1(request);
    if (!validation.valid) {
      this.logSupportEvent('run-validate', validation.error, 'Run blocked by validation error');
      this.submitMessage = this.mapJobError(
        validation.error,
        'Validation failed before submit.'
      );
      this.isSubmitting = false;
      return;
    }

    const runResponse = await this.wailsService.runJobV1(request);
    if (!runResponse.success || !runResponse.jobId) {
      this.logSupportEvent('run-submit', runResponse.error, 'RunJobV1 failed to submit');
      this.submitMessage = this.mapJobError(
        runResponse.error,
        runResponse.message
      );
      this.isSubmitting = false;
      return;
    }

    this.activeJobId = runResponse.jobId;
    this.submitMessage = `Job submitted: ${runResponse.jobId}`;
    this.logSupportEvent('run-submitted', undefined, 'Job submitted successfully');
    this.startPolling();
    this.isSubmitting = false;
  }

  async cancel(): Promise<void> {
    if (!this.activeJobId) {
      return;
    }

    const response = await this.wailsService.cancelJobV1(this.activeJobId);
    this.statusMessage = response.success
      ? 'Cancel requested.'
      : this.mapJobError(response.error, response.message);

    if (!response.success) {
      this.logSupportEvent('cancel', response.error, 'Cancel request failed');
    }
  }

  async selectFilesFromDialog(): Promise<void> {
    this.clearMessages();
    const paths = await this.wailsService.openMultipleFilesDialog();
    this.appendInputPaths(paths);
  }

  async selectOutputDirectory(): Promise<void> {
    const selectedDir = (await this.wailsService.openDirectoryDialog()).trim();
    if (!selectedDir) {
      return;
    }

    const currentOutput = this.form.controls.outputPath.value.trim();
    const currentName = this.filenameFromPath(currentOutput);
    const outputName = this.isPDFPath(currentName) ? currentName : 'merged.pdf';

    this.form.patchValue({
      outputPath: this.joinPathLikeHostOS(selectedDir, outputName),
    });
    this.validationMessage = 'Carpeta de salida seleccionada. Revisá o ajustá el nombre final del archivo.';
  }

  triggerFileInput(): void {
    this.fileInput?.nativeElement.click();
  }

  onFileInputChange(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const files = target?.files;
    if (!files || files.length === 0) {
      return;
    }

    const paths = this.extractPathsFromFileList(files);
    this.appendInputPaths(paths);

    if (target) {
      target.value = '';
    }
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = true;
  }

  onDragLeave(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = false;
  }

  onFileDrop(event: DragEvent): void {
    event.preventDefault();
    this.dragOver = false;

    const files = event.dataTransfer?.files;
    if (!files || files.length === 0) {
      return;
    }

    const paths = this.extractPathsFromFileList(files);
    this.appendInputPaths(paths);
  }

  moveUp(index: number): void {
    if (index <= 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    this.swap(index, index - 1);
  }

  moveDown(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length - 1) {
      return;
    }
    this.swap(index, index + 1);
  }

  removeAt(index: number): void {
    if (index < 0 || index >= this.selectedInputPaths.length) {
      return;
    }
    this.selectedInputPaths.splice(index, 1);
  }

  clearInputList(): void {
    this.selectedInputPaths = [];
  }

  onReorderDragStart(index: number): void {
    this.draggedIndex = index;
  }

  onReorderDragOver(event: DragEvent): void {
    event.preventDefault();
  }

  onReorderDrop(targetIndex: number): void {
    if (this.draggedIndex === null || this.draggedIndex === targetIndex) {
      this.draggedIndex = null;
      return;
    }

    const [moved] = this.selectedInputPaths.splice(this.draggedIndex, 1);
    if (typeof moved === 'string') {
      this.selectedInputPaths.splice(targetIndex, 0, moved);
    }
    this.draggedIndex = null;
  }

  onReorderDragEnd(): void {
    this.draggedIndex = null;
  }

  filenameFromPath(path: string): string {
    const normalized = path.replace(/\\/g, '/');
    const idx = normalized.lastIndexOf('/');
    return idx >= 0 ? normalized.slice(idx + 1) : normalized;
  }

  private localValidationError(): string {
    const outputPath = this.form.controls.outputPath.value.trim();
    const inputPaths = this.selectedInputPaths.map((path) => path.trim());

    if (inputPaths.length < 2) {
      return 'Seleccioná al menos 2 PDFs para poder hacer merge.';
    }

    const invalidInput = inputPaths.find((path) => !this.isPDFPath(path));
    if (invalidInput) {
      return `Archivo inválido en la lista: ${invalidInput}. Solo se permiten .pdf.`;
    }

    const duplicate = this.findDuplicatePath(inputPaths);
    if (duplicate) {
      return `Archivo duplicado detectado: ${duplicate}. Eliminá el duplicado o reordená la lista.`;
    }

    if (!outputPath) {
      return 'Indicá un outputPath para guardar el PDF merged.';
    }

    if (outputPath.endsWith('/') || outputPath.endsWith('\\')) {
      return 'El outputPath debe incluir nombre de archivo (ejemplo: merged.pdf), no solo carpeta.';
    }

    if (!this.isPDFPath(outputPath)) {
      return 'El outputPath debe terminar en .pdf';
    }

    const collision = inputPaths.find(
      (path) => this.normalizePathKey(path) === this.normalizePathKey(outputPath)
    );
    if (collision) {
      return `El outputPath colisiona con un input: ${collision}. Elegí otro archivo de salida.`;
    }

    return '';
  }

  private buildRequest(): JobRequestV1 {
    const outputPath = this.form.controls.outputPath.value.trim();

    return this.buildRequestWithInputs(this.selectedInputPaths, outputPath);
  }

  private buildRequestWithInputs(inputPaths: string[], outputPath: string): JobRequestV1 {
    const normalizedInputPaths = inputPaths.map((path) => path.trim());

    return {
      toolId: PDF_MERGE_TOOL_ID,
      mode: 'single',
      inputPaths: normalizedInputPaths,
      outputDir: this.outputDirFromPath(outputPath),
      options: {
        outputPath,
      },
    };
  }

  private outputDirFromPath(outputPath: string): string {
    const lastSlash = Math.max(outputPath.lastIndexOf('/'), outputPath.lastIndexOf('\\'));
    if (lastSlash <= 0) {
      return '';
    }
    return outputPath.slice(0, lastSlash);
  }

  private startPolling(): void {
    this.stopPolling();
    this.isPolling = true;
    this.pollingTimer = setInterval(() => {
      void this.pollJobStatus();
    }, POLLING_INTERVAL_MS);
    void this.pollJobStatus();
  }

  private stopPolling(): void {
    if (this.pollingTimer) {
      clearInterval(this.pollingTimer);
      this.pollingTimer = null;
    }
    this.isPolling = false;
  }

  private async pollJobStatus(): Promise<void> {
    if (!this.activeJobId) {
      this.stopPolling();
      return;
    }

    const response: JobStatusResponseV1 = await this.wailsService.getJobStatusV1(
      this.activeJobId
    );
    if (!response.found || !response.result) {
      this.statusMessage = this.mapJobError(response.error, response.message);
      this.logSupportEvent('poll-not-found', response.error, 'GetJobStatusV1 returned not found');
      this.stopPolling();
      return;
    }

    this.jobResult = response.result;
    const resultErrorMessage = response.result.error
      ? this.mapJobError(response.result.error, response.result.message)
      : response.result.message;
    this.statusMessage = `${response.result.status}: ${resultErrorMessage}`;

    if (response.result.error) {
      this.logSupportEvent('poll-result-error', response.result.error, 'Terminal or intermediate job error received');
    }

    if (
      response.result.status === 'success' ||
      response.result.status === 'failed' ||
      response.result.status === 'partial_success' ||
      response.result.status === 'cancelled' ||
      response.result.status === 'interrupted'
    ) {
      this.stopPolling();
      this.activeJobId = '';
    }
  }

  private clearMessages(): void {
    this.validationMessage = '';
    this.submitMessage = '';
  }

  private mapJobError(error: JobErrorV1 | undefined, fallback: string): string {
    if (!error) {
      return fallback;
    }

    switch (error.code) {
      case 'VALIDATION_INVALID_INPUT':
        return `Validación${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
      case 'RUNTIME_DEP_MISSING':
        return `Falta dependencia de runtime.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'EXEC_IO_TRANSIENT':
        return `No se pudo completar el merge por un error de ejecución.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'EXEC_TIMEOUT_TRANSIENT':
        return `La ejecución agotó tiempo.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'UNSUPPORTED_FORMAT':
        return `Formato no soportado.${error.detail_code ? ` [${error.detail_code}]` : ''} ${error.message}`;
      case 'CANCELLED_BY_USER':
        return 'El job fue cancelado.';
      default:
        return `${error.code}${error.detail_code ? ` [${error.detail_code}]` : ''}: ${error.message}`;
    }
  }

  appendInputPaths(rawPaths: string[]): void {
    const normalized = rawPaths
      .map((path) => path.trim())
      .filter((path) => path.length > 0);

    if (normalized.length === 0) {
      return;
    }

    const existing = new Set(this.selectedInputPaths.map((path) => this.normalizePathKey(path)));
    const added: string[] = [];
    const ignored: string[] = [];

    for (const path of normalized) {
      if (!this.isPDFPath(path)) {
        ignored.push(path);
        continue;
      }

      const key = this.normalizePathKey(path);
      if (existing.has(key)) {
        ignored.push(path);
        continue;
      }

      this.selectedInputPaths.push(path);
      existing.add(key);
      added.push(path);
    }

    if (added.length > 0) {
      this.validationMessage = `${added.length} archivo(s) agregado(s) a la cola.`;
    }

    if (ignored.length > 0) {
      this.submitMessage = `Se ignoraron ${ignored.length} archivo(s) por extensión inválida o duplicado.`;
    }
  }

  private extractPathsFromFileList(files: FileList): string[] {
    const paths: string[] = [];
    for (let index = 0; index < files.length; index += 1) {
      const file = files.item(index);
      if (!file) {
        continue;
      }
      const nativePath = (file as File & { path?: string }).path;
      paths.push(nativePath ?? file.name);
    }
    return paths;
  }

  private findDuplicatePath(paths: string[]): string {
    const seen = new Set<string>();
    for (const path of paths) {
      const key = this.normalizePathKey(path);
      if (seen.has(key)) {
        return path;
      }
      seen.add(key);
    }
    return '';
  }

  private isPDFPath(path: string): boolean {
    return path.toLowerCase().endsWith('.pdf');
  }

  private joinPathLikeHostOS(dir: string, fileName: string): string {
    const trimmedDir = dir.trim().replace(/[\\/]+$/, '');
    const separator = trimmedDir.includes('\\') && !trimmedDir.includes('/') ? '\\' : '/';
    return `${trimmedDir}${separator}${fileName}`;
  }

  private normalizePathKey(rawPath: string): string {
    const trimmed = rawPath.trim();
    if (!trimmed) {
      return '';
    }

    const normalizedSlashes = trimmed.replace(/\\/g, '/');

    if (normalizedSlashes.startsWith('//')) {
      const uncBody = normalizedSlashes.replace(/^\/+/, '');
      return `unc://${this.cleanSlashPath(`/${uncBody}`).replace(/^\/+/, '').toLowerCase()}`;
    }

    const driveMatch = normalizedSlashes.match(/^([a-zA-Z]):(.*)$/);
    if (driveMatch) {
      const drive = driveMatch[1].toLowerCase();
      const rest = driveMatch[2] ?? '';
      return `win:${drive}:${this.cleanSlashPath(`/${rest.replace(/^\/+/, '')}`).toLowerCase()}`;
    }

    return `posix:${this.cleanSlashPath(normalizedSlashes).toLowerCase()}`;
  }

  private logSupportEvent(stage: string, error?: JobErrorV1, note?: string): void {
    const payload = {
      stage,
      toolId: PDF_MERGE_TOOL_ID,
      jobId: this.activeJobId || 'pending',
      errorCode: error?.code,
      errorMessage: error?.message,
      note,
    };

    if (error) {
      console.warn('[pdf-merge-support]', payload);
      return;
    }

    console.info('[pdf-merge-support]', payload);
  }

  private cleanSlashPath(inputPath: string): string {
    const absolute = inputPath.startsWith('/');
    const segments = inputPath.split('/');
    const stack: string[] = [];

    for (const segment of segments) {
      if (!segment || segment === '.') {
        continue;
      }
      if (segment === '..') {
        if (stack.length > 0) {
          stack.pop();
        }
        continue;
      }
      stack.push(segment);
    }

    const joined = stack.join('/');
    if (!joined) {
      return absolute ? '/' : '.';
    }

    return absolute ? `/${joined}` : joined;
  }

  private swap(leftIndex: number, rightIndex: number): void {
    const left = this.selectedInputPaths[leftIndex];
    this.selectedInputPaths[leftIndex] = this.selectedInputPaths[rightIndex];
    this.selectedInputPaths[rightIndex] = left;
  }
}
