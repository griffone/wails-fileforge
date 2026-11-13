import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { PdfMerge } from './pdf-merge';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('PdfMerge', () => {
  let fixture: ComponentFixture<PdfMerge>;
  let component: PdfMerge;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function fileInfo(path: string): FileInfoV1 {
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

  function plannedOutput(request: JobRequestV1): string {
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

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
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
    status: JobResultV1['status'],
    items: JobResultV1['items'],
  ): JobStatusResponseV1 {
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

  function runningStatus(jobId: string): JobStatusResponseV1 {
    return result(jobId, 'running', []);
  }

  function successStatus(jobId: string, outputPath: string): JobStatusResponseV1 {
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

  function failedStatus(jobId: string): JobStatusResponseV1 {
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

  beforeEach(async () => {
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
      Promise.resolve(runningStatus(jobId)),
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

    await TestBed.configureTestingModule({
      imports: [PdfMerge],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(PdfMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans two PDFs and enables the primary action, while one PDF stays disabled', fakeAsync(() => {
    component.onPathsSelected(['/tmp/one.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
    expect(component.canRun()).toBeFalse();
    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    component.onPathsSelected(['/tmp/two.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
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
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
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

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.inputPaths).toEqual(['/tmp/b.pdf', '/tmp/a.pdf']);
    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(2);
  }));

  it('runs through runJobV1 without a validation step and starts polling', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.merge');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(
      (wailsSpy as unknown as { validateJobV1?: unknown }).validateJobV1,
    ).toBeUndefined();
    expect(wailsSpy.getJobStatusV1).toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');

    fixture.destroy();
  }));

  it('loads output details on terminal success and opens the result', fakeAsync(() => {
    const outputPath = '/tmp/a_merged.pdf';
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(successStatus('job-2', outputPath));

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
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

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/a_merged.pdf',
    );

    component.useFirstPdfFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to the first PDF');
    const resetRequest = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
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

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['outputPath']).toBe('/tmp/out/Final.pdf');
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/out/Final.pdf');

    component.onCustomNameInput({
      target: { value: 'nested/Final' },
    } as unknown as Event);
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
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

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['outputPath']).toBe('C:\\Out\\Final.pdf');
  }));

  it('keeps the plan as the source of truth when the custom name exists', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    wailsSpy.planJobV1.and.callFake(() =>
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
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('retries a failed merge with the same inputs and options', fakeAsync(() => {
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-3',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(failedStatus('job-3'));

    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(component.terminalJob()?.status).toBe('failed');

    void component.retry();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(2);
    const firstRequest = wailsSpy.runJobV1.calls.first().args[0] as JobRequestV1;
    const retryRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
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
});
