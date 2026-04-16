import { Injectable } from '@angular/core';

// PDF rendering and extraction service.
// Implements lazy-loading of pdfjs-dist (keeps existing version declared in package.json)
// and exposes higher-level helpers used by the PDF split UI.

@Injectable({ providedIn: 'root' })
export class PdfRendererService {
  private pdfjs: any = null;
  private loadingTask: any = null;
  private workerSrcSet = false;

  private async ensurePdfJs() {
    if (this.pdfjs) return this.pdfjs;
    // Lazy import to avoid bundling costs when feature not used.
    this.pdfjs = await import('pdfjs-dist/legacy/build/pdf');
    // Try to set workerSrc if available (webpack / angular builders)
    try {
      if ((this.pdfjs as any).GlobalWorkerOptions && !this.workerSrcSet) {
        // Rely on pdfjs-dist provided worker; consumers may need to copy worker file
        // depending on build. Keep a TODO where Go backend fallback will be used.
        (this.pdfjs as any).GlobalWorkerOptions.workerSrc = 'pdf.worker.js';
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
    this.loadingTask = pdfjs.getDocument({ data: bytes });
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
  async renderPageToCanvas(pdf: any, pageNumber: number, canvas: HTMLCanvasElement, scale = 1.0) {
    const page = await pdf.getPage(pageNumber);
    const viewport = page.getViewport({ scale });
    const ctx = canvas.getContext('2d');
    canvas.width = Math.floor(viewport.width);
    canvas.height = Math.floor(viewport.height);
    const renderContext = {
      canvasContext: ctx,
      viewport,
    };
    const renderTask = page.render(renderContext);
    await renderTask.promise;
    return canvas;
  }

  // Render to a Blob (PNG) using an offscreen canvas
  async renderPageToBlob(pdf: any, pageNumber: number, scale = 1.0, type = 'image/png') {
    const page = await pdf.getPage(pageNumber);
    const viewport = page.getViewport({ scale });
    const canvas = document.createElement('canvas');
    canvas.width = Math.floor(viewport.width);
    canvas.height = Math.floor(viewport.height);
    const ctx = canvas.getContext('2d');
    const renderTask = page.render({ canvasContext: ctx, viewport });
    await renderTask.promise;
    return await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type));
  }

  // Extract a set of pages as individual Blob objects (PDF bytes). This helper is
  // a convenience used by the client-side split implementation which uses pdf-lib.
  async extractPagesAsBlobs(pdf: any, ranges: Array<{ from: number; to: number }>): Promise<Blob[]> {
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

/*
Notes:
- Memory: client-side processing of large PDFs may run out of memory in electron-like
  environments. The UI should warn users and fall back to a backend-based split (Wails)
  if blob creation throws OOM or observed memory pressure occurs.

TODO: Implement backend fallback hooks in the UI to call Wails.splitPdfBackend when
client-side split is not feasible.
*/
