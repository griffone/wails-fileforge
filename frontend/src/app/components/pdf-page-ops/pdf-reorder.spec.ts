import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';

import { PdfReorder } from './pdf-reorder';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobRequest,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('PdfReorder', () => {
  let fixture: ComponentFixture<PdfReorder>;
  let component: PdfReorder;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_reordered.pdf'),
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string, pageCount = 6): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 6144,
      pageCount,
      kind: 'pdf',
      exists: true,
    };
  }

  function changeEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function errorLines(): string[] {
    return Array.from(
      (fixture.nativeElement as HTMLElement).querySelectorAll('.field-error'),
    ).map((element) => element.textContent?.trim() ?? '');
  }

  function submittedOrders(): unknown[] {
    return wailsSpy.planJob.calls
      .allArgs()
      .map((args) => (args[0] as JobRequest).options['order']);
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
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
      'getThumbnail',
    ]);

    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.callFake((paths: string[]) =>
      Promise.resolve({
        success: true,
        message: 'ok',
        files: paths.map((path) => fileInfo(path)),
      }),
    );
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-reorder',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo({
      success: true,
      message: 'running',
      found: false,
    });
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancelled',
      jobId: 'job-reorder',
    });
    wailsSpy.openPath.and.resolveTo({ success: true, message: 'opened' });
    wailsSpy.revealInFolder.and.resolveTo({
      success: true,
      message: 'revealed',
    });
    wailsSpy.openMultipleFilesDialog.and.resolveTo([]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.getThumbnail.and.resolveTo(null);
    wailsSpy.isRuntimeAvailable.and.returnValue(true);
    wailsSpy.listTools.and.resolveTo({ success: true, message: 'ok', tools: [] });
    wailsSpy.validateJob.and.resolveTo({
      success: true,
      message: 'ok',
      valid: true,
    });

    await TestBed.configureTestingModule({
      imports: [PdfReorder],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(PdfReorder);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('renders the defaults and the reverse shortcut', fakeAsync(() => {
    const element = fixture.nativeElement as HTMLElement;

    expect(
      (element.querySelector('#pdf-reorder-order') as HTMLInputElement).value,
    ).toBe('');
    expect(element.textContent).toContain('Reverse');
    expect(component.primaryLabel()).toBe('Reorder pages');
    expect(component.outputBadge()).toBe('No order yet');

    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();
    expect(element.querySelector('.action-helper')?.textContent).toContain(
      'Enter the page order, or use Reverse.',
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('sends exactly the manifest option keys for a valid spec', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onOrderInput(changeEvent('3,1,2,4-6'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.reorder');
    expect(request.mode).toBe('single');
    expect(request.options).toEqual({ order: '3,1,2,4-6' });
    expect(component.outputBadge()).toBe('Custom order');
    expect(component.canRun()).toBeTrue();
  }));

  it('plans the reverse shortcut', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.useReverse();
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.options).toEqual({ order: 'reverse' });
    expect(component.outputBadge()).toBe('Reverse order');
    expect(component.canRun()).toBeTrue();
    expect(component.orderError()).toBe('');
  }));

  it('names the missing pages and never sends the spec', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onOrderInput(changeEvent('1-5'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(errorLines()).toContain(
      'The order must list every page exactly once. Missing pages: 6.',
    );
    expect(errorLines()).toContain('Missing pages: 6');
    expect(submittedOrders()).not.toContain('1-5');
  }));

  it('names the repeated pages and never sends the spec', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onOrderInput(changeEvent('1,2,3,4,5,6,6'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(errorLines()).toContain(
      'The order must list every page exactly once. Repeated pages: 6.',
    );
    expect(errorLines()).toContain('Repeated pages: 6');
    expect(submittedOrders()).not.toContain('1,2,3,4,5,6,6');
  }));

  it('names an out-of-bounds page and never sends the spec', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onOrderInput(changeEvent('1,2,3,4,5,7'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(errorLines()).toContain(
      'Page 7 does not exist. The PDF has 6 pages.',
    );
    expect(submittedOrders()).not.toContain('1,2,3,4,5,7');
  }));

  it('remembers the accepted order and restores it on init', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.useReverse();
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.pdf.reorder',
      1,
      ['order'],
    );
    expect(stored).toEqual({ order: 'reverse' });

    const restoredFixture = TestBed.createComponent(PdfReorder);
    restoredFixture.detectChanges();

    expect(restoredFixture.componentInstance.order()).toBe('reverse');
    restoredFixture.destroy();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Reordenar',
    );
    expect(element.textContent).toContain('Orden de páginas');
    expect(element.textContent).toContain('Invertir');
  });
});
