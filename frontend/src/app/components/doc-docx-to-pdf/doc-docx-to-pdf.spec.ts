import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { DocDocxToPdf } from './doc-docx-to-pdf';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('DocDocxToPdf', () => {
  let fixture: ComponentFixture<DocDocxToPdf>;
  let component: DocDocxToPdf;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function fileInfo(path: string): FileInfoV1 {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: 'docx',
      sizeBytes: 8192,
      kind: 'doc',
      exists: true,
    };
  }

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function plannedOutput(inputPath: string, outputDir: string): string {
    const dir = outputDir || firstDir(inputPath);
    const stem = (inputPath.split(/[\\/]/).pop() ?? 'document').replace(
      /\.docx$/i,
      '',
    );
    const name = `${stem}_docx2pdf.pdf`;
    return dir ? `${dir}/${name}` : name;
  }

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: plannedOutput(inputPath, request.outputDir),
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
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
        toolId: 'tool.doc.docx_to_pdf',
        status,
        progress: {
          current: status === 'running' ? 0 : items.length,
          total: items.length || 1,
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

  function inputEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function pdfaToggle(): HTMLInputElement {
    return fixture.nativeElement.querySelector(
      '#docx-pdfa',
    ) as HTMLInputElement;
  }

  function setPdfa(checked: boolean): void {
    const toggle = pdfaToggle();
    toggle.checked = checked;
    toggle.dispatchEvent(new Event('change'));
    flushMicrotasks();
    fixture.detectChanges();
  }

  function lastPlanRequest(): JobRequestV1 {
    return wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
  }

  function partialStatus(jobId: string): JobStatusResponseV1 {
    return result(jobId, 'partial_success', [
      {
        inputPath: '/tmp/a.docx',
        outputPath: '/tmp/a_docx2pdf.pdf',
        outputs: ['/tmp/a_docx2pdf.pdf'],
        success: true,
        message: 'docx to pdf successful',
      },
      {
        inputPath: '/tmp/b.docx',
        outputPath: '/tmp/b_docx2pdf.pdf',
        success: false,
        message: 'The document could not be converted.',
        error: {
          code: 'EXEC_IO_TRANSIENT',
          detail_code: 'DOC_DOCX_TO_PDF_EXECUTION_FAILED',
          message: 'The document could not be converted.',
        },
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
      imports: [DocDocxToPdf],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(DocDocxToPdf);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans single for one file and batch automatically for two', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.doc.docx_to_pdf');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.docx']);
    expect(component.primaryLabel()).toBe('Convert 1 document');

    component.onPathsSelected(['/tmp/b.docx']);
    flushMicrotasks();

    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.docx', '/tmp/b.docx']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.primaryLabel()).toBe('Convert 2 documents');
    expect(component.fileItems().map((item) => item.name)).toEqual([
      'a.docx',
      'b.docx',
    ]);
  }));

  it('rejects non-DOCX files and ignores duplicates with a notice', fakeAsync(() => {
    component.onPathsSelected([
      '/tmp/notes.txt',
      '/tmp/a.docx',
      '/tmp/a.docx',
    ]);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.docx']);
    expect(component.notice()?.text).toContain('notes.txt');
    expect(component.notice()?.text).toContain("isn't a DOCX file");
    expect(component.notice()?.text).toContain('a.docx is already in the queue.');
    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
  }));

  it('never asks for confirmation and shows the fidelity note instead', fakeAsync(() => {
    const confirmSpy = spyOn(window, 'confirm');

    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(confirmSpy).not.toHaveBeenCalled();
    const banner = fixture.nativeElement.querySelector(
      '.banner-info',
    ) as HTMLElement;
    expect(banner).toBeTruthy();
    expect(banner.textContent).toContain('Standard fidelity');
    expect(banner.textContent).toContain(
      'Complex layouts may shift, but the conversion runs locally.',
    );
    expect(fixture.nativeElement.textContent).not.toContain('Check settings');

    fixture.destroy();
  }));

  it('runs from the plan through runJobV1 without a validation step', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx', '/tmp/b.docx']);
    flushMicrotasks();

    const plannedOnce = wailsSpy.planJobV1.calls.count();
    component.plan.set(null);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);
    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.doc.docx_to_pdf');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.docx', '/tmp/b.docx']);
    expect(
      (wailsSpy as unknown as { validateJobV1?: unknown }).validateJobV1,
    ).toBeUndefined();
    expect(component.isPolling()).toBeTrue();

    fixture.destroy();
  }));

  it('renders partial success and retries only the failed inputs', fakeAsync(() => {
    wailsSpy.getJobStatusV1.and.resolveTo(partialStatus('job-1'));

    component.onPathsSelected(['/tmp/a.docx', '/tmp/b.docx']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('partial_success');
    expect(component.isPolling()).toBeFalse();
    expect(fixture.nativeElement.querySelector('app-result-card')).toBeTruthy();
    expect(fixture.nativeElement.textContent).toContain('1 of 2 converted');

    void component.retryFailed();
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/b.docx']);
    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(2);
    const retryRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(retryRequest.inputPaths).toEqual(['/tmp/b.docx']);
    expect(retryRequest.mode).toBe('single');

    fixture.destroy();
  }));

  it('uses the chosen folder in the plan request', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.docx', '/tmp/b.docx']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(component.destinationLabel()).toBe('/tmp/out');

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual(
      ['/tmp/out/a_docx2pdf.pdf', '/tmp/out/b_docx2pdf.pdf'],
    );

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each DOCX');
    const resetRequest = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(resetRequest.outputDir).toBe('');
  }));

  it('sends the trimmed page selection with the plan request', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    expect(lastPlanRequest().options['pageSelection']).toBeUndefined();

    component.onPageSelectionInput(inputEvent(' 1-3, 5 '));
    tick(250);
    flushMicrotasks();

    expect(lastPlanRequest().options['pageSelection']).toBe('1-3, 5');
    expect(component.plan()?.success).toBeTrue();
  }));

  it('omits the page selection when the range is empty', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    component.onPageSelectionInput(inputEvent('2'));
    tick(250);
    flushMicrotasks();
    expect(lastPlanRequest().options['pageSelection']).toBe('2');

    component.onPageSelectionInput(inputEvent('   '));
    tick(250);
    flushMicrotasks();

    expect(lastPlanRequest().options['pageSelection']).toBeUndefined();
    expect('pageSelection' in lastPlanRequest().options).toBeFalse();
  }));

  it('debounces page range edits before refreshing the plan', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    const plannedOnce = wailsSpy.planJobV1.calls.count();

    component.onPageSelectionInput(inputEvent('1-2'));
    tick(249);
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce);

    tick(1);
    flushMicrotasks();
    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);
    expect(lastPlanRequest().options['pageSelection']).toBe('1-2');
  }));

  it('renders the archival toggle with the LibreOffice requirement', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    const toggle = pdfaToggle();
    expect(toggle).toBeTruthy();
    expect(toggle.checked).toBeFalse();

    const helper = fixture.nativeElement.querySelector(
      '#docx-pdfa-help',
    ) as HTMLElement;
    expect(helper.textContent).toContain('Requires LibreOffice.');
    expect(helper.textContent).toContain(
      'the conversion fails instead of producing a non-archival PDF',
    );
    expect(fixture.nativeElement.querySelector('.archival-note')).toBeNull();

    setPdfa(true);

    expect(toggle.checked).toBeTrue();
    const note = fixture.nativeElement.querySelector(
      '.archival-note',
    ) as HTMLElement;
    expect(note.textContent).toContain(
      'Page selection can reduce strict PDF/A compliance.',
    );
  }));

  it('adds pdfa to the plan and run payloads when archival export is on', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    expect('pdfa' in lastPlanRequest().options).toBeFalse();

    setPdfa(true);

    expect(component.pdfa()).toBeTrue();
    expect(lastPlanRequest().options['pdfa']).toBeTrue();

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(runRequest.options['pdfa']).toBeTrue();
  }));

  it('omits pdfa from the plan and run payloads when archival export is off', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    expect('pdfa' in lastPlanRequest().options).toBeFalse();

    setPdfa(true);
    expect(lastPlanRequest().options['pdfa']).toBeTrue();

    setPdfa(false);

    expect(component.pdfa()).toBeFalse();
    expect('pdfa' in lastPlanRequest().options).toBeFalse();

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect('pdfa' in runRequest.options).toBeFalse();
  }));

  it('refreshes the plan when the archival toggle changes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();
    const plannedOnce = wailsSpy.planJobV1.calls.count();

    setPdfa(true);

    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 1);
    expect(component.plan()?.success).toBeTrue();
    expect(component.plan()?.effectiveOptions?.['pdfa']).toBeTrue();

    setPdfa(false);

    expect(wailsSpy.planJobV1.calls.count()).toBe(plannedOnce + 2);
    expect(component.plan()?.success).toBeTrue();
    expect('pdfa' in lastPlanRequest().options).toBeFalse();
  }));

  it('sends the page selection and pdfa together', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    component.onPageSelectionInput(inputEvent('1-2'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    setPdfa(true);

    const planRequest = lastPlanRequest();
    expect(planRequest.options['pageSelection']).toBe('1-2');
    expect(planRequest.options['pdfa']).toBeTrue();

    void component.run();
    flushMicrotasks();

    const runRequest = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(runRequest.options['pageSelection']).toBe('1-2');
    expect(runRequest.options['pdfa']).toBeTrue();
  }));

  it('blocks the action when the plan cannot export PDF/A', fakeAsync(() => {
    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) => {
      if (request.options['pdfa'] === true) {
        return Promise.resolve({
          success: false,
          message: 'libreoffice binary not found in PATH',
          outputs: [],
          error: {
            code: 'DOC_DOCX_TO_PDF_RUNTIME_LIBREOFFICE_MISSING',
            detail_code: 'DOC_DOCX_TO_PDF_RUNTIME_LIBREOFFICE_MISSING',
            message: 'libreoffice binary not found in PATH',
          },
        });
      }
      return Promise.resolve(planResponse(request));
    });

    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    setPdfa(true);

    expect(component.plan()?.success).toBeFalse();
    expect(component.planBlocked()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'A required tool is missing.',
    );

    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();

    setPdfa(false);

    expect(component.plan()?.success).toBeTrue();
    expect(component.planBlocked()).toBeFalse();
    expect(primary.disabled).toBeFalse();
  }));

  it('shows the all pages badge while the range is empty', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelectorAll('.range-badge').length,
    ).toBe(1);

    component.onPageSelectionInput(inputEvent('1-3'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelectorAll('.range-badge').length,
    ).toBe(0);
  }));

  it('blocks the action when the plan rejects the page range', fakeAsync(() => {
    wailsSpy.planJobV1.and.callFake((request: JobRequestV1) => {
      const selection = request.options['pageSelection'];
      if (selection === '9-12') {
        return Promise.resolve({
          success: false,
          message: 'invalid page range',
          outputs: [],
          error: {
            code: 'DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID',
            detail_code: 'DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID',
            message: 'pageSelection "9-12" is not valid. Use ranges like 1-3,5.',
          },
        });
      }
      return Promise.resolve(planResponse(request));
    });

    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    component.onPageSelectionInput(inputEvent('9-12'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.plan()?.success).toBeFalse();
    expect(component.planBlocked()).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'pageSelection "9-12" is not valid',
    );

    const primary = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(primary.disabled).toBeTrue();

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  }));
});
