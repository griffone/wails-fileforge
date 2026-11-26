import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';

import { PdfRemovePages } from './pdf-remove-pages';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobRequest,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('PdfRemovePages', () => {
  let fixture: ComponentFixture<PdfRemovePages>;
  let component: PdfRemovePages;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_pages_removed.pdf'),
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string, pageCount = 5): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 8192,
      pageCount,
      kind: 'pdf',
      exists: true,
    };
  }

  function changeEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
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
      jobId: 'job-remove',
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
      jobId: 'job-remove',
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
      imports: [PdfRemovePages],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(PdfRemovePages);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('renders the defaults with an empty required selection', fakeAsync(() => {
    fixture.detectChanges();
    const element = fixture.nativeElement as HTMLElement;

    expect(element.textContent).toContain('Pages to delete');
    expect(
      (element.querySelector('#pdf-remove-pages') as HTMLInputElement).value,
    ).toBe('');
    expect(component.primaryLabel()).toBe('Remove pages');
    expect(component.outputBadge()).toBe('No pages picked');

    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.canRun()).toBeFalse();
    expect(component.plan()).toBeNull();
    expect(component.primaryLabel()).toBe('Remove pages from 1 PDF');
    expect(element.querySelector('.action-helper')?.textContent).toContain(
      'Enter the pages you want to delete.',
    );
    expect(wailsSpy.planJob).not.toHaveBeenCalled();
  }));

  it('sends exactly the manifest option keys', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('1-2'));
    tick(200);
    flushMicrotasks();

    const request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.remove_pages');
    expect(request.mode).toBe('single');
    expect(request.options).toEqual({ pageSelection: '1-2' });
    expect(component.outputBadge()).toBe('Delete 1-2');
    expect(component.canRun()).toBeTrue();
  }));

  it('blocks a selection that covers every page with the backend reason', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('1-5'));
    flushMicrotasks();

    fixture.detectChanges();
    const element = fixture.nativeElement as HTMLElement;
    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(element.querySelector('.field-error')?.textContent).toContain(
      'Refusing to remove every page: the selection covers all 5 pages. Keep at least one page.',
    );

    const submitted = wailsSpy.planJob.calls
      .allArgs()
      .map((args) => args[0] as JobRequest)
      .filter((request) => request.options['pageSelection'] === '1-5');
    expect(submitted).toEqual([]);
  }));

  it('blocks an out-of-bounds selection before the plan call', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('1-6'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(
      (fixture.nativeElement as HTMLElement).querySelector('.field-error')
        ?.textContent,
    ).toContain('exceeds the 5-page document');
  }));

  it('explains a reversed range instead of planning it', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('3-1'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(
      (fixture.nativeElement as HTMLElement).querySelector('.field-error')
        ?.textContent,
    ).toContain('ascending order');
  }));

  it('remembers the accepted selection and restores it on init', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('2-3'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.pdf.remove_pages',
      1,
      ['pageSelection'],
    );
    expect(stored).toEqual({ pageSelection: '2-3' });

    const restoredFixture = TestBed.createComponent(PdfRemovePages);
    restoredFixture.detectChanges();

    expect(restoredFixture.componentInstance.pageSelection()).toBe('2-3');
    restoredFixture.destroy();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Eliminar',
    );
    expect(element.textContent).toContain('Páginas a eliminar');
  });
});
