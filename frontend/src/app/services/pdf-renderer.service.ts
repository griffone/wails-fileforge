import { Injectable } from '@angular/core';

import { Wails } from './wails';

const PDF_WORKER_PUBLIC_URL = 'assets/pdfjs/pdf.worker.min.mjs';
const PDF_CMAP_PUBLIC_URL = 'assets/pdfjs/cmaps/';
const PDF_STANDARD_FONT_PUBLIC_URL = 'assets/pdfjs/standard_fonts/';
const PDF_ICC_PUBLIC_URL = 'assets/pdfjs/iccs/';
const PDF_WASM_PUBLIC_URL = 'assets/pdfjs/wasm/';
const BACKEND_PREVIEW_MAX_DIMENSION = 4096;
const BACKEND_PREVIEW_POLL_ATTEMPTS = 300;
const BACKEND_PREVIEW_POLL_DELAY_MS = 100;

// PDF rendering and extraction service.
// Implements lazy-loading of pdfjs-dist (keeps existing version declared in package.json)
// and exposes higher-level helpers used by the PDF split UI.

@Injectable({ providedIn: 'root' })
export class PdfRendererService {
  private pdfjs: any = null;
  private loadingTask: any = null;
  private workerSrcSet = false;

  constructor(private readonly wails: Wails) {}

  private async ensurePdfJs() {
    if (this.pdfjs) return this.pdfjs;
    // Lazy import to avoid bundling costs when feature not used.
    this.pdfjs = await import('pdfjs-dist');
    // Try to set workerSrc if available (webpack / angular builders)
    try {
      if (this.pdfjs.GlobalWorkerOptions && !this.workerSrcSet) {
        // Angular copies the worker asset into /assets/pdfjs during build.
        this.pdfjs.GlobalWorkerOptions.workerSrc = PDF_WORKER_PUBLIC_URL;
        this.workerSrcSet = true;
      }
    } catch {
      // ignore
    }
    return this.pdfjs;
  }

  async loadFromBytes(bytes: Uint8Array) {
    const pdfjs = await this.ensurePdfJs();
    // Cancel previous loading if any
    try {
      this.loadingTask?.destroy?.();
    } catch {}
    this.loadingTask = pdfjs.getDocument({
      data: bytes,
      cMapUrl: PDF_CMAP_PUBLIC_URL,
      cMapPacked: true,
      iccUrl: PDF_ICC_PUBLIC_URL,
      standardFontDataUrl: PDF_STANDARD_FONT_PUBLIC_URL,
      wasmUrl: PDF_WASM_PUBLIC_URL,
      useWasm: true,
      useWorkerFetch: true,
      isImageDecoderSupported: false,
      isOffscreenCanvasSupported: false,
    });
    return await this.loadingTask.promise;
  }

  async loadFromFile(file: File) {
    const arr = new Uint8Array(await file.arrayBuffer());
    return this.loadFromBytes(arr);
  }

  async getNumPages(pdf: any): Promise<number> {
    return pdf.numPages || 0;
  }

  // Render page to an existing canvas element. Caller provides canvas element.
  async renderPageToCanvas(
    pdf: any,
    pageNumber: number,
    canvas: HTMLCanvasElement,
    scale = 1,
  ) {
    const page = await pdf.getPage(pageNumber);
    const viewport = page.getViewport({ scale });
    const ctx = canvas.getContext('2d');
    if (!ctx) {
      throw new Error('Unable to create 2D context for PDF preview rendering.');
    }

    canvas.width = Math.max(1, Math.round(viewport.width));
    canvas.height = Math.max(1, Math.round(viewport.height));
    canvas.style.width = `${canvas.width}px`;
    canvas.style.height = `${canvas.height}px`;
    const renderContext = {
      canvasContext: ctx,
      viewport,
    };
    const renderTask = page.render(renderContext);
    await renderTask.promise;
    return canvas;
  }

  async renderPageToCanvasWithFallback(
    pdf: any,
    pageNumber: number,
    canvas: HTMLCanvasElement,
    scale = 1,
    sourcePath?: string,
  ): Promise<HTMLCanvasElement> {
    try {
      await this.renderPageToCanvas(pdf, pageNumber, canvas, scale);
      if (!sourcePath || this.canvasHasVisibleContent(canvas)) {
        return canvas;
      }
    } catch (error) {
      if (!sourcePath) {
        throw error;
      }
    }

    if (!sourcePath) {
      return canvas;
    }

    await this.renderBackendPreviewToCanvas(sourcePath, pageNumber, canvas);
    return canvas;
  }

  // Render to a Blob (PNG) using an offscreen canvas
  async renderPageToBlob(
    pdf: any,
    pageNumber: number,
    scale = 1,
    type = 'image/png',
  ) {
    const page = await pdf.getPage(pageNumber);
    const viewport = page.getViewport({ scale });
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(viewport.width));
    canvas.height = Math.max(1, Math.round(viewport.height));
    canvas.style.width = `${canvas.width}px`;
    canvas.style.height = `${canvas.height}px`;
    const ctx = canvas.getContext('2d');
    if (!ctx) {
      throw new Error('Unable to create 2D context for PDF preview rendering.');
    }
    const renderTask = page.render({ canvasContext: ctx, viewport });
    await renderTask.promise;
    return await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, type),
    );
  }

  private async renderBackendPreviewToCanvas(
    sourcePath: string,
    pageNumber: number,
    canvas: HTMLCanvasElement,
  ): Promise<void> {
    const width = Math.max(
      1,
      Math.min(BACKEND_PREVIEW_MAX_DIMENSION, Math.round(canvas.width || 0)),
    );
    const height = Math.max(
      1,
      Math.min(BACKEND_PREVIEW_MAX_DIMENSION, Math.round(canvas.height || 0)),
    );

    const request = {
      path: sourcePath,
      width,
      height,
      format: 'auto',
      pageRange: { start: pageNumber, end: pageNumber },
      pageOffset: 0,
    };

    const startResponse = await this.wails.StartPreview(request);
    if (!startResponse?.success || !startResponse?.jobID) {
      throw new Error(
        startResponse?.message || 'Unable to start backend preview.',
      );
    }

    const previewResult = await this.waitForPreviewResult(startResponse.jobID);
    if (!previewResult?.success || !previewResult?.data) {
      throw new Error(previewResult?.message || 'Backend preview failed.');
    }

    const image = await this.loadImageFromDataUrl(
      `data:${previewResult.contentType || 'image/png'};base64,${previewResult.data}`,
    );
    const context = canvas.getContext('2d');
    if (!context) {
      throw new Error(
        'Unable to create 2D context for backend preview rendering.',
      );
    }

    context.clearRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
  }

  private async waitForPreviewResult(jobID: string): Promise<any> {
    let terminalStatus: string | null = null;
    let terminalMessage = '';

    for (let attempt = 0; attempt < BACKEND_PREVIEW_POLL_ATTEMPTS; attempt++) {
      const status = await this.wails.GetPreviewStatus(jobID);
      if (
        status?.status === 'succeeded' ||
        status?.status === 'failed' ||
        status?.status === 'canceled' ||
        status?.status === 'timedout'
      ) {
        terminalStatus = status.status;
        terminalMessage = status?.message || '';
        break;
      }

      await new Promise((resolve) =>
        globalThis.setTimeout(resolve, BACKEND_PREVIEW_POLL_DELAY_MS),
      );
    }

    if (!terminalStatus) {
      return {
        success: false,
        message: 'Backend preview timed out while waiting for job completion.',
      };
    }

    if (terminalStatus !== 'succeeded') {
      return {
        success: false,
        message: terminalMessage || `Backend preview ${terminalStatus}.`,
      };
    }

    return await this.wails.GetPreview(jobID);
  }

  private async loadImageFromDataUrl(
    dataUrl: string,
  ): Promise<HTMLImageElement> {
    return await new Promise<HTMLImageElement>((resolve, reject) => {
      const image = new Image();
      image.onload = () => resolve(image);
      image.onerror = () =>
        reject(new Error('Unable to load backend preview image.'));
      image.src = dataUrl;
    });
  }

  private canvasHasVisibleContent(canvas: HTMLCanvasElement): boolean {
    const context = canvas.getContext('2d');
    if (!context || canvas.width === 0 || canvas.height === 0) {
      return false;
    }

    const { data } = context.getImageData(0, 0, canvas.width, canvas.height);
    let visibleSamples = 0;

    for (let offset = 0; offset < data.length; offset += 16) {
      const alpha = data[offset + 3] ?? 0;
      if (
        alpha > 32 &&
        ((data[offset] ?? 255) < 245 ||
          (data[offset + 1] ?? 255) < 245 ||
          (data[offset + 2] ?? 255) < 245)
      ) {
        visibleSamples++;
        if (visibleSamples >= 2) {
          return true;
        }
      }
    }

    return false;
  }

  // Extract a set of pages as individual Blob objects (PDF bytes). This helper is
  // a convenience used by the client-side split implementation which uses pdf-lib.
  async extractPagesAsBlobs(
    pdf: any,
    ranges: Array<{ from: number; to: number }>,
  ): Promise<Blob[]> {
    // Implementation note: We do not re-encode pages with pdfjs here. Instead,
    // consumers should use `pdf-lib` to produce new PDF files client-side. This
    // function is left as a placeholder for future page-level extraction if needed.
    return [];
  }

  async destroy(pdf: any) {
    try {
      await pdf?.destroy?.();
    } catch {}
    this.loadingTask = null;
  }
}
