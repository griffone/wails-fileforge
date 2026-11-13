import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';

import { ImageConverter } from './image-converter';
import { JobRequestV1, JobStatusResponseV1, Wails, JobProgressEventV1 } from '../../services/wails';
import { Subject } from 'rxjs';

describe('ImageConverter', () => {
  let component: ImageConverter;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'listToolsV1',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
    ]);

    // Keep the Subject itself so specs can emit progress events; expose its
    // observable on the service spy the way the real Wails service does.
    jobProgressSubject = new Subject<JobProgressEventV1>();
    (wailsSpy as any).jobProgress$ = jobProgressSubject.asObservable();
    (wailsSpy as any).subscribeJobProgressV1 = jasmine.createSpy('subscribeJobProgressV1').and.callFake((cb: any) => {
      const s = jobProgressSubject.subscribe(cb);
      return () => s.unsubscribe();
    });

    await TestBed.configureTestingModule({
      imports: [ImageConverter],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(ImageConverter);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('maps dropped File[] to local input paths', () => {
    const fakeFiles = [new File(['x'], 'a.png')];
    component.onFileDropFiles(fakeFiles);

    expect(component.selectedInputPaths.length).toBe(1);
    expect(component.selectedInputPaths[0]).toContain('a.png');
  });

  it('builds single request payload for tool.image.convert', async () => {
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve({ success: true, message: 'ok', valid: true }));

    component.form.patchValue({
      jobMode: 'single',
      outputPath: '/tmp/out.webp',
      format: 'webp',
      quality: '80',
    });
    component.selectedInputPaths = ['/tmp/input.png'];

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.image.convert');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/input.png']);
    expect(req.options['outputPath']).toBe('/tmp/out.webp');
    expect(req.options['format']).toBe('webp');
    expect(req.options['quality']).toBe(80);
  });

  it('validates locally before backend calls', async () => {
    component.form.patchValue({ jobMode: 'single', outputPath: '/tmp/out.webp', format: 'webp', quality: '80' });
    component.selectedInputPaths = [];

    await component.validate();

    expect(component.validationMessage).toContain('Select exactly one input image');
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
  });

  it('runs and reaches success terminal status through polling', fakeAsync(() => {
    wailsSpy.validateJobV1.and.returnValue(Promise.resolve({ success: true, message: 'ok', valid: true }));
    wailsSpy.runJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'submitted', jobId: 'img-job', status: 'queued' })
    );

    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'img-job',
        success: false,
        message: 'running',
        toolId: 'tool.image.convert',
        status: 'running',
        progress: { current: 1, total: 2, stage: 'run', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };

    const done: JobStatusResponseV1 = {
      success: true,
      message: 'success',
      found: true,
      result: {
        jobId: 'img-job',
        success: true,
        message: 'done',
        toolId: 'tool.image.convert',
        status: 'success',
        progress: { current: 2, total: 2, stage: 'done', message: 'success' },
        items: [
          {
            inputPath: '/tmp/input.png',
            outputPath: '/tmp/out.webp',
            success: true,
            message: 'ok',
          },
        ],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.getJobStatusV1.and.returnValues(Promise.resolve(running), Promise.resolve(done));

    // Push progress events through the Subject exposed by the service spy
    setTimeout(() =>
      jobProgressSubject.next({ jobId: 'img-job', toolId: 'tool.image.convert', status: 'running', progress: running.result!.progress }),
      0
    );
    setTimeout(() =>
      jobProgressSubject.next({ jobId: 'img-job', toolId: 'tool.image.convert', status: 'success', progress: done.result!.progress }),
      500
    );

    component.form.patchValue({
      jobMode: 'single',
      outputPath: '/tmp/out.webp',
      format: 'webp',
      quality: '80',
    });
    component.selectedInputPaths = ['/tmp/input.png'];

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('img-job');
    expect(component.isPolling).toBeTrue();

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('success');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));
});
