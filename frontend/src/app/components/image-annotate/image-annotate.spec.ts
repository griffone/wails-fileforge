import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { ImageAnnotate } from './image-annotate';
import { I18nService } from '../../services/i18n.service';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResultItem,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

describe('ImageAnnotate', () => {
  let fixture: ComponentFixture<ImageAnnotate>;
  let component: ImageAnnotate;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function firstDir(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index > 0 ? path.slice(0, index) : '';
  }

  function fileName(path: string): string {
    const index = Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\'));
    return index >= 0 ? path.slice(index + 1) : path;
  }

  function fileStem(path: string): string {
    return fileName(path).replace(/\.[^./\\]+$/, '');
  }

  function outputExtension(inputPath: string, request: JobRequest): string {
    const requested = String(request.options['format'] ?? '').toLowerCase();
    if (requested) {
      return requested === 'jpg' ? 'jpeg' : requested;
    }
    const extension = (inputPath.split('.').pop() ?? 'png').toLowerCase();
    return extension === 'jpg' ? 'jpeg' : extension;
  }

  function planResponse(request: JobRequest): PlanJobResponse {
    const outputs = request.inputPaths.map((inputPath) => {
      const directory = request.outputDir || firstDir(inputPath);
      const name = `${fileStem(inputPath)}_annotated.${outputExtension(
        inputPath,
        request,
      )}`;
      return {
        inputPath,
        outputPath: directory ? `${directory}/${name}` : name,
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

  function fileInfo(path: string): FileInfo {
    const name = fileName(path);
    return {
      path,
      name,
      directory: firstDir(path),
      extension: (name.split('.').pop() ?? '').toLowerCase(),
      sizeBytes: 2048,
      kind: 'image',
      width: 100,
      height: 80,
      exists: true,
    };
  }

  function status(
    jobId: string,
    jobStatus: JobResult['status'],
    items: JobResultItem[],
  ): JobStatusResponse {
    return {
      success: true,
      message: jobStatus,
      found: true,
      result: {
        jobId,
        success: jobStatus === 'success',
        message: jobStatus,
        toolId: 'tool.image.annotate',
        status: jobStatus,
        progress: {
          current: jobStatus === 'running' ? 0 : items.length,
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

  function pressKey(
    key: string,
    init: KeyboardEventInit = {},
    target: EventTarget = document.body,
  ): KeyboardEvent {
    const event = new KeyboardEvent('keydown', {
      key,
      bubbles: true,
      cancelable: true,
      ...init,
    });
    target.dispatchEvent(event);
    return event;
  }

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function primaryButton(): HTMLButtonElement {
    return fixture.nativeElement.querySelector(
      '.action-primary',
    ) as HTMLButtonElement;
  }

  function addDefaultOperation(): void {
    component.addOperation();
  }

  beforeEach(async () => {
    localStorage.removeItem('ff-preferences');
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJob',
      'inspectFiles',
      'getThumbnail',
      'getImagePreviewSource',
      'getImageAnnotatePreview',
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
    wailsSpy.getImagePreviewSource.and.resolveTo({
      success: true,
      message: 'ok',
      dataBase64: 'AA==',
      mimeType: 'image/png',
      width: 100,
      height: 80,
    });
    wailsSpy.getImageAnnotatePreview.and.resolveTo({
      success: true,
      message: 'ok',
      dataBase64: 'AA==',
      mimeType: 'image/png',
      width: 100,
      height: 80,
    });
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'annot-job-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(status(jobId, 'running', [])),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancelled',
      jobId: 'annot-job-1',
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
      imports: [ImageAnnotate],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(ImageAnnotate);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
  });

  it('plans one image as single with a default operation', fakeAsync(() => {
    addDefaultOperation();
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.image.annotate');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/photo.png']);
    expect(request.outputDir).toBe('');
    const operations = request.options['operations'] as Array<
      Record<string, unknown>
    >;
    expect(operations.length).toBe(1);
    expect(operations[0]['type']).toBe('rect');
    expect(request.options['format']).toBeUndefined();

    expect(component.previewUrl()).toContain('data:image/png;base64,');
    expect(component.imageWidth()).toBe(100);
    expect(component.imageHeight()).toBe(80);
    expect(component.primaryLabel()).toBe('Annotate image');
    expect(component.canRun()).toBeTrue();

    fixture.detectChanges();
    expect(primaryButton().disabled).toBeFalse();
  }));

  it('plans a batch automatically for two images', fakeAsync(() => {
    addDefaultOperation();
    component.onPathsSelected(['/tmp/a.png']);
    flushMicrotasks();
    component.onPathsSelected(['/tmp/b.jpg']);
    flushMicrotasks();

    const request = lastPlanRequest();
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.png', '/tmp/b.jpg']);
    expect(component.plan()?.outputs.length).toBe(2);
    expect(component.isBatch()).toBeTrue();
    expect(component.primaryLabel()).toBe('Annotate 2 images');

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain(
      'The same annotations apply to every image.',
    );
  }));

  it('refreshes the plan payload when an operation is added or changed', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    addDefaultOperation();
    flushMicrotasks();
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const added = lastPlanRequest().options['operations'] as Array<
      Record<string, unknown>
    >;
    expect(added.length).toBe(1);
    expect(added[0]['width']).toBe(40);
    expect(added[0]['height']).toBe(24);

    component.onOperationField('width', inputEvent('55'));
    component.applyOperationEdit();
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);

    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(2);
    const changed = lastPlanRequest().options['operations'] as Array<
      Record<string, unknown>
    >;
    expect(changed[0]['width']).toBe(55);
    expect(component.operations()[0].width).toBe(55);
  }));

  it('blocks the action when there are no operations', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    expect(component.hasOperations()).toBeFalse();
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toContain('annotation');
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    fixture.detectChanges();
    expect(primaryButton().disabled).toBeTrue();
  }));

  it('shows a failed plan inline and blocks the action', fakeAsync(() => {
    addDefaultOperation();
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    wailsSpy.planJob.and.resolveTo({
      success: false,
      message: 'The folder is not available.',
      outputs: [],
      error: {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'IMAGE_ANNOTATE_OUTPUT_DIR_INVALID',
        message: 'The folder is not available.',
      },
    });

    component.onOperationField('width', inputEvent('50'));
    component.applyOperationEdit();
    tick(250);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.planBlocked()).toBeTrue();
    expect(component.canRun()).toBeFalse();
    expect(primaryButton().disabled).toBeTrue();
    expect(
      fixture.nativeElement.querySelector('.output-plan-error'),
    ).not.toBeNull();
  }));

  it('runs from the fresh plan without a validation step', fakeAsync(() => {
    addDefaultOperation();
    component.onPathsSelected(['/tmp/a.png', '/tmp/b.jpg']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.planJob.calls.count()).toBe(planCalls + 1);
    expect(wailsSpy.validateJob).not.toHaveBeenCalled();
    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);

    const request = wailsSpy.runJob.calls.mostRecent()
      .args[0] as JobRequest;
    expect(request.toolId).toBe('tool.image.annotate');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual(['/tmp/a.png', '/tmp/b.jpg']);
    expect(request.outputDir).toBe('');
    expect(
      (request.options['operations'] as unknown[]).length,
    ).toBe(1);
    expect(component.isPolling()).toBeTrue();
    expect(component.activeJobId()).toBe('annot-job-1');

    component.reset();
  }));

  it('inspects terminal results and opens the annotated file', fakeAsync(() => {
    const inputPath = '/tmp/in/photo.png';
    const outputPath = '/tmp/in/photo_annotated.png';

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'annot-job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      status('annot-job-2', 'success', [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          outputCount: 1,
          success: true,
          message: 'image annotate successful',
        },
      ]),
    );

    addDefaultOperation();
    component.onPathsSelected([inputPath]);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('success');
    expect(component.terminalJob()).not.toBeNull();
    expect(component.isPolling()).toBeFalse();
    expect(component.activeJobId()).toBe('');
    expect(fixture.nativeElement.textContent).toContain(
      '1 image annotated',
    );

    const inspectedPaths = wailsSpy.inspectFiles.calls
      .allArgs()
      .map((args) => args[0] as string[]);
    expect(
      inspectedPaths.some((paths) => paths.includes(outputPath)),
    ).toBeTrue();

    const openButton = fixture.nativeElement.querySelector(
      '.result-open',
    ) as HTMLButtonElement;
    openButton.click();
    flushMicrotasks();

    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);
  }));

  it('rejects non-images and duplicates with a notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.txt']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toContain('notes.txt');
    expect(component.notice()?.text).toContain("isn't an image");
    expect(wailsSpy.planJob).not.toHaveBeenCalled();

    component.onPathsSelected(['/tmp/a.png', '/tmp/a.png']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.png']);
    expect(component.notice()?.text).toContain(
      'a.png is already in the queue.',
    );
    expect(component.fileItems().length).toBe(1);
  }));

  it('uses the chosen destination folder in the plan', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    addDefaultOperation();
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();
    const planCalls = wailsSpy.planJob.calls.count();

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    expect(component.isDefaultDestination()).toBeFalse();
    expect(wailsSpy.planJob.calls.count()).toBe(planCalls + 1);
    expect(lastPlanRequest().outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/photo_annotated.png',
    );

    component.useOriginalFolders();
    flushMicrotasks();

    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to each image');
    expect(lastPlanRequest().outputDir).toBe('');
  }));

  it('adds the chosen output format and omits it for preserve original', fakeAsync(() => {
    addDefaultOperation();
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    component.onFormatChange(inputEvent('webp'));
    flushMicrotasks();
    expect(component.outputFormat()).toBe('webp');
    expect(lastPlanRequest().options['format']).toBe('webp');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/photo_annotated.webp',
    );

    component.onFormatChange(inputEvent(''));
    flushMicrotasks();
    expect(component.outputFormat()).toBe('');
    expect(lastPlanRequest().options['format']).toBeUndefined();
  }));

  it('creates a rect operation using direct overlay drag coordinates', () => {
    component.previewUrl.set('data:image/png;base64,AA==');
    component.imageWidth.set(100);
    component.imageHeight.set(80);
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

    (component as unknown as { previewImage: unknown }).previewImage = {
      nativeElement: {
        getBoundingClientRect: () => rect,
      },
    };

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

    expect(component.operations().length).toBe(1);
    const operation = component.operations()[0];
    expect(operation.type).toBe('rect');
    expect(operation.x).toBe(10);
    expect(operation.y).toBe(20);
    expect(operation.width).toBe(40);
    expect(operation.height).toBe(40);
  });

  it('updates the selected operation when the form edit is applied', () => {
    addDefaultOperation();
    component.selectOperation(0);

    component.onOperationField('width', inputEvent('22'));
    component.onOperationField('height', inputEvent('11'));
    component.onOperationField('color', inputEvent('#00ff00'));

    expect(component.operations()[0].width).toBe(40);
    expect(component.formDirty()).toBeTrue();

    component.applyOperationEdit();

    expect(component.operations()[0].width).toBe(22);
    expect(component.operations()[0].height).toBe(11);
    expect(component.operations()[0].color).toBe('#00ff00');
    expect(component.formDirty()).toBeFalse();
  });

  it('undoes and redoes an added operation', () => {
    addDefaultOperation();
    expect(component.operations().length).toBe(1);
    expect(component.canUndo()).toBeTrue();
    expect(component.canRedo()).toBeFalse();

    component.undo();

    expect(component.operations().length).toBe(0);
    expect(component.selectedOperationIndex()).toBe(-1);
    expect(component.canUndo()).toBeFalse();
    expect(component.canRedo()).toBeTrue();

    component.redo();

    expect(component.operations().length).toBe(1);
    expect(component.operations()[0].type).toBe('rect');
    expect(component.selectedOperationIndex()).toBe(0);
    expect(component.canUndo()).toBeTrue();
    expect(component.canRedo()).toBeFalse();
  });

  it('caps the undo history at 50 snapshots', () => {
    for (let i = 0; i < 55; i += 1) {
      component.addOperation();
    }

    for (let i = 0; i < 55; i += 1) {
      component.undo();
    }

    expect(component.operations().length).toBe(5);
    expect(component.canUndo()).toBeFalse();
    expect(component.canRedo()).toBeTrue();
  });

  it('undoes removals, reorders and clear all', () => {
    const xs = (): Array<number | undefined> =>
      component.operations().map((operation) => operation.x);

    component.onOperationField('x', inputEvent('10'));
    component.addOperation();
    component.onOperationField('x', inputEvent('20'));
    component.addOperation();
    component.onOperationField('x', inputEvent('30'));
    component.addOperation();
    expect(xs()).toEqual([10, 20, 30]);

    component.removeOperation(1);
    expect(xs()).toEqual([10, 30]);
    component.undo();
    expect(xs()).toEqual([10, 20, 30]);
    component.redo();
    expect(xs()).toEqual([10, 30]);
    component.undo();
    expect(xs()).toEqual([10, 20, 30]);

    component.moveOperationUp(2);
    expect(xs()).toEqual([10, 30, 20]);
    component.undo();
    expect(xs()).toEqual([10, 20, 30]);

    component.moveOperationDown(0);
    expect(xs()).toEqual([20, 10, 30]);
    component.undo();
    expect(xs()).toEqual([10, 20, 30]);

    component.clearOperations();
    expect(component.operations().length).toBe(0);
    expect(component.selectedOperationIndex()).toBe(-1);

    component.undo();
    expect(xs()).toEqual([10, 20, 30]);
  });

  it('drops the redo stack when a new edit happens after undo', () => {
    component.onOperationField('x', inputEvent('10'));
    component.addOperation();
    component.undo();
    expect(component.canRedo()).toBeTrue();

    component.onOperationField('x', inputEvent('20'));
    component.addOperation();

    expect(component.canRedo()).toBeFalse();
    expect(component.operations().length).toBe(1);
    expect(component.operations()[0].x).toBe(20);

    component.redo();
    expect(component.operations().length).toBe(1);
    expect(component.operations()[0].x).toBe(20);
  });

  it('duplicates the selected operation with a small offset', () => {
    component.onOperationField('x', inputEvent('5'));
    component.onOperationField('y', inputEvent('6'));
    component.addOperation();

    component.duplicateOperation();

    const [source, copy] = component.operations();
    expect(component.operations().length).toBe(2);
    expect(copy.type).toBe(source.type);
    expect(copy.width).toBe(source.width);
    expect(copy.x).toBe(15);
    expect(copy.y).toBe(16);
    expect(copy.x2).toBe((source.x2 ?? 0) + 10);
    expect(copy.y2).toBe((source.y2 ?? 0) + 10);
    expect(component.selectedOperationIndex()).toBe(1);

    component.undo();
    expect(component.operations().length).toBe(1);
  });

  it('handles annotation shortcuts outside form fields', () => {
    addDefaultOperation();

    const undo = pressKey('z', { ctrlKey: true });
    expect(undo.defaultPrevented).toBeTrue();
    expect(component.operations().length).toBe(0);
    expect(component.canRedo()).toBeTrue();

    pressKey('z', { metaKey: true, shiftKey: true });
    expect(component.operations().length).toBe(1);

    pressKey('z', { ctrlKey: true });
    pressKey('y', { ctrlKey: true });
    expect(component.operations().length).toBe(1);

    const duplicate = pressKey('d', { ctrlKey: true });
    expect(duplicate.defaultPrevented).toBeTrue();
    expect(component.operations().length).toBe(2);
    expect(component.selectedOperationIndex()).toBe(1);

    pressKey('Delete');
    expect(component.operations().length).toBe(1);
    expect(component.selectedOperationIndex()).toBe(-1);

    component.selectOperation(0);
    pressKey('Backspace');
    expect(component.operations().length).toBe(0);

    addDefaultOperation();
    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();
    const ignoredUndo = pressKey('z', { ctrlKey: true }, input);
    expect(ignoredUndo.defaultPrevented).toBeFalse();
    expect(component.operations().length).toBe(1);

    const textarea = document.createElement('textarea');
    document.body.appendChild(textarea);
    const ignoredDelete = pressKey('Delete', {}, textarea);
    expect(ignoredDelete.defaultPrevented).toBeFalse();

    const editable = document.createElement('div');
    editable.contentEditable = 'true';
    document.body.appendChild(editable);
    const ignoredDuplicate = pressKey('d', { ctrlKey: true }, editable);
    expect(ignoredDuplicate.defaultPrevented).toBeFalse();
    expect(component.operations().length).toBe(1);

    input.remove();
    textarea.remove();
    editable.remove();
  });

  it('refreshes the plan after undo and redo', fakeAsync(() => {
    component.onPathsSelected(['/tmp/photo.png']);
    flushMicrotasks();

    addDefaultOperation();
    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);

    addDefaultOperation();
    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(2);

    component.undo();
    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(3);
    expect(
      (lastPlanRequest().options['operations'] as unknown[]).length,
    ).toBe(1);

    component.redo();
    tick(250);
    flushMicrotasks();
    expect(wailsSpy.planJob).toHaveBeenCalledTimes(4);
    expect(
      (lastPlanRequest().options['operations'] as unknown[]).length,
    ).toBe(2);

    component.reset();
    flushMicrotasks();
  }));

  it('retries only failed inputs with the same annotations and folder', fakeAsync(() => {
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'annot-retry-1',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      status('annot-retry-1', 'partial_success', [
        {
          inputPath: '/tmp/a.png',
          outputPath: '/tmp/a_annotated.png',
          outputs: ['/tmp/a_annotated.png'],
          success: true,
          message: 'image annotate successful',
        },
        {
          inputPath: '/tmp/b.jpg',
          outputPath: '',
          success: false,
          message: 'image annotate failed',
          error: {
            code: 'EXEC_IO_TRANSIENT',
            detail_code: 'IMAGE_ANNOTATE_BATCH_ITEM',
            message: 'image annotate failed',
          },
        },
      ]),
    );

    addDefaultOperation();
    component.onPathsSelected(['/tmp/a.png', '/tmp/b.jpg']);
    flushMicrotasks();

    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');
    void component.chooseOutputFolder();
    flushMicrotasks();

    void component.run();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.jobResult()?.status).toBe('partial_success');

    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'annot-retry-2',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.resolveTo(
      status('annot-retry-2', 'running', []),
    );

    void component.retryFailed();
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/b.jpg']);

    const runCalls = wailsSpy.runJob.calls.allArgs();
    const retryRequest = runCalls[runCalls.length - 1][0] as JobRequest;
    expect(retryRequest.mode).toBe('single');
    expect(retryRequest.inputPaths).toEqual(['/tmp/b.jpg']);
    expect(retryRequest.outputDir).toBe('/tmp/out');
    expect(
      (retryRequest.options['operations'] as unknown[]).length,
    ).toBe(1);

    component.reset();
  }));

  it('renders the file-first page without the legacy mode and path controls', () => {
    const element = fixture.nativeElement as HTMLElement;

    expect(element.querySelector('.eyebrow')?.textContent).toContain('IMAGE');
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Image',
    );
    expect(element.querySelector('.page-title')?.textContent).toContain(
      'Annotate',
    );
    expect(element.querySelectorAll('.page-head a').length).toBe(0);

    expect(element.querySelector('#annot-mode')).toBeNull();
    expect(element.querySelector('#outputPath')).toBeNull();
    expect(element.querySelector('app-tool-execution-panel')).toBeNull();

    const legacy = component as unknown as Record<string, unknown>;
    expect(legacy['validate']).toBeUndefined();
    expect(legacy['panelFields']).toBeUndefined();
    expect(legacy['form']).toBeUndefined();
  });

  it('renders the page copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();

    const element = fixture.nativeElement as HTMLElement;
    const title = element.querySelector('.page-title')?.textContent ?? '';
    expect(title).toContain('Anotar');
    expect(title).toContain('imágenes');
    expect(
      element.querySelector('[data-tool="text"]')?.textContent,
    ).toContain('Texto');
    expect(
      element.querySelector('.operation-form-actions .btn-primary')
        ?.textContent,
    ).toContain('Añadir operación');
  });
});
