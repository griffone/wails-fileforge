import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { EMPTY } from 'rxjs';

import { PdfOcr } from './pdf-ocr';
import {
  FileInfoV1,
  JobProgressV1,
  JobRequestV1,
  JobResultItemV1,
  JobResultV1,
  JobStatusResponseV1,
  OCRCapabilitiesResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('PdfOcr', () => {
  let fixture: ComponentFixture<PdfOcr>;
  let component: PdfOcr;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function capabilities(
    overrides: Partial<OCRCapabilitiesResponseV1> = {},
  ): OCRCapabilitiesResponseV1 {
    return {
      success: true,
      message: 'Local OCR is available with 2 installed languages.',
      available: true,
      pdfToPpm: true,
      tesseract: true,
      pdfToText: true,
      languages: ['eng', 'spa'],
      recommendedLanguage: 'eng',
      ...overrides,
    };
  }

  function fileInfo(path: string): FileInfoV1 {
    return {
      path,
      name: path.split(/[\\/]/).pop() ?? path,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 512 * 1024,
      kind: 'pdf',
      pageCount: 5,
      exists: true,
    };
  }

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function fileStem(path: string): string {
    const name = path.split(/[\\/]/).pop() ?? 'document';
    return name.replace(/\.pdf$/i, '');
  }

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    const sidecar = request.options['textSidecar'] !== false;
    const outputs = request.inputPaths.flatMap((inputPath) => {
      const dir = request.outputDir || firstDir(inputPath);
      const stem = `${fileStem(inputPath)}_ocr`;
      const pdfOutput = {
        inputPath,
        outputPath: dir ? `${dir}/${stem}.pdf` : `${stem}.pdf`,
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      };
      if (!sidecar) {
        return [pdfOutput];
      }
      return [
        pdfOutput,
        {
          inputPath,
          outputPath: dir ? `${dir}/${stem}.txt` : `${stem}.txt`,
          outputKind: 'file',
          outputCount: 1,
          exists: false,
          adjusted: false,
        },
      ];
    });

    return {
      success: true,
      message: 'Ready to OCR 1 PDF.',
      outputs,
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function status(
    jobId: string,
    jobStatus: JobResultV1['status'],
    items: JobResultItemV1[],
    progress: Partial<JobProgressV1> = {},
  ): JobStatusResponseV1 {
    return {
      success: true,
      message: jobStatus,
      found: true,
      result: {
        jobId,
        success: jobStatus === 'success',
        message: jobStatus,
        toolId: 'tool.pdf.ocr',
        status: jobStatus,
        progress: {
          current: items.length,
          total: items.length || 1,
          stage: jobStatus,
          message: jobStatus,
          ...progress,
        },
        items,
        startedAt: 1,
        endedAt: jobStatus === 'running' ? undefined : 2,
      },
    };
  }

  /** Creates the page with the given capabilities and renders the first frame. */
  function create(caps: OCRCapabilitiesResponseV1 = capabilities()): void {
    wailsSpy.getOCRCapabilitiesV1.and.resolveTo(caps);
    fixture = TestBed.createComponent(PdfOcr);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'getOCRCapabilitiesV1',
      'inspectFilesV1',
      'planJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openDirectoryDialog',
      'openMultipleFilesDialog',
      'openPath',
      'revealInFolder',
    ]);
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ = EMPTY;

    wailsSpy.getOCRCapabilitiesV1.and.resolveTo(capabilities());
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'ocr-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJobV1.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'ocr-job-1',
    });
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [PdfOcr],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem('ff-preferences');
  });

  it('loads capabilities, selects the recommended language and renders the page', fakeAsync(() => {
    create(capabilities({ languages: ['afr', 'eng'], recommendedLanguage: 'eng' }));
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getOCRCapabilitiesV1).toHaveBeenCalled();
    expect(component.capabilities()?.available).toBeTrue();
    expect(component.ocrAvailable()).toBeTrue();
    expect(component.ocrBlocked()).toBeFalse();
    expect(component.language()).toBe('eng');
    expect(component.languages()).toEqual(['afr', 'eng']);

    const select = query<HTMLSelectElement>('#pdf-ocr-language');
    expect(select).not.toBeNull();
    expect(select?.disabled).toBeFalse();
    expect(select?.value).toBe('eng');
    expect(select?.options.length).toBe(2);

    expect((query<HTMLInputElement>('#pdf-ocr-sidecar') as HTMLInputElement).checked).toBeTrue();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('PDF OCR');
    expect(text).toContain('Drop scanned PDFs here');
    expect(text).toContain('Writes <name>_ocr.txt next to the searchable PDF.');
    expect(text).toContain(
      'Pages are rasterized at 300 DPI. Vector text is replaced by an image with an invisible text layer.',
    );
    expect(text).not.toContain('Local OCR is unavailable.');
    expect(component.actionHelper()).toBe('Add at least one PDF.');
  }));

  it('falls back to the first installed language without a recommendation', fakeAsync(() => {
    create(capabilities({ languages: ['afr'], recommendedLanguage: '' }));
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.language()).toBe('afr');
    expect(component.weakLanguageWarning()).toBe(
      'Only AFR language data is installed. Install eng/spa Tesseract data for better accuracy.',
    );
    expect(fixture.nativeElement.textContent).toContain(
      'Only AFR language data is installed. Install eng/spa Tesseract data for better accuracy.',
    );
  }));

  it('shows the danger banner and blocks the run when OCR is unavailable', fakeAsync(() => {
    create(
      capabilities({
        message: 'Local OCR is unavailable: install pdftoppm and tesseract.',
        available: false,
        pdfToPpm: false,
        tesseract: false,
        pdfToText: false,
        languages: [],
        recommendedLanguage: '',
      }),
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.ocrAvailable()).toBeFalse();
    expect(component.ocrBlocked()).toBeTrue();
    expect(component.missingPieces()).toEqual([
      'pdftoppm',
      'tesseract',
      'Tesseract language data (eng or spa)',
    ]);

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Local OCR is unavailable.');
    expect(text).toContain('Missing pdftoppm and tesseract.');
    expect(text).toContain(
      'No Tesseract language data is installed; install eng or spa.',
    );

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Local OCR is unavailable on this machine.',
    );
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeTrue();
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('plans with the chosen language and sidecar, and follows both changes', fakeAsync(() => {
    create(capabilities({ languages: ['afr', 'eng', 'spa'], recommendedLanguage: 'eng' }));
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    let request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.ocr');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/in/scan.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({ language: 'eng', textSidecar: true });
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/in/scan_ocr.pdf',
      '/tmp/in/scan_ocr.txt',
    ]);
    expect(component.primaryLabel()).toBe('OCR 1 PDF');

    component.onLanguageChange({ target: { value: 'spa' } } as unknown as Event);
    tick(250);
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options).toEqual({ language: 'spa', textSidecar: true });

    component.onSidecarToggle({ target: { checked: false } } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options).toEqual({ language: 'spa', textSidecar: false });
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/in/scan_ocr.pdf',
    ]);
    expect(component.sidecar()).toBeFalse();
  }));

  it('uses batch mode for several PDFs and rejects non-PDF drops', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected([
      '/tmp/in/a.pdf',
      '/tmp/in/b.pdf',
      '/tmp/in/notes.txt',
    ]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/in/a.pdf', '/tmp/in/b.pdf']);
    expect(component.notice()?.text).toContain("isn't a PDF");
    expect(component.primaryLabel()).toBe('OCR 2 PDFs');
    expect(component.plan()?.outputs.length).toBe(4);
  }));

  it('runs, polls to terminal and shows the recognized summary and result', fakeAsync(() => {
    create();
    flushMicrotasks();

    wailsSpy.getJobStatusV1.and.callFake((jobId: string) => {
      const calls = wailsSpy.getJobStatusV1.calls.all().length;
      if (calls <= 1) {
        return Promise.resolve(
          status(jobId, 'running', [], {
            stage: 'ocr',
            current: 3,
            total: 5,
            message: 'recognizing page 3/5',
          }),
        );
      }
      return Promise.resolve(
        status(jobId, 'success', [
          {
            inputPath: '/tmp/in/scan.pdf',
            outputPath: '/tmp/in/scan_ocr.pdf',
            outputs: ['/tmp/in/scan_ocr.pdf', '/tmp/in/scan_ocr.txt'],
            outputCount: 2,
            success: true,
            message: 'OCR finished: 5 pages, 1234 characters recognized.',
          },
        ]),
      );
    });

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    expect(component.activeJobId()).toBe('ocr-job-1');
    expect(component.pageProgressLabel()).toBe('OCR page 3 of 5');
    expect(fixture.nativeElement.textContent).toContain('OCR page 3 of 5');

    tick(1000);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.recognizedSummary()?.tone).toBe('success');
    expect(component.recognizedSummary()?.text).toBe(
      'OCR finished: 5 pages, 1234 characters recognized.',
    );

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('OCR finished: 5 pages, 1234 characters recognized.');
    expect(query('app-result-card')).not.toBeNull();
    expect(text).toContain('scan_ocr.pdf');

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some(
        (paths) =>
          paths.includes('/tmp/in/scan_ocr.pdf') &&
          paths.includes('/tmp/in/scan_ocr.txt'),
      ),
    ).toBeTrue();

    (query<HTMLButtonElement>('.result-open') as HTMLButtonElement).click();
    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/in/scan_ocr.pdf');
  }));

  it('uses the warning tone when no text was recognized', fakeAsync(() => {
    create();
    flushMicrotasks();

    wailsSpy.getJobStatusV1.and.resolveTo(
      status('ocr-job-1', 'partial_success', [
        {
          inputPath: '/tmp/in/scan.pdf',
          outputPath: '/tmp/in/scan_ocr.pdf',
          outputs: ['/tmp/in/scan_ocr.pdf', '/tmp/in/scan_ocr.txt'],
          outputCount: 2,
          success: true,
          message:
            'OCR finished: 2 pages, 0 characters recognized. No text was detected.',
        },
      ]),
    );

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    tick(250);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.recognizedSummary()?.tone).toBe('warning');
    const summary = query<HTMLElement>('.recognized-summary');
    expect(summary?.classList.contains('banner-warning')).toBeTrue();
    expect(summary?.classList.contains('banner-success')).toBeFalse();
  }));

  it('cancels the running job', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    tick(250);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isPolling()).toBeTrue();

    void component.cancel();
    flushMicrotasks();

    expect(wailsSpy.cancelJobV1).toHaveBeenCalledWith('ocr-job-1');

    component.reset();
    flushMicrotasks();
    expect(component.isPolling()).toBeFalse();
  }));

  it('uses the chosen output folder in the plan', fakeAsync(() => {
    create();
    flushMicrotasks();

    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    tick(250);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(wailsSpy.planJobV1.calls.mostRecent().args[0].outputDir).toBe(
      '/tmp/out',
    );
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/out/scan_ocr.pdf');

    component.useOriginalFolders();
    tick(250);
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(wailsSpy.planJobV1.calls.mostRecent().args[0].outputDir).toBe('');
  }));

  it('seeds the destination from the saved preferences', fakeAsync(() => {
    localStorage.setItem(
      'ff-preferences',
      JSON.stringify({ defaultOutputDir: '/tmp/default' }),
    );

    create();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.outputDir()).toBe('/tmp/default');
    expect(component.destinationLabel()).toBe('/tmp/default');
  }));
});
