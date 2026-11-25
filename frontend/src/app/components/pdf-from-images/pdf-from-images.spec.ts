import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
  ComponentFixture,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { PdfFromImages } from './pdf-from-images';
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

interface SlotOption {
  page: number;
  cell: number;
  inputIndex: number;
}

describe('PdfFromImages', () => {
  let fixture: ComponentFixture<PdfFromImages>;
  let component: PdfFromImages;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;
  let jobStateSubject: Subject<JobResult>;
  let inspectCalls: string[][];

  const outputPath = '/tmp/out/album_images.pdf';

  function planResponse(request: JobRequest): PlanJobResponse {
    const slots = Array.isArray(request.options['slots'])
      ? (request.options['slots'] as SlotOption[])
      : [];
    const pageCount = slots.reduce(
      (highest, slot) => Math.max(highest, Number(slot.page) + 1),
      1,
    );

    return {
      success: true,
      message: `Ready to place ${request.inputPaths.length} images on ${pageCount} page(s).`,
      outputs: [
        {
          inputPath: request.inputPaths.join(','),
          outputPath,
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

  function fileInfo(path: string, sizeBytes = 2048): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/out',
      extension: name.split('.').pop() ?? '',
      sizeBytes,
      kind: 'pdf',
      pageCount: 2,
      exists: true,
    };
  }

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function lastPlanSlots(): SlotOption[] {
    return lastPlanRequest().options['slots'] as SlotOption[];
  }

  function select(paths: string[]): void {
    component.onPathsSelected(paths);
    tick(250);
    flushMicrotasks();
  }

  function runningStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.pdf.images',
      status: 'running',
      progress: {
        current: 0,
        total: 5,
        stage: 'running',
        message: 'Placed 0 of 5 images',
      },
      items: [],
      startedAt: 1,
    };
    return { success: true, message: 'running', found: true, result };
  }

  function terminalStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.pdf.images',
      status: 'success',
      progress: {
        current: 5,
        total: 5,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath: '/tmp/a.png,/tmp/b.png',
          outputPath,
          outputs: [outputPath],
          success: true,
          message: '2 images on 1 page(s).',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    localStorage.removeItem('ff-recent-conversions');
    inspectCalls = [];

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
      'validateJob',
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
    wailsSpy.inspectFiles.and.callFake((paths: string[]) => {
      inspectCalls.push([...paths]);
      return Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => fileInfo(path)),
      });
    });
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-default',
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
    wailsSpy.validateJob.and.resolveTo({
      success: true,
      message: 'ok',
      valid: true,
    });

    await TestBed.configureTestingModule({
      imports: [PdfFromImages],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(PdfFromImages);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    localStorage.removeItem('ff-recent-conversions');
  });

  it('fills five images into two 2×2 pages and sends the slot map', fakeAsync(() => {
    const paths = [
      '/tmp/a.png',
      '/tmp/b.png',
      '/tmp/c.png',
      '/tmp/d.png',
      '/tmp/e.png',
    ];
    select(paths);

    expect(component.grid()).toEqual({ columns: 2, rows: 2, cells: 4 });
    expect(component.pages()).toBe(2);
    expect(component.unplacedPaths()).toEqual([]);

    const slots = component.pageSlots();
    expect(slots.length).toBe(2);
    expect(slots[0].filter(Boolean).length).toBe(4);
    expect(slots[1].filter(Boolean).length).toBe(1);

    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.pdf.images');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(paths);
    expect(request.options['columns']).toBe(2);
    expect(request.options['rows']).toBe(2);
    expect(request.options['gapMm']).toBe(4);

    const slotOptions = lastPlanSlots();
    expect(slotOptions.length).toBe(5);
    expect(slotOptions[4]).toEqual({ page: 1, cell: 0, inputIndex: 4 });
  }));

  it('uses a 1×1 grid and one page per image at one image per page', fakeAsync(() => {
    select(['/tmp/a.png', '/tmp/b.png', '/tmp/c.png', '/tmp/d.png', '/tmp/e.png']);

    component.selectPerPage(1);
    tick(250);
    flushMicrotasks();

    expect(component.grid()).toEqual({ columns: 1, rows: 1, cells: 1 });
    expect(component.pages()).toBe(5);
    expect(lastPlanRequest().options['columns']).toBe(1);
    expect(lastPlanRequest().options['rows']).toBe(1);
    expect(component.notice()?.text).toBe(
      'The grid changed; the images were refilled.',
    );
  }));

  it('rotates the grid for landscape pages with two images per page', fakeAsync(() => {
    select(['/tmp/a.png', '/tmp/b.png']);

    component.selectOrientation('landscape');
    component.selectPerPage(2);
    tick(250);
    flushMicrotasks();

    expect(component.grid()).toEqual({ columns: 2, rows: 1, cells: 2 });
    expect(lastPlanRequest().options['orientation']).toBe('landscape');
    expect(lastPlanRequest().options['columns']).toBe(2);
    expect(lastPlanRequest().options['rows']).toBe(1);
  }));

  it('swaps images between slots and moves them within a page', fakeAsync(() => {
    select([
      '/tmp/a.png',
      '/tmp/b.png',
      '/tmp/c.png',
      '/tmp/d.png',
      '/tmp/e.png',
    ]);

    component.moveImageToSlot('/tmp/e.png', 0, 0);
    tick(250);
    flushMicrotasks();

    expect(component.placements()['/tmp/e.png']).toEqual({ page: 0, cell: 0 });
    expect(component.placements()['/tmp/a.png']).toEqual({ page: 1, cell: 0 });

    let slots = lastPlanSlots();
    expect(slots.find((slot) => slot.inputIndex === 4)).toEqual({
      page: 0,
      cell: 0,
      inputIndex: 4,
    });
    expect(slots.find((slot) => slot.inputIndex === 0)).toEqual({
      page: 1,
      cell: 0,
      inputIndex: 0,
    });

    component.moveSlot('/tmp/b.png', 'down');
    tick(250);
    flushMicrotasks();

    expect(component.placements()['/tmp/b.png']).toEqual({ page: 0, cell: 3 });
    expect(component.placements()['/tmp/d.png']).toEqual({ page: 0, cell: 1 });

    slots = lastPlanSlots();
    expect(slots.find((slot) => slot.inputIndex === 1)).toEqual({
      page: 0,
      cell: 3,
      inputIndex: 1,
    });
  }));

  it('disables the run action while an image is unplaced and re-places it', fakeAsync(() => {
    select(['/tmp/a.png']);

    component.unplace('/tmp/a.png');
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.unplacedPaths()).toEqual(['/tmp/a.png']);
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe('Place all images or remove them.');

    const button = fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
    expect(button.disabled).toBeTrue();
    expect(fixture.nativeElement.textContent).toContain(
      'Place all images or remove them.',
    );

    component.placeFromTray('/tmp/a.png');
    tick(250);
    flushMicrotasks();

    expect(component.placements()['/tmp/a.png']).toEqual({ page: 0, cell: 0 });
    expect(component.canRun()).toBeTrue();
  }));

  it('removes one image and clears the whole selection', fakeAsync(() => {
    select([
      '/tmp/a.png',
      '/tmp/b.png',
      '/tmp/c.png',
      '/tmp/d.png',
      '/tmp/e.png',
    ]);

    component.removeImage('/tmp/e.png');
    tick(250);
    flushMicrotasks();

    expect(component.selectedPaths().length).toBe(4);
    expect(component.pages()).toBe(1);
    expect(component.placements()['/tmp/e.png']).toBeUndefined();

    component.clear();
    tick(250);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.placements()).toEqual({});
    expect(component.pages()).toBe(1);
    expect(component.pageSlots()).toEqual([[null, null, null, null]]);
    expect(component.plan()).toBeNull();
  }));

  it('runs one PDF job and renders the terminal output', fakeAsync(() => {
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-7',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(runningStatus('job-7'));
    const completed = terminalStatus('job-7');

    select(['/tmp/a.png', '/tmp/b.png']);

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.images');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.png', '/tmp/b.png']);
    expect(request.options['columns']).toBe(2);
    expect((request.options['slots'] as SlotOption[]).length).toBe(2);
    expect(wailsSpy.validateJob).not.toHaveBeenCalled();
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('job-7');

    jobStateSubject.next(completed.result!);
    flushMicrotasks();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(inspectCalls.some((paths) => paths.includes(outputPath))).toBeTrue();
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('album_images.pdf');
    expect(fixture.nativeElement.textContent).toContain('1 PDF created');
    expect(fixture.nativeElement.textContent).toContain(
      'Verification unavailable for this job.',
    );
  }));

  it('renders the page copy and layout options in Spanish', fakeAsync(() => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Imágenes');
    expect(title).toContain('PDF');

    const pageSizes = Array.from(
      element.querySelectorAll('#pdf-images-page-size option'),
    ) as HTMLOptionElement[];
    expect(
      pageSizes.some((option) => option.textContent?.includes('Carta')),
    ).toBeTrue();
    expect(
      element.querySelector('.action-primary')?.textContent,
    ).toContain('Crear PDF');
  }));
});
