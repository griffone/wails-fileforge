import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { PdfOptimize } from './pdf-optimize';
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

describe('PdfOptimize', () => {
  let fixture: ComponentFixture<PdfOptimize>;
  let component: PdfOptimize;
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
      const name = `${fileStem(inputPath)}_optimized.pdf`;
      return {
        inputPath,
        outputPath: dir ? `${dir}/${name}` : name,
        outputKind: 'file',
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
        toolId: 'tool.pdf.optimize',
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

  function inputEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function lastPlanRequest(): JobRequestV1 {
    return wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
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

    await TestBed.configureTestingModule({
      imports: [PdfOptimize],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(PdfOptimize);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans one PDF with default compression and no metadata', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.pdf.optimize');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options).toEqual({});
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/a_optimized.pdf',
    );
    expect(component.primaryLabel()).toBe('Optimize 1 PDF');
    expect(component.canRun()).toBeTrue();
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

  it('adds and removes PDFs, switching between single and batch mode', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(lastPlanRequest().mode).toBe('batch');
    expect(component.primaryLabel()).toBe('Optimize 2 PDFs');
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.plan()?.outputs[1].outputPath).toBe(
      '/tmp/b_optimized.pdf',
    );

    component.removeFile('/tmp/a.pdf');
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/b.pdf']);
    expect(lastPlanRequest().mode).toBe('single');
    expect(component.primaryLabel()).toBe('Optimize 1 PDF');
  }));

  it('sends trimmed metadata and the compression opt-out', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.onCompressToggle({
      target: { checked: false },
    } as unknown as Event);
    flushMicrotasks();

    component.onMetadataInput('title', inputEvent('  Annual report  '));
    component.onMetadataInput('keywords', inputEvent('pdf, report'));
    component.onMetadataInput('author', inputEvent('   '));
    tick(250);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options['compress']).toBeFalse();
    expect(request.options['metadata']).toEqual({
      title: 'Annual report',
      keywords: 'pdf, report',
    });
    expect(request.mode).toBe('single');
  }));

  it('omits compression and metadata when nothing changes them', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.onMetadataInput('title', inputEvent('  '));
    component.onMetadataInput('creator', inputEvent(''));
    tick(250);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options['compress']).toBeUndefined();
    expect(request.options['metadata']).toBeUndefined();
  }));

  it('resets compression and metadata to the defaults', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.onCompressToggle({
      target: { checked: false },
    } as unknown as Event);
    component.onMetadataInput('title', inputEvent('Draft'));
    tick(250);
    flushMicrotasks();

    component.resetAdvanced();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.compress()).toBeTrue();
    expect(component.metadata()['title']).toBe('');
    expect(lastPlanRequest().options).toEqual({});
    expect(
      (fixture.nativeElement.querySelector('#pdf-optimize-compress') as HTMLInputElement)
        .checked,
    ).toBeTrue();
  }));

  it('blocks the action and clears the plan for oversized metadata', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.onMetadataInput('subject', inputEvent('a'.repeat(513)));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.metadataInvalid()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();
    expect(component.actionHelper()).toBe(
      'Shorten each metadata value to 512 characters or fewer.',
    );
    expect(
      (fixture.nativeElement.querySelector('.action-primary') as HTMLButtonElement)
        .disabled,
    ).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
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

    let request = lastPlanRequest();
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual([
      '/tmp/out/a_optimized.pdf',
      '/tmp/out/b_optimized.pdf',
    ]);

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each PDF');
    request = lastPlanRequest();
    expect(request.outputDir).toBe('');
  }));

  it('keeps a failed plan as the source of truth and blocks the run', fakeAsync(() => {
    wailsSpy.planJobV1.and.resolveTo({
      success: false,
      message: 'options.metadata.title must be at most 512 characters',
      outputs: [],
      error: {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'PDF_OPTIMIZE_METADATA_INVALID',
        message: 'options.metadata.title must be at most 512 characters',
      },
    });

    component.onPathsSelected(['/tmp/a.pdf']);
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
    expect(request.toolId).toBe('tool.pdf.optimize');
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

  it('renders the optimized result and opens the output', fakeAsync(() => {
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(
      status('job-2', 'success', [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/a_optimized.pdf',
          outputs: ['/tmp/a_optimized.pdf'],
          outputCount: 1,
          success: true,
          message: 'PDF optimized',
        },
      ]),
    );

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('1 PDF optimized');
    expect(text).toContain('a_optimized.pdf');

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) =>
        paths.includes('/tmp/a_optimized.pdf'),
      ),
    ).toBeTrue();

    (fixture.nativeElement.querySelector('.result-open') as HTMLButtonElement).click();
    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/a_optimized.pdf');
  }));

  it('renders the workbench header and no legacy validate step', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector('.eyebrow')?.textContent).toContain('PDF');
    expect(element.querySelector('.page-title')?.textContent).toContain('PDF');
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Optimize',
    );
    expect(element.querySelectorAll('.page-head a').length).toBe(0);

    const text = element.textContent as string;
    expect(text).toContain(
      'Compress PDFs and edit document metadata. Everything runs on this machine.',
    );
    expect(text).toContain('Leave blank to keep the existing value.');

    const legacy = component as unknown as Record<string, unknown>;
    expect(legacy['validate']).toBeUndefined();
  });
});
