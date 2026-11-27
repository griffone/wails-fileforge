import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';

import { PdfRotate } from './pdf-rotate';
import { I18nService } from '../../services/i18n.service';
import { PreferencesService } from '../../services/preferences.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  FileInfo,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('PdfRotate', () => {
  let fixture: ComponentFixture<PdfRotate>;
  let component: PdfRotate;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_rotated.pdf'),
        outputKind: 'file',
        outputCount: 1,
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: ['A destination warning from the backend.'],
    };
  }

  function fileInfo(path: string, pageCount = 3): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 4096,
      pageCount,
      kind: 'pdf',
      exists: true,
    };
  }

  function changeEvent(value: string): Event {
    return { target: { value } } as unknown as Event;
  }

  function terminalStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.pdf.rotate',
      status: 'success',
      progress: {
        current: 100,
        total: 100,
        stage: 'pdf.rotate',
        message: 'Finished',
      },
      items: [
        {
          inputPath: '/tmp/in/report.pdf',
          outputPath: '/tmp/in/report_rotated.pdf',
          outputs: ['/tmp/in/report_rotated.pdf'],
          outputCount: 1,
          success: true,
          message: 'PDF rotate successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  function angleButton(angle: number): HTMLButtonElement {
    return fixture.nativeElement.querySelector(
      `[data-angle="${angle}"]`,
    ) as HTMLButtonElement;
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
      jobId: 'job-rotate',
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
      jobId: 'job-rotate',
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
      imports: [PdfRotate],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(PdfRotate);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('renders the manifest defaults', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(angleButton(90).getAttribute('aria-pressed')).toBe('true');
    expect(angleButton(180).getAttribute('aria-pressed')).toBe('false');
    expect(angleButton(270).getAttribute('aria-pressed')).toBe('false');
    expect(
      (element.querySelector('#pdf-rotate-pages') as HTMLInputElement).value,
    ).toBe('');
    expect(component.primaryLabel()).toBe('Rotate PDF');
    expect(component.outputBadge()).toBe('90° · All pages');
    expect(component.canRun()).toBeFalse();
  });

  it('sends exactly the manifest option keys', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.rotate');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/in/report.pdf']);
    expect(request.options).toEqual({ pageSelection: '', angle: 90 });

    component.selectAngle(270);
    component.onPagesInput(changeEvent('1-2'));
    tick(200);
    flushMicrotasks();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options).toEqual({ pageSelection: '1-2', angle: 270 });
    expect(component.outputBadge()).toBe('270° · Pages 1-2');
    expect(component.canRun()).toBeTrue();

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(
      'A destination warning from the backend.',
    );
  }));

  it('renders the usual result card output', fakeAsync(() => {
    wailsSpy.getJobStatus.and.resolveTo(terminalStatus('job-rotate'));

    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    expect(fixture.nativeElement.textContent).toContain('1 PDF rotated');
    expect(fixture.nativeElement.textContent).toContain('report_rotated.pdf');
  }));

  it('blocks the plan when the page selection exceeds the document', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('1-5'));
    flushMicrotasks();

    fixture.detectChanges();
    const element = fixture.nativeElement as HTMLElement;
    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(element.querySelector('.field-error')?.textContent).toContain(
      'exceeds the 3-page document',
    );

    const submitted = wailsSpy.planJob.calls
      .allArgs()
      .map((args) => args[0] as JobRequest)
      .filter((request) => request.options['pageSelection'] === '1-5');
    expect(submitted).toEqual([]);
  }));

  it('explains an invalid selection instead of planning it', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('5-2'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(
      (fixture.nativeElement as HTMLElement).querySelector('.field-error')
        ?.textContent,
    ).toContain('ascending order');
    expect(component.canRun()).toBeFalse();
  }));

  it('remembers the accepted options and restores them on init', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/report.pdf']);
    flushMicrotasks();

    component.selectAngle(180);
    component.onPagesInput(changeEvent('2'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.pdf.rotate',
      1,
      ['pageSelection', 'angle'],
    );
    expect(stored).toEqual({ pageSelection: '2', angle: 180 });

    const restoredFixture = TestBed.createComponent(PdfRotate);
    restoredFixture.detectChanges();
    const restored = restoredFixture.componentInstance;

    expect(restored.angle()).toBe(180);
    expect(restored.pageSelection()).toBe('2');
    expect(
      (restoredFixture.nativeElement.querySelector(
        '[data-angle="180"]',
      ) as HTMLButtonElement).getAttribute('aria-pressed'),
    ).toBe('true');
    restoredFixture.destroy();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Rotar',
    );
    expect(element.textContent).toContain('Ángulo');
    expect(element.textContent).toContain('Todas las páginas');
  });
});
