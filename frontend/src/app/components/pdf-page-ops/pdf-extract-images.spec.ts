import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';

import { PdfExtractImages } from './pdf-extract-images';
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

describe('PdfExtractImages', () => {
  let fixture: ComponentFixture<PdfExtractImages>;
  let component: PdfExtractImages;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: inputPath.replace(/\.[^./\\]+$/, '_images'),
        outputKind: 'directory',
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string, pageCount = 3): FileInfo {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp/in',
      extension: 'pdf',
      sizeBytes: 5120,
      pageCount,
      kind: 'pdf',
      exists: true,
    };
  }

  function terminalStatus(jobId: string): JobStatusResponse {
    const result: JobResult = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.pdf.extract_images',
      status: 'success',
      progress: {
        current: 100,
        total: 100,
        stage: 'pdf.extract_images',
        message: 'Finished',
      },
      items: [
        {
          inputPath: '/tmp/in/scan.pdf',
          outputPath: '/tmp/in/scan_images',
          outputs: [
            '/tmp/in/scan_images/scan_p1_photo.png',
            '/tmp/in/scan_images/scan_p1_logo.png',
            '/tmp/in/scan_images/scan_p2_chart.png',
          ],
          outputCount: 3,
          success: true,
          message: 'Extracted 3 images to scan_images',
          details: { imageCount: 3 },
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
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
      jobId: 'job-extract',
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
      jobId: 'job-extract',
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
      imports: [PdfExtractImages],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(PdfExtractImages);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('states that the output is a folder and renders the defaults', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(
      (element.querySelector(
        '#pdf-extract-images-pages',
      ) as HTMLInputElement).value,
    ).toBe('');
    expect(component.outputBadge()).toBe('Folder output');
    expect(component.primaryLabel()).toBe('Extract images');
    expect(element.querySelector('.folder-note')?.textContent).toContain(
      '_images',
    );
    expect(element.querySelector('.folder-note')?.textContent).toContain(
      'folder, not a PDF',
    );
  });

  it('sends exactly the manifest option keys', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/scan.pdf']);
    flushMicrotasks();

    let request = wailsSpy.planJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.pdf.extract_images');
    expect(request.mode).toBe('single');
    expect(request.options).toEqual({ pageSelection: '' });

    component.onPagesInput(changeEvent('1-2'));
    tick(200);
    flushMicrotasks();

    request = wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
    expect(request.options).toEqual({ pageSelection: '1-2' });
    expect(component.canRun()).toBeTrue();
  }));

  it('blocks an out-of-bounds selection with the right message', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/scan.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('1-4'));
    flushMicrotasks();

    fixture.detectChanges();
    expect(component.plan()).toBeNull();
    expect(component.canRun()).toBeFalse();
    expect(
      (fixture.nativeElement as HTMLElement).querySelector('.field-error')
        ?.textContent,
    ).toContain('exceeds the 3-page document');

    const submitted = wailsSpy.planJob.calls
      .allArgs()
      .map((args) => args[0] as JobRequest)
      .filter((request) => request.options['pageSelection'] === '1-4');
    expect(submitted).toEqual([]);
  }));

  it('shows the _images folder in the plan card', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/scan.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';
    expect(text).toContain('scan_images');
    expect(text).toContain('/tmp/in');
    expect(
      component.plan()?.outputs[0].outputKind,
    ).toBe('directory');
  }));

  it('renders the folder and the extracted image count', fakeAsync(() => {
    wailsSpy.getJobStatus.and.resolveTo(terminalStatus('job-extract'));

    component.onPathsSelected(['/tmp/in/scan.pdf']);
    flushMicrotasks();
    fixture.detectChanges();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.terminalJob()?.status).toBe('success');
    expect(component.extractedFolders()).toEqual([
      {
        path: '/tmp/in/scan_images',
        name: 'scan_images',
        directory: '/tmp/in',
        countLabel: '3 images',
      },
    ]);
    expect(component.totalExtractedLabel()).toBe('3 images');

    const element = fixture.nativeElement as HTMLElement;
    expect(element.textContent).toContain('Extracted folders');
    expect(element.querySelector('.extracted-count')?.textContent).toContain(
      '3 images',
    );
    expect(element.querySelector('.extracted-directory')?.textContent).toContain(
      '/tmp/in',
    );

    // A folder is not inspected as a file: only the input was read once.
    expect(wailsSpy.inspectFiles).toHaveBeenCalledTimes(1);
    expect(component.outputInfos()).toEqual({});
  }));

  it('remembers the accepted selection and restores it on init', fakeAsync(() => {
    component.onPathsSelected(['/tmp/in/scan.pdf']);
    flushMicrotasks();

    component.onPagesInput(changeEvent('2'));
    tick(200);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    const stored = TestBed.inject(PreferencesService).readToolOptions(
      'tool.pdf.extract_images',
      1,
      ['pageSelection'],
    );
    expect(stored).toEqual({ pageSelection: '2' });

    const restoredFixture = TestBed.createComponent(PdfExtractImages);
    restoredFixture.detectChanges();

    expect(restoredFixture.componentInstance.pageSelection()).toBe('2');
    restoredFixture.destroy();
  }));

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Extraer',
    );
    expect(element.textContent).toContain('Páginas a revisar');
    expect(element.textContent).toContain('Salida en carpeta');
  });
});
