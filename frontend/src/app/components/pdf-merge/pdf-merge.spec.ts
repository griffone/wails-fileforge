import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { PdfMerge } from './pdf-merge';
import { ActivityService, ActivityTask } from '../../services/activity.service';
import { I18nService } from '../../services/i18n.service';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('PdfMerge', () => {
  let fixture: ComponentFixture<PdfMerge>;
  let component: PdfMerge;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;
  let jobStateSubject: Subject<JobResult>;

  function fileInfo(path: string): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: 'pdf',
      sizeBytes: 4096,
      kind: 'pdf',
      pageCount: 3,
      exists: true,
    };
  }

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function plannedOutput(request: JobRequest): string {
    const explicit = String(request.options['outputPath'] ?? '');
    if (explicit) {
      return explicit;
    }
    const first = request.inputPaths[0] ?? '';
    const dir = request.outputDir || firstDir(first);
    const stem = (first.split(/[\\/]/).pop() ?? 'document').replace(
      /\.pdf$/i,
      '',
    );
    const name = `${stem}_merged.pdf`;
    return dir ? `${dir}/${name}` : name;
  }

  function planResponse(request: JobRequest): PlanJobResponse {
    const outputPath = plannedOutput(request);
    return {
      success: true,
      message: 'ok',
      outputs: [
        {
          inputPath: request.inputPaths.join(','),
          outputPath,
          outputKind: 'file',
          exists: false,
          adjusted: false,
        },
      ],
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function result(
    jobId: string,
    status: JobResult['status'],
    items: JobResult['items'],
  ): JobStatusResponse {
    return {
      success: true,
      message: status,
      found: true,
      result: {
        jobId,
        success: status === 'success',
        message: status,
        toolId: 'tool.pdf.merge',
        status,
        progress: {
          current: status === 'running' ? 1 : 2,
          total: 2,
          stage: status,
          message: status,
        },
        items,
        startedAt: 1,
        endedAt: status === 'running' ? undefined : 2,
      },
    };
  }

  function runningStatus(jobId: string): JobStatusResponse {
    return result(jobId, 'running', []);
  }

  function successStatus(jobId: string, outputPath: string): JobStatusResponse {
    return result(jobId, 'success', [
      {
        inputPath: '/tmp/a.pdf,/tmp/b.pdf',
        outputPath,
        outputs: [outputPath],
        success: true,
        message: 'PDF merge successful',
      },
    ]);
  }

  function failedStatus(jobId: string): JobStatusResponse {
    return result(jobId, 'failed', [
      {
        inputPath: '/tmp/a.pdf,/tmp/b.pdf',
        outputPath: '',
        success: false,
        message: 'The merge failed.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'PDF_MERGE_FAILED',
          message: 'The merge failed.',
        },
      },
    ]);
  }

  function inputEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'getThumbnail',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
      'listTools',
    ]);

    jobProgressSubject = new Subject<JobProgressEvent>();
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ =
      jobProgressSubject.asObservable();
    jobStateSubject = new Subject<JobResult>();
    (wailsSpy as unknown as { jobState$: unknown }).jobState$ =
      jobStateSubject.asObservable();

    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(runningStatus(jobId)),
    );
    wailsSpy.cancelJob.and.resolveTo({
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
    wailsSpy.listTools.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });

    await TestBed.configureTestingModule({
      imports: [PdfMerge],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(PdfMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
  });

  it('plans two PDFs and enables the primary action, while one PDF stays disabled', fakeAsync(() => {
    component.onPathsSelected(['/tmp/one.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJob).not.toHaveBeenCalled();
    expect(component.canRun()).toBeFalse();
    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    component.onPathsSelected(['/tmp/two.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/one.pdf', '/tmp/two.pdf']);
    expect(request.outputDir).toBe('');
    expect(component.plan()?.success).toBeTrue();
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/one_merged.pdf',
    );
    expect(component.canRun()).toBeTrue();
    expect(component.primaryLabel()).toBe('Merge 2 PDFs');
    expect(primary.disabled).toBeFalse();
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
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('reordering changes the input order in the plan request', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    component.moveDown('/tmp/a.pdf');
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/b.pdf', '/tmp/a.pdf']);
    expect(component.fileItems().map((item) => item.name)).toEqual([
      'b.pdf',
      'a.pdf',
    ]);

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.inputPaths).toEqual(['/tmp/b.pdf', '/tmp/a.pdf']);
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(2);
  }));

  it('sends one page range per input in queue order', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    expect(lastPlanRequest().options['inputPageRanges']).toBeUndefined();

    component.onPageRangeInput('/tmp/a.pdf', inputEvent('1-3'));
    component.onPageRangeInput('/tmp/b.pdf', inputEvent('5'));
    tick(250);
    flushMicrotasks();

    expect(lastPlanRequest().options['inputPageRanges']).toEqual(['1-3', '5']);
  }));

  it('omits page ranges when every file keeps all pages', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    component.onPageRangeInput('/tmp/a.pdf', inputEvent('  '));
    component.onPageRangeInput('/tmp/b.pdf', inputEvent(''));
    tick(250);
    flushMicrotasks();

    expect(lastPlanRequest().options['inputPageRanges']).toBeUndefined();
  }));

  it('keeps page ranges attached to the right file after reordering', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    component.onPageRangeInput('/tmp/a.pdf', inputEvent('1'));
    component.onPageRangeInput('/tmp/b.pdf', inputEvent('2-4'));
    tick(250);
    flushMicrotasks();

    component.moveDown('/tmp/a.pdf');
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/b.pdf', '/tmp/a.pdf']);
    expect(component.pageRanges()).toEqual({
      '/tmp/a.pdf': '1',
      '/tmp/b.pdf': '2-4',
    });
    expect(lastPlanRequest().options['inputPageRanges']).toEqual(['2-4', '1']);
  }));

  it('drops the page range when its file is removed', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf', '/tmp/c.pdf']);
    flushMicrotasks();

    component.onPageRangeInput('/tmp/a.pdf', inputEvent('1-2'));
    component.onPageRangeInput('/tmp/b.pdf', inputEvent('3'));
    component.onPageRangeInput('/tmp/c.pdf', inputEvent('4'));
    tick(250);
    flushMicrotasks();

    component.removeFile('/tmp/a.pdf');
    flushMicrotasks();

    expect(component.pageRanges()).toEqual({
      '/tmp/b.pdf': '3',
      '/tmp/c.pdf': '4',
    });
    expect(lastPlanRequest().inputPaths).toEqual(['/tmp/b.pdf', '/tmp/c.pdf']);
    expect(lastPlanRequest().options['inputPageRanges']).toEqual(['3', '4']);
  }));

  it('renders one row per PDF and marks empty ranges as all pages', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    const rows = fixture.nativeElement.querySelectorAll('.range-row');
    expect(rows.length).toBe(2);
    expect(
      fixture.nativeElement.querySelectorAll('.range-badge').length,
    ).toBe(2);
    expect(
      fixture.nativeElement.querySelector('#pdf-merge-pages-0'),
    ).not.toBeNull();

    component.onPageRangeInput('/tmp/b.pdf', inputEvent('2'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelectorAll('.range-badge').length,
    ).toBe(1);
  }));

  it('blocks the merge when the plan rejects a page range', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    wailsSpy.planJob.and.callFake((request: JobRequest) => {
      const ranges = request.options['inputPageRanges'] as
        | string[]
        | undefined;
      if (ranges?.includes('9')) {
        return Promise.resolve({
          success: false,
          message: 'invalid page range',
          outputs: [],
          error: {
            code: 'PDF_MERGE_PAGE_RANGE_INVALID',
            detail_code: 'PDF_MERGE_PAGE_RANGE_INVALID',
            message: 'invalid page range "9" for a.pdf: page 9 is outside 1-3',
          },
        });
      }
      return Promise.resolve(planResponse(request));
    });

    component.onPageRangeInput('/tmp/a.pdf', inputEvent('9'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.plan()?.success).toBeFalse();
    expect(component.planBlocked()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'page 9 is outside 1-3',
    );
    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('runs through runJob without a validation step and starts polling', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.merge');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(
      (wailsSpy as unknown as { validateJob?: unknown }).validateJob,
    ).toBeUndefined();
    expect(wailsSpy.getJobStatus).toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');

    fixture.destroy();
  }));

  it('drives progress and the terminal result from job events', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.isPolling()).toBeTrue();

    jobProgressSubject.next({
      jobId: 'job-1',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: {
        current: 1,
        total: 2,
        stage: 'merge',
        message: 'Merging 1 of 2',
        etaSeconds: 3,
      },
    });
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('running');
    expect(component.jobResult()?.progress.stage).toBe('merge');
    expect(fixture.nativeElement.textContent).toContain('Merging 1 of 2');

    const completed = successStatus('job-1', '/tmp/a_merged.pdf');
    jobStateSubject.next(completed.result!);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(component.outputInfos()['/tmp/a_merged.pdf'].sizeBytes).toBe(4096);
  }));

  it('resumes an active job after navigation and follows its events', fakeAsync(() => {
    const task: ActivityTask = {
      id: 'job-resumed',
      kind: 'tool',
      title: 'PDF Merge',
      route: '/pdf-merge',
      toolId: 'tool.pdf.merge',
      status: 'running',
      stage: 'merge',
      message: 'Merging 1 of 2',
      current: 1,
      total: 2,
      percent: 50,
      startedAt: 1,
      endedAt: null,
      etaSeconds: 3,
      inputCount: 2,
      outputCount: 0,
      errorMessage: '',
    };
    const activity = TestBed.inject(ActivityService);
    spyOn(activity, 'activeJobForTool').and.returnValue(task);
    wailsSpy.getJobStatus.and.resolveTo(runningStatus('job-resumed'));

    const resumedFixture = TestBed.createComponent(PdfMerge);
    resumedFixture.detectChanges();
    flushMicrotasks();

    const resumed = resumedFixture.componentInstance;
    expect(resumed.activeJobId()).toBe('job-resumed');
    expect(resumed.jobResult()?.status).toBe('running');
    expect(resumed.isPolling()).toBeTrue();

    jobProgressSubject.next({
      jobId: 'job-resumed',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: {
        current: 2,
        total: 2,
        stage: 'finalize',
        message: 'Writing the merged PDF',
      },
    });
    resumedFixture.detectChanges();

    expect(resumed.jobResult()?.progress.stage).toBe('finalize');
    expect(resumedFixture.nativeElement.textContent).toContain(
      'Writing the merged PDF',
    );

    const completed = successStatus('job-resumed', '/tmp/a_merged.pdf');
    jobStateSubject.next(completed.result!);
    flushMicrotasks();
    resumedFixture.detectChanges();

    expect(resumed.terminalJob()?.status).toBe('success');
    expect(resumed.isPolling()).toBeFalse();
    expect(resumed.activeJobId()).toBe('');

    resumedFixture.destroy();
  }));

  it('loads output details on terminal success and opens the result', fakeAsync(() => {
    const outputPath = '/tmp/a_merged.pdf';
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(successStatus('job-2', outputPath));

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');

    const inspectedPaths = wailsSpy.inspectFiles.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some(
        (paths) =>
          paths.length === 1 && paths[0] === outputPath,
      ),
    ).toBeTrue();
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(4096);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('PDF converted');

    void component.onOpen(outputPath);
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);

    void component.onReveal(outputPath);
    flushMicrotasks();
    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith(outputPath);

    fixture.destroy();
  }));

  it('uses the chosen folder in the plan request', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(component.destinationLabel()).toBe('/tmp/out');

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/a_merged.pdf',
    );

    component.useFirstPdfFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to the first PDF');
    const resetRequest = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(resetRequest.outputDir).toBe('');
  }));

  it('builds options.outputPath from a custom file name', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    component.onCustomNameInput({
      target: { value: 'Final.PDF' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['outputPath']).toBe('/tmp/out/Final.pdf');
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/out/Final.pdf');

    component.onCustomNameInput({
      target: { value: 'nested/Final' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(component.customNameInvalid()).toBeTrue();
    expect(request.options['outputPath']).toBeUndefined();
    expect(fixture.nativeElement.textContent).toContain(
      'Use a file name, not a path.',
    );
    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();
  }));

  it('keeps Windows separators when joining a custom name', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('C:\\Out');

    component.onPathsSelected(['C:\\Docs\\a.pdf', 'C:\\Docs\\b.pdf']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    component.onCustomNameInput({
      target: { value: 'Final' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options['outputPath']).toBe('C:\\Out\\Final.pdf');
  }));

  it('keeps the plan as the source of truth when the custom name exists', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    wailsSpy.planJob.and.callFake(() =>
      Promise.resolve({
        success: false,
        message: 'output file already exists',
        outputs: [],
        error: {
          code: 'VALIDATION_INVALID_INPUT',
          detail_code: 'PDF_MERGE_OUTPUT_ALREADY_EXISTS',
          message: 'output file already exists: /tmp/taken.pdf',
        },
      }),
    );

    component.onCustomNameInput({
      target: { value: 'taken' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.plan()?.success).toBeFalse();
    expect(component.planBlocked()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'A file with that name already exists.',
    );

    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('retries a failed merge with the same inputs and options', fakeAsync(() => {
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-3',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(failedStatus('job-3'));

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.terminalJob()?.status).toBe('failed');

    void component.retry();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(2);
    const firstRequest = wailsSpy.runJob.calls.first().args[0] as JobRequest;
    const retryRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(retryRequest.inputPaths).toEqual(firstRequest.inputPaths);
    expect(retryRequest.options).toEqual(firstRequest.options);

    fixture.destroy();
  }));

  it('no longer exposes the old validate step or confirmation modal', () => {
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).not.toContain('Check settings');
    expect(text).not.toContain('Confirm merge order');
    expect(fixture.nativeElement.querySelector('.modal-backdrop')).toBeNull();

    const legacy = component as unknown as Record<string, unknown>;
    expect(legacy['showRunOrderConfirmation']).toBeUndefined();
    expect(legacy['validate']).toBeUndefined();
    expect(legacy['confirmRunOrderAndExecute']).toBeUndefined();
    expect(legacy['cancelRunOrderConfirmation']).toBeUndefined();
  });

  it('reset clears inputs and the custom name but keeps the output folder', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    component.onCustomNameInput({
      target: { value: 'custom' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();

    component.reset();
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.fileItems()).toEqual([]);
    expect(component.customName()).toBe('');
    expect(component.plan()).toBeNull();
    expect(component.outputDir()).toBe('/tmp/out');
  }));

  it('renders the page copy in Spanish', fakeAsync(() => {
    TestBed.inject(I18nService).setLocale('es');

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Unir');
    expect(title).toContain('PDF');
    expect(element.textContent).toContain('Todas las páginas');
    expect(
      element.querySelector('.toolbar .btn-secondary')?.textContent,
    ).toContain('Elegir PDF');
  }));
});
