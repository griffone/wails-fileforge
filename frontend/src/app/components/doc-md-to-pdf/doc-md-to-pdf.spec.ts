import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { DocMdToPdf } from './doc-md-to-pdf';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('DocMdToPdf', () => {
  let fixture: ComponentFixture<DocMdToPdf>;
  let component: DocMdToPdf;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function fileInfo(path: string): FileInfoV1 {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: 'md',
      sizeBytes: 2048,
      kind: 'doc',
      exists: true,
    };
  }

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function plannedOutput(request: JobRequestV1): string {
    const first = request.inputPaths[0] ?? '';
    const dir = request.outputDir || firstDir(first);
    const stem = (first.split(/[\\/]/).pop() ?? 'document').replace(
      /\.(md|markdown)$/i,
      '',
    );
    const name = `${stem}_md2pdf.pdf`;
    return dir ? `${dir}/${name}` : name;
  }

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    const outputPath = plannedOutput(request);
    return {
      success: true,
      message: 'ok',
      outputs: [
        {
          inputPath: request.inputPaths[0],
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
        toolId: 'tool.doc.md_to_pdf',
        status,
        progress: {
          current: status === 'running' ? 0 : 1,
          total: 1,
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

  function lastPlanRequest(): JobRequestV1 {
    return wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
  }

  function successStatus(
    jobId: string,
    outputPath: string,
  ): JobStatusResponseV1 {
    return result(jobId, 'success', [
      {
        inputPath: '/tmp/notes.md',
        outputPath,
        outputs: [outputPath],
        success: true,
        message: 'markdown to pdf successful',
      },
    ]);
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
      'openFileDialog',
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
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.isRuntimeAvailable.and.returnValue(true);
    wailsSpy.listToolsV1.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });

    await TestBed.configureTestingModule({
      imports: [DocMdToPdf],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(DocMdToPdf);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans a single Markdown conversion with the destination folder', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.md']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.doc.md_to_pdf');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/notes.md']);
    expect(request.outputDir).toBe('');

    expect(component.selectedPaths()).toEqual(['/tmp/notes.md']);
    expect(component.fileItems()[0].meta).toBe('2 KB');
    expect(component.plan()?.success).toBeTrue();
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/notes_md2pdf.pdf',
    );
    expect(component.destinationLabel()).toBe('Next to the Markdown file');
    expect(component.canRun()).toBeTrue();
    expect(component.primaryLabel()).toBe('Convert to PDF');
  }));

  it('rejects non-Markdown files with a notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toContain('photo.png');
    expect(component.notice()?.text).toContain("isn't a Markdown file");
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();

    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();
  }));

  it('includes enabled header and footer values in the plan request', fakeAsync(() => {
    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();

    component.form.controls.header.patchValue({
      enabled: true,
      text: 'H {page}/{totalPages}',
      align: 'center',
      font: 'times',
      marginTop: 4,
      marginBottom: 2,
      color: '#112233',
    });
    tick(250);
    flushMicrotasks();

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['header']).toEqual({
      enabled: true,
      text: 'H {page}/{totalPages}',
      align: 'center',
      font: 'times',
      marginTop: 4,
      marginBottom: 2,
      color: '#112233',
    });
    expect(request.options['footer']).toEqual({
      enabled: false,
      text: '',
      align: 'left',
      font: 'helvetica',
      marginTop: 0,
      marginBottom: 0,
      color: '#000000',
    });
    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(2);
  }));

  it('always sends the resolved page setup in the plan request', fakeAsync(() => {
    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.options['pageSize']).toBe('A4');
    expect(request.options['orientation']).toBe('P');
    expect(request.options['margins']).toEqual({
      top: 20,
      right: 15,
      bottom: 18,
      left: 15,
    });
    expect(component.pageSetupSummary()).toBe(
      'A4 · Portrait · 20/15/18/15 mm margins',
    );
  }));

  it('refreshes the plan immediately when the page size or orientation change', fakeAsync(() => {
    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();
    const plannedOnce = wailsSpy.planJobV1.calls.count();

    const size = fixture.nativeElement.querySelector(
      '#md-page-size',
    ) as HTMLSelectElement;
    size.value = 'Letter';
    size.dispatchEvent(new Event('change'));
    flushMicrotasks();

    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);
    expect(lastPlanRequest().options['pageSize']).toBe('Letter');

    const orientation = fixture.nativeElement.querySelector(
      '#md-orientation',
    ) as HTMLSelectElement;
    orientation.value = 'L';
    orientation.dispatchEvent(new Event('change'));
    flushMicrotasks();

    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 2);
    expect(lastPlanRequest().options['orientation']).toBe('L');
    expect(component.pageSetupSummary()).toContain('Letter · Landscape');
  }));

  it('debounces margin edits before planning', fakeAsync(() => {
    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();
    const plannedOnce = wailsSpy.planJobV1.calls.count();

    const top = fixture.nativeElement.querySelector(
      '#md-margin-top',
    ) as HTMLInputElement;
    top.value = '30';
    top.dispatchEvent(new Event('input'));

    tick(199);
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce);

    tick(1);
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);
    expect(lastPlanRequest().options['margins']).toEqual({
      top: 30,
      right: 15,
      bottom: 18,
      left: 15,
    });
  }));

  it('resets the page setup to the backend defaults', fakeAsync(() => {
    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();

    component.pageSetupForm.patchValue({
      pageSize: 'Legal',
      orientation: 'L',
      marginTop: 40,
    });
    tick(250);
    flushMicrotasks();
    expect(lastPlanRequest().options['pageSize']).toBe('Legal');

    const resetButton = fixture.nativeElement.querySelector(
      '.page-setup-actions button',
    ) as HTMLButtonElement;
    resetButton.click();
    flushMicrotasks();

    expect(component.pageSetupForm.getRawValue()).toEqual({
      pageSize: 'A4',
      orientation: 'P',
      marginTop: 20,
      marginRight: 15,
      marginBottom: 18,
      marginLeft: 15,
    });
    const request = lastPlanRequest();
    expect(request.options['pageSize']).toBe('A4');
    expect(request.options['orientation']).toBe('P');
    expect(request.options['margins']).toEqual({
      top: 20,
      right: 15,
      bottom: 18,
      left: 15,
    });
  }));

  it('blocks the action when the plan rejects the page setup', fakeAsync(() => {
    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) => {
      const margins = request.options['margins'] as
        | { left: number }
        | undefined;
      if (margins?.left === 99) {
        return Promise.resolve({
          success: false,
          message: 'invalid page setup',
          outputs: [],
          error: {
            code: 'DOC_MD_TO_PDF_CONFIG_INVALID',
            detail_code: 'DOC_MD_TO_PDF_CONFIG_INVALID',
            message: 'options.margins.left must be between 0 and 100 mm',
          },
        });
      }
      return Promise.resolve(planResponse(request));
    });

    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();

    component.pageSetupForm.patchValue({ marginLeft: 99 });
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.plan()?.success).toBeFalse();
    expect(component.planBlocked()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'options.margins.left must be between 0 and 100 mm',
    );

    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));

  it('runs through runJobV1 without a validation step and starts polling', fakeAsync(() => {
    component.onPathsSelected(['/tmp/doc.md']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.doc.md_to_pdf');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/doc.md']);
    expect(
      (wailsSpy as unknown as { validateJobV1?: unknown }).validateJobV1,
    ).toBeUndefined();
    expect(wailsSpy.getJobStatusV1).toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');

    fixture.destroy();
  }));

  it('inspects outputs on terminal success and opens the result', fakeAsync(() => {
    const outputPath = '/tmp/doc_md2pdf.pdf';
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(successStatus('job-2', outputPath));

    component.onPathsSelected(['/tmp/doc.md']);
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
        (paths) => paths.length === 1 && paths[0] === outputPath,
      ),
    ).toBeTrue();
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(
      '1 document converted',
    );

    void component.onOpen(outputPath);
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);

    void component.onReveal(outputPath);
    flushMicrotasks();
    expect(wailsSpy.revealInFolder).toHaveBeenCalledWith(outputPath);

    fixture.destroy();
  }));

  it('uses the chosen folder in the plan request and can go back to the source folder', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/doc.md']);
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
      '/tmp/out/doc_md2pdf.pdf',
    );

    component.useMarkdownFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to the Markdown file');
    const resetRequest = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(resetRequest.outputDir).toBe('');
  }));
});
