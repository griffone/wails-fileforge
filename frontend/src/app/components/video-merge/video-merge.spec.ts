import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { VideoMerge } from './video-merge';
import {
  FileInfoV1,
  JobProgressEventV1,
  JobRequestV1,
  JobResultV1,
  JobStatusResponseV1,
  PlanJobResponseV1,
  Wails,
} from '../../services/wails';

describe('VideoMerge', () => {
  let fixture: ComponentFixture<VideoMerge>;
  let component: VideoMerge;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEventV1>;

  function planResponse(request: JobRequestV1): PlanJobResponseV1 {
    const format = String(request.options['targetFormat'] ?? 'mp4');
    const first = request.inputPaths[0] ?? '/tmp/video';
    const stem = (first.split(/[\\/]/).pop() ?? 'video').replace(
      /\.[^./\\]+$/,
      '',
    );
    const directory = request.outputDir || '/tmp';
    return {
      success: true,
      message: 'ok',
      outputs: [
        {
          inputPath: request.inputPaths.join(','),
          outputPath: `${directory}/${stem}_merged.${format}`,
          outputKind: 'file',
          exists: false,
          adjusted: false,
        },
      ],
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function fileInfo(path: string): FileInfoV1 {
    const name = path.split(/[\\/]/).pop() ?? path;
    return {
      path,
      name,
      directory: '/tmp',
      extension: name.split('.').pop() ?? '',
      sizeBytes: 2048,
      durationSeconds: 83,
      kind: 'video',
      exists: true,
    };
  }

  function runningStatus(jobId: string): JobStatusResponseV1 {
    const result: JobResultV1 = {
      jobId,
      success: false,
      message: 'running',
      toolId: 'tool.video.merge',
      status: 'running',
      progress: {
        current: 0,
        total: 2,
        stage: 'running',
        message: 'Merging videos',
      },
      items: [],
      startedAt: 1,
    };
    return { success: true, message: 'running', found: true, result };
  }

  function terminalStatus(
    jobId: string,
    inputPath: string,
    outputPath: string,
  ): JobStatusResponseV1 {
    const result: JobResultV1 = {
      jobId,
      success: true,
      message: 'done',
      toolId: 'tool.video.merge',
      status: 'success',
      progress: {
        current: 2,
        total: 2,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath,
          outputPath,
          outputs: [outputPath],
          success: true,
          message: 'Video merge successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
    return { success: true, message: 'done', found: true, result };
  }

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'planJobV1',
      'inspectFilesV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'openPath',
      'revealInFolder',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'isRuntimeAvailable',
      'listToolsV1',
      'validateJobV1',
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
        files: paths.map(fileInfo),
      }),
    );
    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-default',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.callFake((jobId: string) =>
      Promise.resolve(runningStatus(jobId)),
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
    wailsSpy.validateJobV1.and.resolveTo({
      success: true,
      message: 'ok',
      valid: true,
    });

    await TestBed.configureTestingModule({
      imports: [VideoMerge],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoMerge);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  it('requires at least two videos', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual(['/tmp/a.mp4']);
    expect(component.canRun()).toBeFalse();
    expect(component.primaryLabel()).toBe('Merge videos');
    expect(component.actionHelper()).toBe('Add one more video to merge.');
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  }));

  it('plans a merge with the default preset', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    expect(wailsSpy.planJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.video.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.mp4', '/tmp/b.mov']);
    expect(request.outputDir).toBe('');
    expect(request.options['targetFormat']).toBe('mp4');
    expect(request.options['qualityPreset']).toBe('medium');
    expect(component.plan()?.outputs[0].outputPath).toBe('/tmp/a_merged.mp4');
    expect(component.primaryLabel()).toBe('Merge 2 videos');
    expect(component.fileItems()[0].meta).toBe('2 KB · 1:23');
  }));

  it('reorders the queue and refreshes the plan input order', fakeAsync(() => {
    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov', '/tmp/c.mkv']);
    flushMicrotasks();

    component.moveDown('/tmp/a.mp4');
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([
      '/tmp/b.mov',
      '/tmp/a.mp4',
      '/tmp/c.mkv',
    ]);

    const request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.inputPaths).toEqual([
      '/tmp/b.mov',
      '/tmp/a.mp4',
      '/tmp/c.mkv',
    ]);
    expect(component.primaryLabel()).toBe('Merge 3 videos');
  }));

  it('uses the selected preset and the chosen folder in the plan', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/out');

    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    component.selectPreset('webm-balanced');
    flushMicrotasks();

    let request = wailsSpy.planJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.options['targetFormat']).toBe('webm');
    expect(request.options['qualityPreset']).toBe('medium');

    void component.chooseOutputFolder();
    flushMicrotasks();

    expect(component.outputDir()).toBe('/tmp/out');
    expect(component.destinationLabel()).toBe('/tmp/out');
    request = wailsSpy.planJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(request.outputDir).toBe('/tmp/out');
    expect(component.plan()?.outputs[0].outputPath).toBe(
      '/tmp/out/a_merged.webm',
    );

    component.useFirstVideoFolder();
    flushMicrotasks();
    expect(component.outputDir()).toBe('');
    expect(component.destinationLabel()).toBe('Next to the first video');
  }));

  it('runs without a validate step and shows the terminal result', fakeAsync(() => {
    const outputPath = '/tmp/a_merged.mp4';

    wailsSpy.runJobV1.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-2',
      status: 'queued',
    });
    wailsSpy.getJobStatusV1.and.resolveTo(
      terminalStatus('job-2', '/tmp/a.mp4', outputPath),
    );

    component.onPathsSelected(['/tmp/a.mp4', '/tmp/b.mov']);
    flushMicrotasks();

    void component.run();
    flushMicrotasks();

    expect(wailsSpy.runJobV1).toHaveBeenCalledTimes(1);
    const request = wailsSpy.runJobV1.calls.mostRecent()
      .args[0] as JobRequestV1;
    expect(request.toolId).toBe('tool.video.merge');
    expect(request.mode).toBe('single');
    expect(request.inputPaths).toEqual(['/tmp/a.mp4', '/tmp/b.mov']);
    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
    expect(component.isPolling()).toBeFalse();
    expect(component.terminalJob()?.status).toBe('success');
    expect(component.outputInfos()[outputPath].sizeBytes).toBe(2048);

    fixture.detectChanges();
    expect(fixture.nativeElement.textContent).toContain('1 video converted');

    void component.onOpen(outputPath);
    flushMicrotasks();
    expect(wailsSpy.openPath).toHaveBeenCalledWith(outputPath);
  }));

  it('rejects non-video files with a plain notice', fakeAsync(() => {
    component.onPathsSelected(['/tmp/notes.txt', '/tmp/photo.png']);
    flushMicrotasks();

    expect(component.selectedPaths()).toEqual([]);
    expect(component.notice()?.text).toBe(
      "notes.txt, photo.png weren't added because they aren't videos.",
    );
    expect(wailsSpy.planJobV1).not.toHaveBeenCalled();
  }));
});
