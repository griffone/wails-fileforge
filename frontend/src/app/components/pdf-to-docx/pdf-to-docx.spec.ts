import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { EMPTY } from 'rxjs';

import { PdfToDocx } from './pdf-to-docx';
import {
  FileInfoV1,
  JobProgressV1,
  JobRequestV1,
  JobResultItemV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('PdfToDocx', () => {
  let fixture: ComponentFixture<PdfToDocx>;
  let component: PdfToDocx;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function queryAll(selector: string): NodeListOf<HTMLElement> {
    return fixture.nativeElement.querySelectorAll(selector) as NodeListOf<HTMLElement>;
  }

  function fileInfo(path: string): FileInfoV1 {
    return {
      path,
      name: path.split(/[\\/]/).pop() ?? path,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 768 * 1024,
      kind: 'pdf',
      pageCount: 4,
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
    const inputPath = request.inputPaths[0] ?? '/tmp/in/document.pdf';
    const dir = request.outputDir || firstDir(inputPath);
    const stem = fileStem(inputPath);

    return {
      success: true,
      message: 'Ready to convert 4 pages to DOCX.',
      outputs: [
        {
          inputPath,
          outputPath: dir ? `${dir}/${stem}.docx` : `${stem}.docx`,
          outputKind: 'file',
          outputCount: 1,
          exists: false,
          adjusted: false,
        },
      ],
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
        toolId: 'tool.pdf.to_docx',
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

  function create(): void {
    fixture = TestBed.createComponent(PdfToDocx);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  function selectPdf(path = '/tmp/in/report.pdf'): void {
    component.onPathsSelected([path]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'inspectFilesV1',
      'openFileDialog',
      'openDirectoryDialog',
      'openPath',
      'revealInFolder',
      'isRuntimeAvailable',
      'listToolsV1',
      'validateJobV1',
    ]);
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ = EMPTY;

    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'docx-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJobV1.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'docx-job-1',
    });
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });
    wailsSpy.isRuntimeAvailable.and.returnValue(false);
    wailsSpy.listToolsV1.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });
    wailsSpy.validateJobV1.and.resolveTo({
      success: true,
      message: 'ok',
      valid: true,
    });

    await TestBed.configureTestingModule({
      imports: [PdfToDocx],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem('ff-preferences');
  });

  it('renders the fidelity contract and plans one PDF as DOCX', fakeAsync(() => {
    create();
    flushMicrotasks();
    fixture.detectChanges();

    const pageText = fixture.nativeElement.textContent as string;
    expect(pageText).toContain('PDF to DOCX');
    expect(pageText).toContain(
      'Turn a PDF into an editable Word document while keeping fonts, weight, colors, images, tables and page layout as close as possible.',
    );
    expect(pageText).toContain('What it preserves');
    expect(pageText).toContain('A real editable Word document, not a screenshot.');
    expect(pageText).toContain(
      'Best for text-based PDFs. Complex multi-column layouts and equations may need touch-ups. Scanned PDFs need OCR first.',
    );

    expect(queryAll('.fidelity-item').length).toBe(6);
    for (const [title, detail] of [
      ['Fonts & sizes', 'Family, size and line height'],
      ['Bold & italic', 'Weight, italic, underline and strike-through'],
      ['Colors & alignment', 'Text color, highlights and paragraph alignment'],
      ['Images', 'Inline and floating images'],
      ['Tables', 'Borders, shading and merged cells'],
      ['Page layout', 'Page size, margins and simple columns'],
    ]) {
      expect(pageText).toContain(title);
      expect(pageText).toContain(detail);
    }

    const ocrLink = query<HTMLAnchorElement>('.fidelity-action');
    expect(ocrLink?.getAttribute('href')).toBe('/pdf-ocr');
    expect(ocrLink?.textContent).toContain('Run PDF OCR');

    expect(pageText).toContain('Drop a PDF here');
    expect(pageText).toContain('PDF only · conversion runs locally');
    expect(component.actionHelper()).toBe('Add one PDF to convert.');

    selectPdf('/tmp/in/report.pdf');

    const request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.to_docx');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/in/report.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({});

    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/in/report.docx',
    ]);
    expect(component.canRun()).toBeTrue();
    expect(component.actionHelper()).toBe('');
    expect(component.pdfCountLabel()).toBe('1 PDF');
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain('report.pdf');
    expect(fixture.nativeElement.textContent).toContain('4 pages');
  }));

  it('opens the native picker for a single PDF', fakeAsync(() => {
    wailsSpy.openFileDialog.and.resolveTo('/tmp/in/picked.pdf');

    create();
    flushMicrotasks();

    void component.chooseFile();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.openFileDialog).toHaveBeenCalled();
    expect(component.selectedPaths()).toEqual(['/tmp/in/picked.pdf']);
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/in/picked.docx');
  }));

  it('rejects non-PDFs and keeps only the first of several dropped PDFs', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/notes.txt']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "notes.txt wasn't added because it isn't a PDF.",
    );
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();

    component.onPathsSelected([
      '/tmp/in/first.pdf',
      '/tmp/in/second.pdf',
      '/tmp/in/notes.txt',
    ]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual(['/tmp/in/first.pdf']);
    expect(component.notice()?.text).toBe(
      'PDF to DOCX converts one PDF at a time. Only first.pdf was added. notes.txt wasn\'t added because it isn\'t a PDF.',
    );
    expect(wailsSpy.planJobV1.calls.mostRecent().args[0].inputPaths).toEqual([
      '/tmp/in/first.pdf',
    ]);
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/in/first.docx');
  }));

  it('shows the engine banner and disables the run when the runtime is missing', fakeAsync(() => {
    const planError = {
      code: 'RUNTIME_DEP_MISSING',
      detail_code: 'PDF_DOCX_RUNTIME_UNAVAILABLE',
      message: 'The PDF to DOCX engine is not installed. Run Install / repair.',
    };
    wailsSpy.planJobV1.and.resolveTo({
      success: false,
      message: planError.message,
      outputs: [],
      error: planError,
    });

    create();
    flushMicrotasks();

    selectPdf('/tmp/in/scan.pdf');

    expect(component.planError()?.detail_code).toBe(
      'PDF_DOCX_RUNTIME_UNAVAILABLE',
    );
    expect(component.planBanner()?.showSettings).toBeTrue();

    const pageText = fixture.nativeElement.textContent as string;
    expect(pageText).toContain('The PDF to DOCX engine is not installed.');
    expect(pageText).toContain('Run Install / repair in Settings, then try again.');

    const settingsLink = query<HTMLAnchorElement>('.plan-banner-action');
    expect(settingsLink?.getAttribute('href')).toBe('/settings');
    expect(settingsLink?.textContent).toContain('Open Settings');

    expect(component.canRun()).toBeFalse();
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeTrue();
    expect(component.actionHelper()).toBe('The DOCX output cannot be planned yet.');

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('runs, polls to terminal and shows the DOCX output with the backend message', fakeAsync(() => {
    const backendMessage =
      'DOCX created: 12 paragraphs, 4 bold runs, 2 images, 1 table.';
    wailsSpy.getJobStatusV1.and.callFake((jobId: string) => {
      const calls = wailsSpy.getJobStatusV1.calls.all().length;
      if (calls <= 1) {
        return Promise.resolve(
          status(jobId, 'running', [], {
            stage: 'processing',
            current: 40,
            total: 100,
            message: 'Converting page 2 of 5',
          }),
        );
      }
      return Promise.resolve(
        status(jobId, 'success', [
          {
            inputPath: '/tmp/in/report.pdf',
            outputPath: '/tmp/in/report.docx',
            outputs: ['/tmp/in/report.docx'],
            outputCount: 1,
            success: true,
            message: backendMessage,
          },
        ]),
      );
    });

    create();
    flushMicrotasks();

    selectPdf('/tmp/in/report.pdf');

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const runRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(runRequest.toolId).toBe('tool.pdf.to_docx');
    expect(runRequest.mode).toBe('single');
    expect(runRequest.options).toEqual({});
    expect(component.activeJobId()).toBe('docx-job-1');
    expect(component.showProgress()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain('Converting page 2 of 5');

    tick(1000);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.resultSummary()?.tone).toBe('success');
    expect(component.resultSummary()?.text).toBe(backendMessage);
    expect(component.isPolling()).toBeFalse();

    const pageText = fixture.nativeElement.textContent as string;
    expect(pageText).toContain(backendMessage);
    expect(pageText).toContain('1 DOCX converted');
    expect(pageText).toContain('report.docx');
    expect(query('app-result-card')).not.toBeNull();

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) => paths.includes('/tmp/in/report.docx')),
    ).toBeTrue();

    (query<HTMLButtonElement>('.result-open') as HTMLButtonElement).click();
    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/in/report.docx');
  }));

  it('uses the chosen output folder in the plan and can go back to the PDF folder', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    create();
    flushMicrotasks();

    selectPdf('/tmp/in/report.pdf');

    void component.chooseOutputFolder();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    let request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/report.docx',
    );

    component.usePdfFolder();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(component.outputDir()).toBe('');
    expect(request.outputDir).toBe('');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/in/report.docx',
    );
  }));

  it('cancels a running job and resets the page', fakeAsync(() => {
    create();
    flushMicrotasks();

    selectPdf('/tmp/in/report.pdf');

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isPolling()).toBeTrue();

    void component.cancel();
    flushMicrotasks();
    expect(wailsSpy.cancelJobV1).toHaveBeenCalledWith('docx-job-1');

    component.reset();
    flushMicrotasks();
    expect(component.isPolling()).toBeFalse();
    expect(component.selectedPaths()).toEqual([]);
    expect(component.plan()).toBeNull();
  }));
});
