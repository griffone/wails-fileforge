import {
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { ImageAnnotate } from './image-annotate';
import { JobRequestV1, JobStatusResponseV1, Wails } from '../../services/wails';

describe('ImageAnnotate', () => {
  let component: ImageAnnotate;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'getImagePreviewSourceV1',
      'getImageAnnotatePreviewV1',
      'isRuntimeAvailable',
      'getImageCropPreviewV1',
    ]);

    wailsSpy.getImagePreviewSourceV1.and.returnValue(
      Promise.resolve({
        success: true,
        message: 'ok',
        dataBase64: 'AA==',
        mimeType: 'image/png',
        width: 100,
        height: 80,
      })
    );
    wailsSpy.getImageAnnotatePreviewV1.and.returnValue(
      Promise.resolve({
        success: true,
        message: 'ok',
        dataBase64: 'AA==',
        mimeType: 'image/png',
        width: 100,
        height: 80,
      })
    );

    await TestBed.configureTestingModule({
      imports: [ImageAnnotate],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(ImageAnnotate);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('builds annotate request payload with deterministic operation order', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.selectedInputPath = '/tmp/in.png';
    component.operationForm.patchValue({
      type: 'rect',
      x: '1',
      y: '2',
      width: '10',
      height: '6',
      strokeWidth: '2',
      opacity: '0.8',
      color: '#ff0000',
    });
    component.addOperation();

    component.operationForm.patchValue({
      type: 'blur',
      x: '3',
      y: '4',
      width: '7',
      height: '5',
      blurIntensity: '45',
    });
    component.addOperation();

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.image.annotate');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/in.png']);

    const operations = req.options['operations'] as any[];
    expect(operations.length).toBe(2);
    expect(operations[0].type).toBe('rect');
    expect(operations[1].type).toBe('blur');
  });

  it('creates rect operation using direct overlay drag coordinates', () => {
    component.selectedInputPath = '/tmp/in.png';
    component.sourcePreviewDataUrl = 'data:image/png;base64,AA==';
    component.sourceImageWidth = 100;
    component.sourceImageHeight = 80;
    component.setActiveTool('rect');

    const rect = {
      left: 0,
      top: 0,
      width: 100,
      height: 80,
      x: 0,
      y: 0,
      right: 100,
      bottom: 80,
      toJSON: () => ({}),
    } as DOMRect;

    component.previewImage = {
      nativeElement: {
        getBoundingClientRect: () => rect,
      },
    } as any;

    component.onOverlayPointerDown({
      clientX: 10,
      clientY: 20,
      currentTarget: { setPointerCapture: () => undefined },
      pointerId: 1,
    } as unknown as PointerEvent);

    component.onOverlayPointerMove({
      clientX: 50,
      clientY: 60,
    } as unknown as PointerEvent);

    component.onOverlayPointerUp();

    expect(component.operations.length).toBe(1);
    const op = component.operations[0];
    expect(op.type).toBe('rect');
    expect(op.x).toBe(10);
    expect(op.y).toBe(20);
    expect(op.width).toBe(40);
    expect(op.height).toBe(40);
  });

  it('updates selected operation from advanced settings form', () => {
    component.selectedInputPath = '/tmp/in.png';
    component.operationForm.patchValue({
      type: 'rect',
      x: '1',
      y: '2',
      width: '10',
      height: '6',
      color: '#ff0000',
    });
    component.addOperation();
    component.selectOperation(0);
    component.setActiveTool('select');

    component.operationForm.patchValue({ width: '22', height: '11', color: '#00ff00' });

    expect(component.operations[0].width).toBe(22);
    expect(component.operations[0].height).toBe(11);
    expect(component.operations[0].color).toBe('#00ff00');
  });

  it('requires explicit redact confirmation before run', async () => {
    component.selectedInputPath = '/tmp/in.png';
    component.operationForm.patchValue({
      type: 'redact',
      x: '2',
      y: '2',
      width: '5',
      height: '5',
      color: '#000000',
    });
    component.addOperation();

    await component.run();

    expect(component.submitMessage).toContain('confirm irreversible redact operations');
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
  });

  it('runs and completes with success status', fakeAsync(() => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );
    wailsSpy.runJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'submitted', jobId: 'annot-job-1', status: 'queued' })
    );

    const running: JobStatusResponseV1 = {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId: 'annot-job-1',
        success: false,
        message: 'working',
        toolId: 'tool.image.annotate',
        status: 'running',
        progress: { current: 0, total: 1, stage: 'running', message: 'running' },
        items: [],
        startedAt: Date.now(),
      },
    };

    const done: JobStatusResponseV1 = {
      success: true,
      message: 'done',
      found: true,
      result: {
        jobId: 'annot-job-1',
        success: true,
        message: 'job success',
        toolId: 'tool.image.annotate',
        status: 'success',
        progress: { current: 1, total: 1, stage: 'running', message: 'done' },
        items: [
          {
            inputPath: '/tmp/in.png',
            outputPath: '/tmp/in_annotated.png',
            success: true,
            message: 'image annotate successful',
          },
        ],
        startedAt: Date.now(),
        endedAt: Date.now(),
      },
    };

    wailsSpy.getJobStatusV1.and.returnValues(Promise.resolve(running), Promise.resolve(done));

    component.selectedInputPath = '/tmp/in.png';
    component.operationForm.patchValue({
      type: 'blur',
      x: '1',
      y: '1',
      width: '4',
      height: '4',
      blurIntensity: '20',
    });
    component.addOperation();

    void component.run();
    flushMicrotasks();

    expect(component.activeJobId).toBe('annot-job-1');

    tick(1000);
    flushMicrotasks();

    expect(component.jobResult?.status).toBe('success');
    expect(component.isPolling).toBeFalse();
    expect(component.activeJobId).toBe('');
  }));
});
