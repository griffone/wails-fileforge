import { TestBed } from '@angular/core/testing';

import { PdfRendererService } from '../../services/pdf-renderer.service';
import { Wails } from '../../services/wails';

describe('PdfRendererService', () => {
  let service: PdfRendererService;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(() => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'StartPreview',
      'GetPreviewStatus',
      'GetPreview',
    ]);

    TestBed.configureTestingModule({
      providers: [{ provide: Wails, useValue: wailsSpy }],
    });
    service = TestBed.inject(PdfRendererService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('configures PDF.js with local font, cmap, and image-decoder settings', async () => {
    const getDocument = jasmine.createSpy('getDocument').and.returnValue({
      promise: Promise.resolve({ numPages: 1 }),
    });
    (service as any).pdfjs = {
      GlobalWorkerOptions: {},
      getDocument,
    };

    await service.loadFromBytes(new Uint8Array([37, 80, 68, 70]));

    expect(getDocument).toHaveBeenCalledWith(
      jasmine.objectContaining({
        cMapUrl: 'assets/pdfjs/cmaps/',
        cMapPacked: true,
        iccUrl: 'assets/pdfjs/iccs/',
        standardFontDataUrl: 'assets/pdfjs/standard_fonts/',
        wasmUrl: 'assets/pdfjs/wasm/',
        useWasm: true,
        useWorkerFetch: true,
        isImageDecoderSupported: false,
        isOffscreenCanvasSupported: false,
      }),
    );
  });

  it('renders the text PDF fixture to a visible canvas', async () => {
    const pdf = await service.loadFromBytes(
      await loadFixturePdfBytes('CV_Ivan_Decima_EN.pdf'),
    );

    const canvas = document.createElement('canvas');
    await service.renderPageToCanvas(pdf, 1, canvas, 1);

    expect(canvasHasVisibleContent(canvas)).toBeTrue();
  });

  it('renders the image-only PDF fixture to a visible canvas', async () => {
    const previewBase64 = createSolidPreviewBase64();
    wailsSpy.StartPreview.and.resolveTo({
      success: true,
      jobID: 'preview-job-1',
    });
    wailsSpy.GetPreviewStatus.and.resolveTo({ status: 'succeeded' });
    wailsSpy.GetPreview.and.resolveTo({
      success: true,
      data: previewBase64,
      contentType: 'image/png',
    });

    const pdf = await service.loadFromBytes(
      await loadFixturePdfBytes('factura_auriculares.pdf'),
    );

    const canvas = document.createElement('canvas');
    await service.renderPageToCanvasWithFallback(
      pdf,
      1,
      canvas,
      1,
      'test-fixtures/factura_auriculares.pdf',
    );

    expect(canvasHasVisibleContent(canvas)).toBeTrue();
  });
});

async function loadFixturePdfBytes(filename: string): Promise<Uint8Array> {
  const candidates = [
    `test-fixtures/${filename}`,
    `/test-fixtures/${filename}`,
    `base/test-fixtures/${filename}`,
  ];

  for (const candidate of candidates) {
    try {
      const response = await fetch(candidate);
      if (!response.ok) {
        continue;
      }

      return new Uint8Array(await response.arrayBuffer());
    } catch {
      // Try the next candidate.
    }
  }

  throw new Error(`Unable to load PDF fixture: ${filename}`);
}

function createSolidPreviewBase64(): string {
  const canvas = document.createElement('canvas');
  canvas.width = 16;
  canvas.height = 16;

  const context = canvas.getContext('2d');
  if (!context) {
    throw new Error('Unable to create preview canvas.');
  }

  context.fillStyle = '#000000';
  context.fillRect(0, 0, 16, 16);
  return canvas.toDataURL('image/png').split(',')[1] ?? '';
}

function canvasHasVisibleContent(canvas: HTMLCanvasElement): boolean {
  const context = canvas.getContext('2d');
  if (!context || canvas.width === 0 || canvas.height === 0) {
    return false;
  }

  const { data } = context.getImageData(0, 0, canvas.width, canvas.height);
  for (let offset = 0; offset < data.length; offset += 4) {
    const alpha = data[offset + 3] ?? 0;
    if (
      alpha > 32 &&
      ((data[offset] ?? 255) < 245 ||
        (data[offset + 1] ?? 255) < 245 ||
        (data[offset + 2] ?? 255) < 245)
    ) {
      return true;
    }
  }

  return false;
}
