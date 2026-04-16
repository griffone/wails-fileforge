import { Component } from '@angular/core';
import { PdfRendererService } from '../../services/pdf-renderer.service';
import { Wails } from '../../services/wails';

@Component({
  selector: 'app-pdf-split-preview',
  templateUrl: './pdf-split-preview.component.html',
  styleUrls: ['./pdf-split-preview.component.scss'],
  standalone: true,
})
export class PdfSplitPreviewComponent {
  file: File | null = null;
  pdf: any = null;
  pages: number[] = [];
  selectedRanges: Array<{ from: number; to: number }> = [];

  constructor(private pdfRenderer: PdfRendererService, private wails: Wails) {}

  async onFileSelected(ev: Event) {
    const input = ev.target as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;
    this.file = input.files[0];
    const pdf = await this.pdfRenderer.loadFromFile(this.file);
    this.pdf = pdf;
    this.pages = Array.from({ length: pdf.numPages }, (_, i) => i + 1);
  }

  toggleRange(from: number, to: number) {
    const idx = this.selectedRanges.findIndex((r) => r.from === from && r.to === to);
    if (idx >= 0) this.selectedRanges.splice(idx, 1);
    else this.selectedRanges.push({ from, to });
  }

  async splitClientSide() {
    if (!this.file || !this.pdf) return;
    // Use pdf-lib + jszip in the client to create split files and a single ZIP.
    try {
      const { PDFDocument } = await import('pdf-lib');
      const JSZip = (await import('jszip')).default;
      const arrayBuffer = await this.file.arrayBuffer();
      const srcPdf = await PDFDocument.load(arrayBuffer as ArrayBuffer);

      const zip = new JSZip();
      if (this.selectedRanges.length === 0) {
        // Default: split each page to its own file
        for (let i = 0; i < srcPdf.getPageCount(); i++) {
          const newDoc = await PDFDocument.create();
          const [copied] = await newDoc.copyPages(srcPdf, [i]);
          newDoc.addPage(copied);
          const bytes = await newDoc.save();
          zip.file(`page-${i + 1}.pdf`, bytes);
        }
      } else {
        for (let r = 0; r < this.selectedRanges.length; r++) {
          const rng = this.selectedRanges[r];
          const pages: number[] = [];
          for (let p = rng.from; p <= rng.to; p++) pages.push(p - 1);
          const newDoc = await PDFDocument.create();
          const copied = await newDoc.copyPages(srcPdf, pages);
          copied.forEach((pg) => newDoc.addPage(pg));
          const bytes = await newDoc.save();
          zip.file(`part-${rng.from}-${rng.to}.pdf`, bytes);
        }
      }

      const content = await zip.generateAsync({ type: 'blob' });
      const url = URL.createObjectURL(content);
      const a = document.createElement('a');
      a.href = url;
      a.download = `${this.file.name.replace(/\.pdf$/i, '')}-split.zip`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    } catch (err) {
      // On memory errors or other failures we should fallback to calling the backend Wails method.
      console.error('Client-side split failed, falling back to backend', err);
      // TODO: call Wails.splitPdfBackend when implemented on Go side.
      // await this.wails.splitPdfBackend(...)
    }
  }
}
