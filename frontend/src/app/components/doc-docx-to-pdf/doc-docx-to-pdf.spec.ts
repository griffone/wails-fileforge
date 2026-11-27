import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { DocDocxToPdf } from './doc-docx-to-pdf';
import { I18nService } from '../../services/i18n.service';
import {
  TOOL_OPTIONS_STORAGE_PREFIX,
  toolOptionsStorageKey,
} from '../../services/preferences.service';
import { LAST_REQUEST_STORAGE_PREFIX } from '../../services/last-request.service';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobResultItem,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('DocDocxToPdf', () => {
  let fixture: ComponentFixture<DocDocxToPdf>;
  let component: DocDocxToPdf;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function fileInfo(path: string): FileInfo {
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

  function planResponse(request: JobRequest): PlanJobResponse {
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
    status: JobResult['status'],
    items: JobResultItem[],
  ): JobStatusResponse {
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

  function runningStatus(jobId: string): JobStatusResponse {
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

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function clearToolStorage(): void {
    const prefixes = [
      TOOL_OPTIONS_STORAGE_PREFIX,
      LAST_REQUEST_STORAGE_PREFIX,
    ];
    const keys: string[] = [];
    for (let index = 0; index < localStorage.length; index += 1) {
      const key = localStorage.key(index);
      if (key && prefixes.some((prefix) => key.startsWith(prefix))) {
        keys.push(key);
      }
    }
    for (const key of keys) {
      localStorage.removeItem(key);
    }
  }

  function seedOptions(values: Record<string, unknown>): void {
    localStorage.setItem(
      toolOptionsStorageKey('tool.doc.docx_to_pdf'),
      JSON.stringify({ schemaVersion: 1, values }),
    );
  }

  function storedOptions(): Record<string, unknown> {
    return JSON.parse(
      localStorage.getItem(
        toolOptionsStorageKey('tool.doc.docx_to_pdf'),
      ) ?? '{}',
    );
  }

  function partialStatus(jobId: string): JobStatusResponse {
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
    clearToolStorage();
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'getThumbnail',
      'runJob',
      'getJobStatus',
      'cancelJob',
      'openPath',
      'revealInFolder',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
      'listTools',
    ]);

    jobProgressSubject = new Subject<JobProgressEvent>();
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ =
      jobProgressSubject.asObservable();

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
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.isRuntimeAvailable.and.returnValue(true);
    wailsSpy.listTools.and.resolveTo({
      success: true,
      message: 'ok',
      tools: [],
    });

    await TestBed.configureTestingModule({
      imports: [DocDocxToPdf],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(DocDocxToPdf);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('plans single for one file and batch automatically for two', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.doc.docx_to_pdf');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.docx']);
    expect(component.primaryLabel()).toBe('Convert 1 document');

    component.onPathsSelected(['/tmp/b.docx']);
    flushMicrotasks();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.docx', '/tmp/b.docx']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.primaryLabel()).toBe('Convert 2 documents');
    expect(component.fileItems().map((item) => item.name)).toEqual([
      'a.docx',
      'b.docx',
    ]);
  }));

  it('loads the document first-page thumbnail into the queue', fakeAsync(() => {
    wailsSpy.getThumbnail.and.resolveTo('data:image/jpeg;base64,abc');

    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getThumbnail).toHaveBeenCalledWith('/tmp/a.docx', 160);
    expect(component.fileItems()[0].thumbnailUrl).toBe(
      'data:image/jpeg;base64,abc',
    );
    const image = fixture.nativeElement.querySelector(
      '.file-queue-image',
    ) as HTMLImageElement;
    expect(image?.getAttribute('src')).toBe('data:image/jpeg;base64,abc');
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
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
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

  it('runs from the plan through runJob without a validation step', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx', '/tmp/b.docx']);
    flushMicrotasks();

    const plannedOnce = wailsSpy.planJob.calls.count();
    component.plan.set(null);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 1);
    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.doc.docx_to_pdf');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.docx', '/tmp/b.docx']);
    expect(
      (wailsSpy as unknown as { validateJob?: unknown }).validateJob,
    ).toBeUndefined();
    expect(component.isPolling()).toBeTrue();

    fixture.destroy();
  }));

  it('renders partial success and retries only the failed inputs', fakeAsync(() => {
    wailsSpy.getJobStatus.and.resolveTo(partialStatus('job-1'));

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
    expect(wailsSpy.runJob).toHaveBeenCalledTimes(2);
    const retryRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
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

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual(
      ['/tmp/out/a_docx2pdf.pdf', '/tmp/out/b_docx2pdf.pdf'],
    );

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each DOCX');
    const resetRequest = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
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
    const plannedOnce = wailsSpy.planJob.calls.count();

    component.onPageSelectionInput(inputEvent('1-2'));
    tick(249);
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce);

    tick(1);
    flushMicrotasks();
    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 1);
    expect(lastPlanRequest().options['pageSelection']).toBe('1-2');
  }));

  it('renders the archival toggle with the Ghostscript requirement', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();

    const toggle = pdfaToggle();
    expect(toggle).toBeTruthy();
    expect(toggle.checked).toBeFalse();

    const helper = fixture.nativeElement.querySelector(
      '#docx-pdfa-help',
    ) as HTMLElement;
    expect(helper.textContent).toContain('Requires Ghostscript.');
    expect(helper.textContent).toContain(
      'the export fails instead of producing a non-archival PDF',
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

    const runRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
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

    const runRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect('pdfa' in runRequest.options).toBeFalse();
  }));

  it('refreshes the plan when the archival toggle changes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    fixture.detectChanges();
    const plannedOnce = wailsSpy.planJob.calls.count();

    setPdfa(true);

    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 1);
    expect(component.plan()?.success).toBeTrue();
    expect(component.plan()?.effectiveOptions?.['pdfa']).toBeTrue();

    setPdfa(false);

    expect(wailsSpy.planJob.calls.count()).toBe(plannedOnce + 2);
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

    const runRequest = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(runRequest.options['pageSelection']).toBe('1-2');
    expect(runRequest.options['pdfa']).toBeTrue();
  }));

  it('blocks the action when the plan cannot export PDF/A', fakeAsync(() => {
    wailsSpy.planJob.and.callFake((request: JobRequest) => {
      if (request.options['pdfa'] === true) {
        return Promise.resolve({
          success: false,
          message: 'Ghostscript (gs) is required for PDF/A export but is not installed.',
          outputs: [],
          error: {
            code: 'RUNTIME_DEP_MISSING',
            detail_code: 'DOC_DOCX_TO_PDF_RUNTIME_GHOSTSCRIPT_MISSING',
            message: 'Ghostscript (gs) is required for PDF/A export but is not installed.',
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
    expect(wailsSpy.runJob).not.toHaveBeenCalled();

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
    wailsSpy.planJob.and.callFake((request: JobRequest) => {
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
    expect(wailsSpy.runJob).not.toHaveBeenCalled();
  }));

  it('renders the page copy in Spanish', fakeAsync(() => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('DOCX');
    expect(title).toContain('PDF');

    expect(
      element.querySelector('label[for="docx-pdfa"]')?.textContent,
    ).toContain('PDF/A (archivístico)');
    expect(
      element.querySelector('.action-primary')?.textContent,
    ).toContain('Convertir a PDF');
  }));

  it('restores the remembered options on init', fakeAsync(() => {
    seedOptions({ pageSelection: '1-2', pdfa: true });

    const recreated = TestBed.createComponent(DocDocxToPdf);
    recreated.detectChanges();
    flushMicrotasks();
    const page = recreated.componentInstance;

    expect(page.pageSelection()).toBe('1-2');
    expect(page.pdfa()).toBeTrue();

    recreated.destroy();
  }));

  it('saves the options and the submitted request after a successful run', fakeAsync(() => {
    component.onPageSelectionInput(inputEvent('1-2'));
    component.onPdfaToggle({ target: { checked: true } } as unknown as Event);
    tick(250);
    flushMicrotasks();

    component.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(storedOptions()).toEqual({
      schemaVersion: 1,
      values: { pageSelection: '1-2', pdfa: true },
    });

    const request = JSON.parse(
      localStorage.getItem(
        `${LAST_REQUEST_STORAGE_PREFIX}tool.doc.docx_to_pdf`,
      ) ?? '{}',
    );
    expect(request.request.inputPaths).toEqual(['/tmp/a.docx']);
    expect(request.request.options).toEqual({
      pageSelection: '1-2',
      pdfa: true,
    });
  }));

  it('keeps the remembered options when reset clears the form', fakeAsync(() => {
    seedOptions({ pageSelection: '1-2', pdfa: true });

    const recreated = TestBed.createComponent(DocDocxToPdf);
    recreated.detectChanges();
    flushMicrotasks();
    const page = recreated.componentInstance;

    page.onPathsSelected(['/tmp/a.docx']);
    flushMicrotasks();
    void page.run();
    flushMicrotasks();

    page.reset();
    flushMicrotasks();

    expect(page.selectedPaths()).toEqual([]);
    expect(page.jobResult()).toBeNull();
    expect(page.pageSelection()).toBe('1-2');
    expect(page.pdfa()).toBeTrue();
    expect(storedOptions()['schemaVersion']).toBe(1);

    recreated.destroy();
  }));
});
