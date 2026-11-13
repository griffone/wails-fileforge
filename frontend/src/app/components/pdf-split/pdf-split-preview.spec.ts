import { ComponentFixture, TestBed } from '@angular/core/testing';

import { PdfSplitPreview } from './pdf-split-preview';
import { PdfRendererService } from '../../services/pdf-renderer.service';
import { PDFPreviewSourceResponseV1, Wails } from '../../services/wails';

type PreviewStrategy = 'every_page' | 'ranges';

describe('PdfSplitPreview', () => {
  let fixture: ComponentFixture<PdfSplitPreview>;
  let component: PdfSplitPreview;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let rendererSpy: jasmine.SpyObj<PdfRendererService>;

  function fakePdf(numPages: number): unknown {
    return {
      numPages,
      getPage: () =>
        Promise.resolve({
          getViewport: () => ({ width: 600, height: 800 }),
        }),
    };
  }

  function previewSource(): PDFPreviewSourceResponseV1 {
    return {
      success: true,
      message: 'ok',
      dataBase64: btoa('fake-pdf-bytes'),
      mimeType: 'application/pdf',
    };
  }

  async function render(
    paths: string[],
    strategy: PreviewStrategy = 'every_page',
    ranges = '',
    numPages = 3,
  ): Promise<void> {
    rendererSpy.loadFromBytes.and.resolveTo(fakePdf(numPages));

    fixture = TestBed.createComponent(PdfSplitPreview);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('paths', paths);
    fixture.componentRef.setInput('strategy', strategy);
    fixture.componentRef.setInput('ranges', ranges);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  beforeEach(() => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', ['getPdfPreviewSource']);
    wailsSpy.getPdfPreviewSource.and.resolveTo(previewSource());

    rendererSpy = jasmine.createSpyObj<PdfRendererService>(
      'PdfRendererService',
      [
        'loadFromBytes',
        'renderPageToCanvasWithFallback',
        'destroy',
      ],
    );
    rendererSpy.loadFromBytes.and.resolveTo(fakePdf(3));
    rendererSpy.renderPageToCanvasWithFallback.and.callFake(
      async (
        _pdf: unknown,
        _pageNumber: number,
        canvas: HTMLCanvasElement,
      ) => {
        canvas.width = 140;
        canvas.height = 181;
        canvas.getContext('2d')?.fillRect(0, 0, 140, 181);
        return canvas;
      },
    );
    rendererSpy.destroy.and.resolveTo();

    TestBed.configureTestingModule({
      imports: [PdfSplitPreview],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: PdfRendererService, useValue: rendererSpy },
      ],
    });
  });

  afterEach(() => {
    fixture?.destroy();
  });

  it('renders one card per page with the output name for every_page', async () => {
    await render(['/tmp/a.pdf'], 'every_page', '', 3);

    const element = fixture.nativeElement as HTMLElement;
    const cards = element.querySelectorAll('.page-card');

    expect(cards.length).toBe(3);
    expect(element.textContent).toContain('page_001.pdf');
    expect(element.textContent).toContain('page_002.pdf');
    expect(element.textContent).toContain('page_003.pdf');
    expect(element.textContent).toContain('3 files');
    expect(cards[0].querySelector('img')?.getAttribute('alt')).toBe(
      'Page 1 preview',
    );
    expect(cards[2].querySelector('.page-number')?.textContent).toContain(
      'Page 3',
    );
  });

  it('groups the split output by range with names and counts', async () => {
    await render(['/tmp/a.pdf'], 'ranges', '1-2,3', 3);

    const element = fixture.nativeElement as HTMLElement;
    const cards = element.querySelectorAll('.range-card');
    const text = element.textContent as string;

    expect(cards.length).toBe(2);
    expect(text).toContain('Part 1 · Pages 1-2');
    expect(text).toContain('range_001_p1-2.pdf');
    expect(text).toContain('Part 2 · Page 3');
    expect(text).toContain('range_002_p3.pdf');
    expect(text).toContain('2 files');
    expect(text).toContain('1 file');
    expect(text).toContain('2 parts');
    expect(cards[0].querySelectorAll('.range-page').length).toBe(2);
    expect(cards[1].querySelectorAll('.range-page').length).toBe(1);
  });

  it('shows an inline error and no groups for invalid ranges', async () => {
    await render(['/tmp/a.pdf'], 'ranges', '2-4', 3);

    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelectorAll('.range-card').length).toBe(0);
    expect(element.querySelector('.preview-alert')?.getAttribute('role')).toBe(
      'alert',
    );
    expect(element.textContent).toContain(
      'Page 4 is outside this PDF (3 pages).',
    );
  });

  it('shows the fallback when the preview source fails', async () => {
    wailsSpy.getPdfPreviewSource.and.resolveTo({
      success: false,
      message: 'PDF is too large to preview.',
    });

    await expectAsync(
      (async () => {
        await render(['/tmp/a.pdf']);
      })(),
    ).toBeResolved();

    const element = fixture.nativeElement as HTMLElement;

    expect(rendererSpy.loadFromBytes).not.toHaveBeenCalled();
    expect(element.textContent).toContain(
      'Preview unavailable for this PDF. The split will still work.',
    );
    expect(element.querySelector('.preview-alert')?.getAttribute('role')).toBe(
      'alert',
    );
  });

  it('shows a file selector for multiple PDFs and reloads on change', async () => {
    await render(['/tmp/a.pdf', '/tmp/b.pdf']);

    const select = fixture.nativeElement.querySelector(
      '#pdf-split-preview-file',
    ) as HTMLSelectElement;

    expect(select).not.toBeNull();
    expect(select.options.length).toBe(2);
    expect(select.value).toBe('/tmp/a.pdf');
    expect(wailsSpy.getPdfPreviewSource).toHaveBeenCalledWith('/tmp/a.pdf');

    select.value = '/tmp/b.pdf';
    select.dispatchEvent(new Event('change'));
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.getPdfPreviewSource).toHaveBeenCalledWith('/tmp/b.pdf');
    expect(rendererSpy.loadFromBytes).toHaveBeenCalledTimes(2);
    expect(component.selectedPath()).toBe('/tmp/b.pdf');
  });

  it('regroups without reloading the PDF when the strategy changes', async () => {
    await render(['/tmp/a.pdf'], 'every_page', '', 3);

    expect(rendererSpy.loadFromBytes).toHaveBeenCalledTimes(1);
    expect(fixture.nativeElement.querySelectorAll('.page-card').length).toBe(3);

    fixture.componentRef.setInput('strategy', 'ranges');
    fixture.componentRef.setInput('ranges', '1-2,3');
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(rendererSpy.loadFromBytes).toHaveBeenCalledTimes(1);
    expect(fixture.nativeElement.querySelectorAll('.range-card').length).toBe(2);
  });
});
