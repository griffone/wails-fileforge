import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { EMPTY, Subject } from 'rxjs';

import { ImageOcr } from './image-ocr';
import { DropIntakeService } from '../../services/drop-intake.service';
import {
  FileInfo,
  JobRequest,
  JobResultItem,
  JobResult,
  JobStatusResponse,
  OCRCapabilitiesResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('ImageOcr', () => {
  let fixture: ComponentFixture<ImageOcr>;
  let component: ImageOcr;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobStateSubject: Subject<JobResult>;

  function query<T extends Element>(selector: string): T | null {
    return fixture.nativeElement.querySelector(selector) as T | null;
  }

  function capabilities(
    overrides: Partial<OCRCapabilitiesResponse> = {},
  ): OCRCapabilitiesResponse {
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

  function fileInfo(path: string, sizeBytes = 2048): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    const extension = name.split('.').pop() ?? '';
    return {
      path,
      name,
      directory: '/tmp/in',
      extension,
      sizeBytes,
      kind: extension === 'pdf' ? 'pdf' : 'image',
      width: 640,
      height: 480,
      exists: true,
    };
  }

  function inputEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function checkedEvent(checked: boolean): Event {
    return { target: { checked } } as unknown as Event;
  }

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function planResponse(request: JobRequest): PlanJobResponse {
    const writePdf = request.options['writePdf'] === true;
    const outputs = request.inputPaths.flatMap((inputPath) => {
      const stem = inputPath.replace(/\.[^./\\]+$/, '');
      const text = {
        inputPath,
        outputPath: `${stem}.txt`,
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      };
      if (!writePdf) {
        return [text];
      }
      return [
        text,
        {
          inputPath,
          outputPath: `${stem}_ocr.pdf`,
          outputKind: 'file',
          outputCount: 1,
          exists: false,
          adjusted: false,
        },
      ];
    });

    return {
      success: true,
      message: 'Ready to OCR 1 image.',
      outputs,
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function status(
    jobId: string,
    jobStatus: JobResult['status'],
    items: JobResultItem[],
  ): JobStatusResponse {
    return {
      success: true,
      message: jobStatus,
      found: true,
      result: {
        jobId,
        success: jobStatus === 'success',
        message: jobStatus,
        toolId: 'tool.image.ocr',
        status: jobStatus,
        progress: {
          current: items.length,
          total: items.length || 1,
          stage: jobStatus,
          message: jobStatus,
        },
        items,
        startedAt: 1,
        endedAt: jobStatus === 'running' ? undefined : 2,
      },
    };
  }

  /** Creates the page with the given capabilities and renders the first frame. */
  function create(caps: OCRCapabilitiesResponse = capabilities()): void {
    wailsSpy.getOCRCapabilities.and.resolveTo(caps);
    fixture = TestBed.createComponent(ImageOcr);
    component = fixture.componentInstance;
    fixture.detectChanges();
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'getOCRCapabilities',
      'inspectFiles',
      'getThumbnail',
      'planJob',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openDirectoryDialog',
      'openMultipleFilesDialog',
      'openPath',
      'revealInFolder',
      'verifyJob',
    ]);
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ = EMPTY;
    jobStateSubject = new Subject<JobResult>();
    (wailsSpy as unknown as { jobState$: unknown }).jobState$ =
      jobStateSubject.asObservable();

    wailsSpy.getOCRCapabilities.and.resolveTo(capabilities());
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => fileInfo(path)),
      }),
    );
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'image-ocr-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancel requested',
      jobId: 'image-ocr-job-1',
    });
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });
    wailsSpy.verifyJob.and.resolveTo({
      success: false,
      message: 'unavailable',
      outputs: [],
    });

    await TestBed.configureTestingModule({
      imports: [ImageOcr],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();
  });

  afterEach(() => {
    fixture?.destroy();
    localStorage.removeItem('ff-preferences');
  });

  it('is available without pdftoppm and selects the recommended language', fakeAsync(() => {
    create(
      capabilities({
        available: false,
        pdfToPpm: false,
        tesseract: true,
        languages: ['afr', 'spa'],
        recommendedLanguage: 'spa',
      }),
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getOCRCapabilities).toHaveBeenCalled();
    expect(component.ocrAvailable()).toBeTrue();
    expect(component.ocrBlocked()).toBeFalse();
    expect(component.capabilityBanner()).toBeNull();
    expect(component.language()).toBe('spa');
    expect(component.languages()).toEqual(['afr', 'spa']);

    const select = query<HTMLSelectElement>('#image-ocr-language');
    expect(select).not.toBeNull();
    expect(select?.disabled).toBeFalse();
    expect(select?.value).toBe('spa');
    expect(select?.options.length).toBe(2);
    expect(select?.options[0].textContent?.trim()).toBe('AFR');
    expect(select?.options[1].textContent?.trim()).toBe('SPA');

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Image OCR');
    expect(text).toContain('Drop images here');
    expect(text).not.toContain('Local image OCR is unavailable.');
    expect(component.actionHelper()).toBe('Add at least one image.');
  }));

  it('falls back to the first installed language without a recommendation', fakeAsync(() => {
    create(capabilities({ languages: ['afr'], recommendedLanguage: '' }));
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.language()).toBe('afr');
    expect(component.languageOptions()).toEqual([{ value: 'afr', label: 'AFR' }]);
  }));

  it('blocks the plan and run when Tesseract is missing', fakeAsync(() => {
    create(
      capabilities({
        available: false,
        pdfToPpm: true,
        tesseract: false,
        pdfToText: false,
        languages: ['eng'],
        recommendedLanguage: 'eng',
      }),
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.ocrAvailable()).toBeFalse();
    expect(component.ocrBlocked()).toBeTrue();
    expect(component.missingPieces()).toEqual(['Tesseract']);
    expect(component.capabilityBanner()?.title).toBe(
      'Local image OCR is unavailable.',
    );

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Local image OCR is unavailable.');
    expect(text).toContain('Missing Tesseract.');

    component.onPathsSelected(['/tmp/in/scan.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Local image OCR is unavailable on this machine.',
    );
    expect(query<HTMLButtonElement>('.action-primary')?.disabled).toBeTrue();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('blocks the plan and run when no language data is installed', fakeAsync(() => {
    create(
      capabilities({
        // `available` requires pdftoppm on the shared DTO; ignoring it here is
        // the point of the image OCR gate.
        available: true,
        pdfToPpm: true,
        tesseract: true,
        languages: [],
        recommendedLanguage: '',
      }),
    );
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.ocrAvailable()).toBeFalse();
    expect(component.ocrBlocked()).toBeTrue();
    expect(component.capabilityBanner()?.detail).toContain(
      'No Tesseract language data is installed; install eng or spa.',
    );

    component.onPathsSelected(['/tmp/in/scan.png']);
    tick(250);
    flushMicrotasks();

    expect(wailsSpy.planJob).not.toHaveBeenCalled();
    expect(component.canRun()).toBeFalse();
  }));

  it('plans with the default language, PDF off and DPI 300', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/scan.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.ocr');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/in/scan.png']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({
      language: 'eng',
      writePdf: false,
      dpi: 300,
    });
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/in/scan.txt',
    ]);
    expect(component.primaryLabel()).toBe('OCR 1 image');
  }));

  it('sends the searchable PDF toggle and the advanced DPI', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/scan.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(query<HTMLInputElement>('#image-ocr-write-pdf')?.checked).toBeFalse();

    component.onWritePdfToggle(checkedEvent(true));
    component.onDpiInput(inputEvent('450'));
    tick(250);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options).toEqual({
      language: 'eng',
      writePdf: true,
      dpi: 450,
    });
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/in/scan.txt',
      '/tmp/in/scan_ocr.pdf',
    ]);
    fixture.detectChanges();
    expect(query<HTMLInputElement>('#image-ocr-write-pdf')?.checked).toBeTrue();
  }));

  it('clamps the advanced DPI to the supported range and resets it', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onDpiInput(inputEvent('10'));
    expect(component.dpi()).toBe(150);

    component.onDpiInput(inputEvent('900'));
    expect(component.dpi()).toBe(600);

    component.onPathsSelected(['/tmp/in/scan.png']);
    tick(250);
    flushMicrotasks();
    expect(lastPlanRequest().options['dpi']).toBe(600);

    component.resetAdvanced();
    tick(250);
    flushMicrotasks();
    expect(component.dpi()).toBe(300);
    expect(lastPlanRequest().options['dpi']).toBe(300);
  }));

  it('runs, observes to terminal and shows the recognized summary', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/screenshot.png']);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.getJobStatus.and.resolveTo(
      status('image-ocr-job-1', 'running', []),
    );
    const completed = status('image-ocr-job-1', 'success', [
      {
        inputPath: '/tmp/in/screenshot.png',
        outputPath: '/tmp/in/screenshot.txt',
        outputs: ['/tmp/in/screenshot.txt', '/tmp/in/screenshot_ocr.pdf'],
        success: true,
        message: '1,234 characters recognized',
      },
    ]);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.image.ocr');
    expect(request.options).toEqual({
      language: 'eng',
      writePdf: false,
      dpi: 300,
    });
    expect(component.isPolling()).toBeTrue();

    jobStateSubject.next(completed.result!);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.isPolling()).toBeFalse();
    expect(component.recognizedSummary()).toEqual({
      tone: 'success',
      text: '1,234 characters recognized',
    });

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('1,234 characters recognized');
    expect(text).toContain('1 image recognized');

    component.onOpen('/tmp/in/screenshot.txt');
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/in/screenshot.txt');
  }));

  it('shows a warning summary when no text was recognized', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/in/blank.png']);
    tick(250);
    flushMicrotasks();

    wailsSpy.getJobStatus.and.resolveTo(
      status('image-ocr-job-1', 'success', [
        {
          inputPath: '/tmp/in/blank.png',
          outputPath: '/tmp/in/blank.txt',
          outputs: ['/tmp/in/blank.txt'],
          success: true,
          message: 'No text was recognized.',
        },
      ]),
    );

    void component.run();
    flushMicrotasks();
    tick(1000);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.recognizedSummary()).toEqual({
      tone: 'warning',
      text: 'No text was recognized.',
    });
    const summary = query<HTMLElement>('.recognized-summary');
    expect(summary?.classList.contains('banner-warning')).toBeTrue();
    expect(summary?.textContent).toContain('No text was recognized.');
  }));

  it('rejects unsupported extensions with a notice', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected(['/tmp/one.heic']);
    tick(250);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "1 file wasn't added. HEIC isn't supported.",
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('uses batch mode for several images', fakeAsync(() => {
    create();
    flushMicrotasks();

    component.onPathsSelected([
      '/tmp/in/a.png',
      '/tmp/in/b.jpg',
      '/tmp/in/c.heic',
    ]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/in/a.png', '/tmp/in/b.jpg']);
    expect(component.primaryLabel()).toBe('OCR 2 images');
  }));

  it('consumes queued drop intake paths on init', fakeAsync(() => {
    TestBed.inject(DropIntakeService).queue('/image-ocr', [
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);

    create();
    flushMicrotasks();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([
      '/tmp/queued-one.png',
      '/tmp/queued-two.jpg',
    ]);
    expect(wailsSpy.planJob).toHaveBeenCalled();
  }));
});
