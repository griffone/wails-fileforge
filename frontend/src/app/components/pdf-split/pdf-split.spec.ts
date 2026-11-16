import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Subject } from 'rxjs';

import { PdfSplit } from './pdf-split';
import { PdfSplitPreview } from './pdf-split-preview';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultItemV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('PdfSplit', () => {
  let fixture: ComponentFixture<PdfSplit>;
  let component: PdfSplit;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function fileInfo(path: string): FileInfoV1 {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: 'pdf',
      sizeBytes: 2 * 1024 * 1024,
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
    const outputs = request.inputPaths.map((inputPath) => {
      const dir = request.outputDir || firstDir(inputPath);
      const name = `${fileStem(inputPath)}_pages`;
      return {
        inputPath,
        outputPath: dir ? `${dir}/${name}` : name,
        outputKind: 'directory',
        outputCount: 3,
        exists: false,
        adjusted: false,
      };
    });

    return {
      success: true,
      message: 'ok',
      outputs,
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function status(
    jobId: string,
    jobStatus: JobResultV1['status'],
    items: JobResultItemV1[],
  ): JobStatusResponseV1 {
    return {
      success: true,
      message: jobStatus,
      found: true,
      result: {
        jobId,
        success: jobStatus === 'success',
        message: jobStatus,
        toolId: 'tool.pdf.split',
        status: jobStatus,
        progress: {
          current: jobStatus === 'running' ? 1 : items.length,
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

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJobV1',
      'inspectFilesV1',
      'getThumbnail',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
      'listToolsV1',
      'getPdfPreviewSource',
    ]);

    jobProgressSubject = new Subject<JobProgressEventV1>();
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ =
      jobProgressSubject.asObservable();

    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJobV1.and.resolveTo({
      success: true,
      message: 'cancelled',
      jobId: 'job-1',
    });
    wailsSpy.openPath.and.resolveTo({ success: true, message: 'opened' });
    wailsSpy.revealInFolder.and.resolveTo({
      success: true,
      message: 'revealed',
    });
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.isRuntimeAvailable.and.returnValue(true);
    wailsSpy.listToolsV1.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });
    wailsSpy.getPdfPreviewSource.and.resolveTo({
      success: false,
      message: 'Preview disabled in tests.',
    });

    await TestBed.configureTestingModule({
      imports: [PdfSplit],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(PdfSplit);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans one PDF as single and two PDFs as batch', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.split');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options['strategy']).toBe('every_page');
    expect(request.options['ranges']).toBeUndefined();
    expect(component.plan()?.outputs[0]).toEqual(
      jasmine.objectContaining({
        outputPath: '/tmp/a_pages',
        outputKind: 'directory',
        outputCount: 3,
      }),
    );
    expect(component.primaryLabel()).toBe('Split 1 PDF');
    expect(component.canRun()).toBeTrue();
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeFalse();

    component.onPathsSelected(['/tmp/b.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.plan()?.outputs[1].outputPath).toBe('/tmp/b_pages');
    expect(component.primaryLabel()).toBe('Split 2 PDFs');
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeFalse();
  }));

  it('rejects non-PDFs and duplicates with a notice', fakeAsync(() => {
    component.onPathsSelected([
      '/tmp/notes.txt',
      '/tmp/a.pdf',
      '/tmp/a.pdf',
    ]);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.pdf']);
    expect(component.notice()?.text).toContain('notes.txt');
    expect(component.notice()?.text).toContain("isn't a PDF");
    expect(component.notice()?.text).toContain('a.pdf is already in the queue.');
  }));

  it('shows PDF metadata and thumbnails in the queue', fakeAsync(() => {
    wailsSpy.getThumbnail.and.resolveTo('data:image/png;base64,abc');

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.fileItems()[0].meta).toContain('2 MB');
    expect(component.fileItems()[0].meta).toContain('4 pages');
    expect(fixture.nativeElement.querySelector('.file-queue-image')).not.toBeNull();
  }));

  it('shows the resulting-files preview only when PDFs are selected', fakeAsync(() => {
    expect(
      fixture.nativeElement.querySelector('app-pdf-split-preview'),
    ).toBeNull();

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelector('app-pdf-split-preview'),
    ).not.toBeNull();
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Resulting files');
    expect(text).toContain('Preview every file the split will create.');
  }));

  it('sends the chosen strategy and ranges in the plan request', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    component.selectStrategy('ranges');
    flushMicrotasks();
    expect(component.plan()).toBeNull();

    component.onRangesInput({
      target: { value: '1-3,5' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.mode).toBe('batch');
    expect(request.options).toEqual(
      jasmine.objectContaining({ strategy: 'ranges', ranges: '1-3,5' }),
    );

    component.selectStrategy('every_page');
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options['strategy']).toBe('every_page');
    expect(request.options['ranges']).toBeUndefined();
  }));

  it('disables the primary action until page ranges are entered', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('ranges');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.rangesRequired()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toContain('Enter the page ranges');
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeTrue();

    component.onRangesInput({
      target: { value: '1-2' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.rangesRequired()).toBeFalse();
    expect(component.canRun()).toBeTrue();
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeFalse();
  }));

  it('offers a selected-pages strategy with a selectable preview and no ranges input', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    const presets = Array.from(
      fixture.nativeElement.querySelectorAll('.strategy-card'),
    ) as HTMLElement[];
    expect(presets.length).toBe(5);
    expect(presets[2].textContent).toContain('Selected pages');
    expect(presets[2].textContent).toContain(
      'Pick pages; consecutive ones group together',
    );
    expect(presets[3].textContent).toContain('Pages per file');
    expect(presets[3].textContent).toContain('Split every N pages.');
    expect(presets[4].textContent).toContain('Bookmarks');
    expect(presets[4].textContent).toContain(
      'One file per top-level bookmark.',
    );

    component.selectStrategy('selected');
    flushMicrotasks();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('#pdf-split-ranges')).toBeNull();

    const preview = fixture.debugElement.query(By.directive(PdfSplitPreview))
      .componentInstance as PdfSplitPreview;
    expect(preview.strategy).toBe('selected');
    expect(preview.selectable).toBeTrue();
    expect(component.activeStrategyLabel()).toBe('Selected pages');
  }));

  it('plans pages per file with the default chunk size of 5', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('pages_per_file');
    flushMicrotasks();
    fixture.detectChanges();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['strategy']).toBe('pages_per_file');
    expect(request.options['pagesPerFile']).toBe(5);
    expect(request.options['ranges']).toBeUndefined();
    expect(component.pagesPerFileInvalid()).toBeFalse();
    expect(component.canRun()).toBeTrue();
    expect(component.primaryLabel()).toBe('Split 1 PDF into 3 parts');

    const preview = fixture.debugElement.query(By.directive(PdfSplitPreview))
      .componentInstance as PdfSplitPreview;
    expect(preview.strategy).toBe('pages_per_file');
    expect(preview.pagesPerFile).toBe(5);

    component.onPagesPerFileInput({
      target: { value: '12' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.options['pagesPerFile']).toBe(12);
  }));

  it('blocks the action and clears the plan for invalid chunk sizes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('pages_per_file');
    flushMicrotasks();

    component.onPagesPerFileInput({
      target: { value: '0' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.pagesPerFileInvalid()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();
    expect(component.actionHelper()).toBe(
      'Enter a whole number of pages from 1 to 1000.',
    );
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeTrue();

    component.onPagesPerFileInput({
      target: { value: '1001' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    expect(component.pagesPerFileInvalid()).toBeTrue();
    expect(component.canRun()).toBeFalse();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('plans bookmarks without extra split options', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    component.selectStrategy('bookmarks');
    flushMicrotasks();
    fixture.detectChanges();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.mode).toBe('batch');
    expect(request.options['strategy']).toBe('bookmarks');
    expect(request.options['pagesPerFile']).toBeUndefined();
    expect(request.options['ranges']).toBeUndefined();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain(
      "Uses the PDF's top-level outline. If it has no bookmarks, the plan will explain what to do.",
    );
    expect(component.activeStrategyLabel()).toBe('Bookmarks');

    const preview = fixture.debugElement.query(By.directive(PdfSplitPreview))
      .componentInstance as PdfSplitPreview;
    expect(preview.strategy).toBe('bookmarks');
  }));

  it('blocks the split when the PDF has no bookmarks', fakeAsync(() => {
    wailsSpy.planJobV1.and.resolveTo({
      success: false,
      message: 'PDF has no bookmarks to split by',
      outputs: [],
      error: {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'PDF_SPLIT_NO_BOOKMARKS',
        message: 'PDF has no bookmarks to split by',
      },
    });

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('bookmarks');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.planBlocked()).toBeTrue();
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('This PDF has no bookmarks.');
    expect(text).toContain('Choose Every page');
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('groups consecutive picked pages into backend ranges', () => {
    expect(component.pagesToRanges([1, 2, 3, 5, 7, 8])).toBe('1-3,5,7-8');
    expect(component.pagesToRanges([3, 1, 2, 2])).toBe('1-3');
    expect(component.pagesToRanges([])).toBe('');
  });

  it('plans picked pages as ranges while keeping the selected badge', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('selected');
    flushMicrotasks();

    component.onSelectedPagesChange([1, 2, 4]);
    tick(250);
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options).toEqual(
      jasmine.objectContaining({ strategy: 'ranges', ranges: '1-2,4' }),
    );
    expect(component.strategy()).toBe('selected');
    expect(component.activeStrategyLabel()).toBe('Selected pages');
    expect(component.plan()?.outputs.length).toBe(1);
    expect(component.primaryLabel()).toBe('Split 1 PDF into 2 parts');
  }));

  it('disables the split and clears the plan without picked pages', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('selected');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectionRequired()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();
    expect(component.actionHelper()).toBe(
      'Select at least one page to build the split.',
    );
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeTrue();

    component.onSelectedPagesChange([2, 1]);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPages()).toEqual([1, 2]);
    expect(component.selectionRequired()).toBeFalse();
    expect(component.canRun()).toBeTrue();
    expect(component.primaryLabel()).toBe('Split 1 PDF into 1 part');
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeFalse();

    component.onSelectedPagesChange([]);
    fixture.detectChanges();

    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
  }));

  it('runs a picked-pages split with the built ranges', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('selected');
    component.onSelectedPagesChange([1, 2, 4]);
    tick(250);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options).toEqual(
      jasmine.objectContaining({ strategy: 'ranges', ranges: '1-2,4' }),
    );
  }));

  it('keeps the plan as the source of truth when ranges are rejected', fakeAsync(() => {
    wailsSpy.planJobV1.and.resolveTo({
      success: false,
      message: 'range out of bounds',
      outputs: [],
      error: {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'PDF_SPLIT_RANGE_OUT_OF_BOUNDS',
        message: 'Page range 9-12 is outside this PDF (4 pages).',
      },
    });

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectStrategy('ranges');
    component.onRangesInput({
      target: { value: '9-12' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.plan()?.success).toBeFalse();
    expect(component.planBlocked()).toBeTrue();
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('plans and runs without a validation step, then polls', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.split');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(2);
    expect(
      (wailsSpy as unknown as { validateJobV1?: unknown }).validateJobV1,
    ).toBeUndefined();
    expect(wailsSpy.getJobStatusV1).toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');
  }));

  it('renders a grouped result for a large split and reveals the folder', fakeAsync(() => {
    const outputs = Array.from(
      { length: 6 },
      (_, index) => `/tmp/a_pages/page_${index + 1}.pdf`,
    );
    const completed = status('job-2', 'success', [
      {
        inputPath: '/tmp/a.pdf',
        outputPath: '/tmp/a_pages',
        outputs,
        outputCount: 6,
        success: true,
        message: 'PDF split successful: generated 6 files',
      },
    ]);

    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(completed);

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('1 PDF split');
    expect(text).toContain('6 files in a_pages');
    expect(text).not.toContain('page_1.pdf');
    expect(fixture.nativeElement.querySelectorAll('.result-row').length).toBe(1);

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) =>
        paths.includes('/tmp/a_pages/page_1.pdf'),
      ),
    ).toBeFalse();

    const showFolder = fixture.nativeElement.querySelector(
      '.result-show-folder',
    ) as HTMLButtonElement;
    showFolder.click();

    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith('/tmp/a_pages');
  }));

  it('uses the chosen destination folder in the plan', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/out/a_pages',
      '/tmp/out/b_pages',
    ]);

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each PDF');
    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.outputDir).toBe('');
  }));

  it('retries only failed inputs with the same strategy, ranges and folder', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-3',
      status: 'queued',
    });
    const partial = status('job-3', 'partial_success', [
      {
        inputPath: '/tmp/a.pdf',
        outputPath: '/tmp/out/a_pages',
        outputs: ['/tmp/out/a_pages/page_1.pdf'],
        outputCount: 1,
        success: true,
        message: 'ok',
      },
      {
        inputPath: '/tmp/b.pdf',
        outputPath: '',
        success: false,
        message: 'The split failed.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PDF_SPLIT_FAILED',
          message: 'The split failed.',
        },
      },
    ]);
    wailsSpy.getJobStatusV1.and.resolveTo(partial);

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    component.selectStrategy('ranges');
    component.onRangesInput({
      target: { value: '1-3' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.terminalJob()?.status).toBe('partial_success');

    void component.retryFailed();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(2);
    const retryRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(retryRequest.inputPaths).toEqual(['/tmp/b.pdf']);
    expect(retryRequest.mode).toBe('single');
    expect(retryRequest.options).toEqual(
      jasmine.objectContaining({ strategy: 'ranges', ranges: '1-3' }),
    );
    expect(retryRequest.outputDir).toBe('/tmp/out');
  }));

  it('renders the workbench header and no legacy validate step', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector('.eyebrow')?.textContent).toContain('PDF');
    expect(element.querySelector('.page-title')?.textContent).toContain('PDF');
    expect(element.querySelector('.page-title')?.textContent).toContain('Split');
    expect(element.querySelectorAll('.page-head a').length).toBe(0);

    const text = element.textContent as string;
    expect(text).toContain(
      'Split PDFs by page, explicit ranges, picked pages, page count or bookmarks.',
    );
    expect(text).not.toContain('Split preview');

    const legacy = component as unknown as Record<string, unknown>;
    expect(legacy['validate']).toBeUndefined();
    expect(legacy['panelFields']).toBeUndefined();
  });
});
