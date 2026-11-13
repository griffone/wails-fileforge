import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { PdfCrop } from './pdf-crop';
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

describe('PdfCrop', () => {
  let fixture: ComponentFixture<PdfCrop>;
  let component: PdfCrop;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function fileInfo(path: string, pageSize?: { width: number; height: number }): FileInfoV1 {
    const name = path.split(/[\\/]/).pop() ?? path;
    const info: FileInfoV1 = {
      path,
      name,
      directory: '/tmp',
      extension: 'pdf',
      sizeBytes: 2 * 1024 * 1024,
      kind: 'pdf',
      pageCount: 4,
      exists: true,
    };
    if (pageSize) {
      info.pageWidthPoints = pageSize.width;
      info.pageHeightPoints = pageSize.height;
    }
    return info;
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
      const name = `${fileStem(inputPath)}_cropped.pdf`;
      return {
        inputPath,
        outputPath: dir ? `${dir}/${name}` : name,
        outputKind: 'file',
        outputCount: 1,
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
        toolId: 'tool.pdf.crop',
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

  function primaryButton(): HTMLButtonElement {
    return fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
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
        files: paths.map((path) => fileInfo(path)),
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
      imports: [PdfCrop],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(PdfCrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('plans one PDF as single with the default Small preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.pdf.crop');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf']);
    expect(request.outputDir).toBe('');
    expect(request.options['cropPreset']).toBe('small');
    expect(request.options['pageSelection']).toBe('');
    expect(request.options['margins']).toBeUndefined();

    expect(component.plan()?.outputs[0]).toEqual(
      jasmine.objectContaining({
        outputPath: '/tmp/a_cropped.pdf',
        outputKind: 'file',
        outputCount: 1,
      }),
    );
    expect(component.primaryLabel()).toBe('Crop 1 PDF');
    expect(component.canRun()).toBeTrue();
    expect(primaryButton().disabled).toBeFalse();
  }));

  it('switches to batch automatically for two PDFs', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    component.onPathsSelected(['/tmp/b.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(component.plan()?.outputs.map((output) => output.outputPath)).toEqual(
      ['/tmp/a_cropped.pdf', '/tmp/b_cropped.pdf'],
    );
    expect(component.primaryLabel()).toBe('Crop 2 PDFs');
    expect(primaryButton().disabled).toBeFalse();
  }));

  it('refreshes the plan when the preset changes', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);

    component.selectPreset('medium');
    flushMicrotasks();
    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(2);
    expect(lastPlanRequest().options['cropPreset']).toBe('medium');

    component.selectPreset('none');
    flushMicrotasks();
    expect(lastPlanRequest().options['cropPreset']).toBe('none');
    expect(component.activePresetLabel()).toBe('None');
    expect(component.canRun()).toBeTrue();
  }));

  it('blocks Custom without margins and sends valid margins', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectPreset('custom');
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.isCustom()).toBeTrue();
    expect(component.marginsValid()).toBeFalse();
    expect(component.canRun()).toBeFalse();
    expect(primaryButton().disabled).toBeTrue();
    expect(component.actionHelper()).toContain('Enter all four margins');
    expect(
      fixture.nativeElement.querySelectorAll('.advanced-grid input').length,
    ).toBe(4);

    component.onMarginInput('top', inputEvent('12'));
    component.onMarginInput('right', inputEvent('8'));
    component.onMarginInput('bottom', inputEvent('6'));
    component.onMarginInput('left', inputEvent('4'));
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.marginsValid()).toBeTrue();
    expect(component.canRun()).toBeTrue();
    expect(primaryButton().disabled).toBeFalse();
    expect(lastPlanRequest().options['cropPreset']).toBe('custom');
    expect(lastPlanRequest().options['margins']).toEqual({
      top: 12,
      right: 8,
      bottom: 6,
      left: 4,
    });
  }));

  it('sends all pages as an empty selection and custom ranges otherwise', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    expect(lastPlanRequest().options['pageSelection']).toBe('');

    component.selectPageMode('ranges');
    flushMicrotasks();
    expect(component.rangesRequired()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();

    component.onRangesInput(inputEvent('1-3,5'));
    tick(250);
    flushMicrotasks();

    expect(lastPlanRequest().options['pageSelection']).toBe('1-3,5');
    expect(component.rangesRequired()).toBeFalse();
    expect(component.canRun()).toBeTrue();

    component.onRangesInput(inputEvent(''));
    flushMicrotasks();
    expect(component.rangesRequired()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();
  }));

  it('plans and runs without a validation step, then polls', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf', '/tmp/b.pdf']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(2);
    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.pdf.crop');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.pdf', '/tmp/b.pdf']);
    expect(request.options['cropPreset']).toBe('small');
    expect(
      (wailsSpy as unknown as { validateJobV1?: unknown }).validateJobV1,
    ).toBeUndefined();
    expect(wailsSpy.getJobStatusV1).toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-1');
  }));

  it('inspects terminal results and opens the cropped file', fakeAsync(() => {
    const completed = status('job-2', 'success', [
      {
        inputPath: '/tmp/a.pdf',
        outputPath: '/tmp/a_cropped.pdf',
        outputs: ['/tmp/a_cropped.pdf'],
        outputCount: 1,
        success: true,
        message: 'PDF crop successful',
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
    expect(fixture.nativeElement.textContent).toContain('1 PDF cropped');

    const inspectedPaths = wailsSpy.inspectFilesV1.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) => paths.includes('/tmp/a_cropped.pdf')),
    ).toBeTrue();

    const openButton = fixture.nativeElement.querySelector(
      '.result-open',
    ) as HTMLButtonElement;
    openButton.click();
    flushMicrotasks();

    expect(wailsSpy.openPath).toHaveBeenCalledWith('/tmp/a_cropped.pdf');
  }));

  it('uses the chosen destination folder in the plan', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(lastPlanRequest().outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/a_cropped.pdf',
    );

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each PDF');
    expect(lastPlanRequest().outputDir).toBe('');
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
    expect(component.notice()?.text).toContain(
      'a.pdf is already in the queue.',
    );
  }));

  it('shows PDF metadata in the queue and an optional first-page preview', fakeAsync(() => {
    wailsSpy.getThumbnail.and.callFake((_path: string, size?: number) =>
      Promise.resolve(
        size === 220 ? 'data:image/webp;base64,preview' : null,
      ),
    );

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.fileItems()[0].meta).toContain('2 MB');
    expect(component.fileItems()[0].meta).toContain('4 pages');
    expect(
      fixture.nativeElement.querySelector('.preview-image'),
    ).not.toBeNull();
    expect(component.previewNote()).toContain('First page of a.pdf');
  }));

  it('shows a crop overlay with preset margins scaled to the page size', fakeAsync(() => {
    wailsSpy.inspectFilesV1.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) =>
          fileInfo(path, { width: 300, height: 400 }),
        ),
      }),
    );
    wailsSpy.getThumbnail.and.callFake((_path: string, size?: number) =>
      Promise.resolve(size === 220 ? 'data:image/webp;base64,preview' : null),
    );

    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.previewPageSize()).toEqual({ width: 300, height: 400 });
    expect(component.previewAspectRatio()).toBe('300 / 400');
    expect(component.previewMarginsLabel()).toBe('10 pt');
    expect(component.previewInsets().left).toBeCloseTo((10 / 300) * 100, 5);
    expect(component.previewInsets().right).toBeCloseTo((10 / 300) * 100, 5);
    expect(component.previewInsets().top).toBeCloseTo((10 / 400) * 100, 5);
    expect(component.previewInsets().bottom).toBeCloseTo((10 / 400) * 100, 5);
    expect(component.previewInsets().dimmed).toBeTrue();
    expect(component.previewInsets().fullPage).toBeFalse();

    const frame = fixture.nativeElement.querySelector(
      '.preview-frame',
    ) as HTMLElement;
    expect(frame.style.aspectRatio).toBe('300 / 400');
    const region = fixture.nativeElement.querySelector(
      '.crop-region',
    ) as HTMLElement;
    expect(region.getAttribute('aria-hidden')).toBe('true');
    expect(region.style.top).toContain('%');
    expect(region.style.left).toContain('%');
    expect(fixture.nativeElement.textContent).toContain(
      'Approximate crop preview',
    );

    component.selectPreset('none');
    fixture.detectChanges();
    expect(component.previewMarginsLabel()).toBe('0 pt');
    expect(component.previewInsets().dimmed).toBeFalse();
    expect(component.previewInsets().fullPage).toBeTrue();

    component.selectPreset('large');
    fixture.detectChanges();
    expect(component.previewInsets().left).toBeCloseTo((40 / 300) * 100, 5);
    expect(component.previewInsets().top).toBeCloseTo((40 / 400) * 100, 5);
  }));

  it('updates the overlay live for custom margins', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    component.selectPreset('custom');
    flushMicrotasks();
    component.onMarginInput('top', inputEvent('20'));
    component.onMarginInput('right', inputEvent('10'));
    component.onMarginInput('bottom', inputEvent('5'));
    component.onMarginInput('left', inputEvent('10'));
    fixture.detectChanges();

    expect(component.previewMarginsLabel()).toBe('T 20 · R 10 · B 5 · L 10');
    expect(component.previewInsets().top).toBeCloseTo((20 / 842) * 100, 5);
    expect(component.previewInsets().right).toBeCloseTo((10 / 595) * 100, 5);
    expect(component.previewInsets().bottom).toBeCloseTo((5 / 842) * 100, 5);
    expect(component.previewInsets().left).toBeCloseTo((10 / 595) * 100, 5);

    component.onMarginInput('top', inputEvent('100'));
    fixture.detectChanges();

    expect(component.previewInsets().top).toBeCloseTo((100 / 842) * 100, 5);
    expect(component.previewMarginsLabel()).toContain('T 100');

    component.onMarginInput('top', inputEvent(''));
    fixture.detectChanges();

    expect(component.previewInsets().fullPage).toBeTrue();
    expect(component.previewInsets().dimmed).toBeFalse();
  }));

  it('falls back to A4 when the page size is unknown', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.previewPageSize()).toEqual({ width: 595, height: 842 });
    expect(component.previewAspectRatio()).toBe('595 / 842');
    expect(component.previewInsets().left).toBeCloseTo((10 / 595) * 100, 5);
    expect(component.previewInsets().top).toBeCloseTo((10 / 842) * 100, 5);
  }));

  it('renders the workbench header and no legacy confirmation flow', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector('.eyebrow')?.textContent).toContain('PDF');
    expect(element.querySelector('.page-title')?.textContent).toContain('PDF');
    expect(element.querySelector('.page-title')?.textContent).toContain('Crop');
    expect(element.querySelectorAll('.page-head a').length).toBe(0);

    const text = element.textContent as string;
    expect(text).toContain(
      'Trim page margins with presets or exact values. Everything runs on this machine.',
    );
    expect(element.querySelector('.modal-backdrop')).toBeNull();
    expect(element.querySelector('.modal-card')).toBeNull();
    expect(element.querySelector('#jobMode')).toBeNull();
    expect(element.querySelector('#outputPath')).toBeNull();

    const legacy = component as unknown as Record<string, unknown>;
    expect(legacy['validate']).toBeUndefined();
    expect(legacy['panelFields']).toBeUndefined();
    expect(legacy['showRunSummaryConfirmation']).toBeUndefined();
  });
});
