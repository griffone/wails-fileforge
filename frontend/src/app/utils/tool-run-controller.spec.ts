import { Subject } from 'rxjs';
import { fakeAsync, flushMicrotasks } from '@angular/core/testing';

import { ActivityService, ActivityTask } from '../services/activity.service';
import { DropIntakeService } from '../services/drop-intake.service';
import { I18nService } from '../services/i18n.service';
import {
  FileInfo,
  JobProgressEvent,
  JobRequest,
  JobResult,
  JobStatusResponse,
  PlanJobResponse,
  RunJobResponse,
  UndoResult,
  VerificationReport,
  Wails,
} from '../services/wails';
import {
  ToolRunController,
  ToolRunControllerConfig,
  toolPathKey,
} from './tool-run-controller';

const REQUEST: JobRequest = {
  toolId: 'tool.pdf.merge',
  mode: 'single',
  inputPaths: ['/tmp/a.pdf'],
  outputDir: '',
  options: {},
};

const MESSAGES: ToolRunControllerConfig['messages'] = {
  start: 'pdfMerge.error.start',
  planFailed: 'pdfMerge.error.plan',
  waiting: 'pdfMerge.status.waiting',
  inspectFailed: 'pdfMerge.error.inspect',
  statusMissing: 'pdfMerge.error.status',
  cancelFailed: 'pdfMerge.error.cancel',
  openFileFailed: 'pdfMerge.error.openFile',
  revealFailed: 'pdfMerge.error.openFolder',
};

const NOTICE: ToolRunControllerConfig['notice'] = {
  rejectedOne: 'pdfMerge.notice.rejectedOne',
  rejectedMany: 'pdfMerge.notice.rejectedMany',
  duplicateOne: 'pdfMerge.notice.duplicateOne',
  duplicateMany: 'pdfMerge.notice.duplicateMany',
  rejectedManyFormat: 'names',
};

const I18N = {
  t: (key: string, params?: Record<string, unknown>) =>
    params ? `${key}:${JSON.stringify(params)}` : key,
  tOr: (_key: string, fallback: string) => fallback,
} as unknown as I18nService;

function planResponse(): PlanJobResponse {
  return {
    success: true,
    message: 'ok',
    outputs: [
      {
        inputPath: '/tmp/a.pdf',
        outputPath: '/tmp/a_merged.pdf',
        outputKind: 'file',
        exists: false,
        adjusted: false,
      },
    ],
  };
}

function runResponse(jobId = 'job-1'): RunJobResponse {
  return {
    success: true,
    message: 'queued',
    jobId,
    status: 'queued',
  };
}

function jobResult(
  status: JobResult['status'],
  jobId = 'job-1',
): JobResult {
  return {
    jobId,
    success: status === 'success' || status === 'partial_success',
    message: status,
    toolId: 'tool.pdf.merge',
    status,
    progress: {
      current: status === 'running' ? 1 : 2,
      total: 2,
      stage: status,
      message: status,
    },
    items:
      status === 'success' || status === 'partial_success'
        ? [
            {
              inputPath: '/tmp/a.pdf',
              outputPath: '/tmp/a_merged.pdf',
              outputs: ['/tmp/a_merged.pdf'],
              success: true,
              message: 'done',
            },
          ]
        : [],
    startedAt: 1,
    endedAt: status === 'running' ? undefined : 2,
  };
}

function statusResponse(result: JobResult): JobStatusResponse {
  return { success: true, message: result.status, found: true, result };
}

function verificationReport(): VerificationReport {
  return {
    success: true,
    message: 'ok',
    outputs: [
      {
        path: '/tmp/a_merged.pdf',
        exists: true,
        sizeBytes: 10,
        checks: [{ label: 'exists', passed: true }],
      },
    ],
  } as VerificationReport;
}

function undoResult(message: string): UndoResult {
  return { success: true, message, moved: [], skipped: [] };
}

function fileInfo(path: string): FileInfo {
  return {
    path,
    name: path.split('/').pop() ?? path,
    directory: '/tmp',
    extension: 'pdf',
    sizeBytes: 42,
    kind: 'pdf',
    exists: true,
  };
}

describe('ToolRunController', () => {
  let wailsSpy: Wails;
  let jobProgress: Subject<JobProgressEvent>;
  let jobState: Subject<JobResult>;
  let activity: ActivityService;
  let dropIntake: DropIntakeService;
  let planJob: jasmine.Spy;
  let runJob: jasmine.Spy;
  let getJobStatus: jasmine.Spy;
  let cancelJob: jasmine.Spy;
  let inspectFiles: jasmine.Spy;
  let verifyJob: jasmine.Spy;
  let undoJob: jasmine.Spy;
  let restoreJob: jasmine.Spy;
  let controller: ToolRunController;

  function create(
    overrides: Partial<ToolRunControllerConfig> = {},
  ): ToolRunController {
    return new ToolRunController({
      toolId: 'tool.pdf.merge',
      wails: wailsSpy,
      i18n: I18N,
      activity,
      dropIntake,
      buildRequest: () => REQUEST,
      acceptsPath: (path) => path.endsWith('.pdf'),
      canPlan: () => true,
      canRun: () => true,
      describeFileInfo: (info) =>
        typeof info.sizeBytes === 'number' ? `${info.sizeBytes} B` : '',
      messages: MESSAGES,
      notice: NOTICE,
      ...overrides,
    });
  }

  beforeEach(() => {
    planJob = jasmine.createSpy('planJob').and.resolveTo(planResponse());
    runJob = jasmine.createSpy('runJob').and.resolveTo(runResponse());
    getJobStatus = jasmine
      .createSpy('getJobStatus')
      .and.callFake((jobId: string) =>
        Promise.resolve(statusResponse(jobResult('running', jobId))),
      );
    cancelJob = jasmine
      .createSpy('cancelJob')
      .and.resolveTo({ success: true, message: 'cancelled', jobId: 'job-1' });
    inspectFiles = jasmine
      .createSpy('inspectFiles')
      .and.callFake((paths: string[]) =>
        Promise.resolve({
          success: true,
          message: 'ok',
          files: paths.map(fileInfo),
        }),
      );
    verifyJob = jasmine
      .createSpy('verifyJob')
      .and.resolveTo(verificationReport());
    undoJob = jasmine
      .createSpy('undoJob')
      .and.resolveTo(undoResult('Moved to trash.'));
    restoreJob = jasmine
      .createSpy('restoreJob')
      .and.resolveTo(undoResult('Restored.'));

    jobProgress = new Subject<JobProgressEvent>();
    jobState = new Subject<JobResult>();

    wailsSpy = {
      jobProgress$: jobProgress.asObservable(),
      jobState$: jobState.asObservable(),
      planJob,
      runJob,
      getJobStatus,
      cancelJob,
      inspectFiles,
      verifyJob,
      undoJob,
      restoreJob,
      getThumbnail: jasmine
        .createSpy('getThumbnail')
        .and.resolveTo(null),
      openPath: jasmine
        .createSpy('openPath')
        .and.resolveTo({ success: true, message: 'opened' }),
      revealInFolder: jasmine
        .createSpy('revealInFolder')
        .and.resolveTo({ success: true, message: 'revealed' }),
    } as unknown as Wails;

    activity = {
      activeJobForTool: jasmine
        .createSpy('activeJobForTool')
        .and.returnValue(null),
    } as unknown as ActivityService;

    dropIntake = {
      take: jasmine.createSpy('take').and.returnValue([]),
    } as unknown as DropIntakeService;

    controller = create();
  });

  it('keeps a failed plan as the source of truth and never runs', fakeAsync(() => {
    planJob.and.resolveTo({
      success: false,
      message: 'invalid page range',
      outputs: [],
      error: {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'PDF_MERGE_PAGE_RANGE_INVALID',
        message: 'invalid page range',
      },
    });

    controller.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    expect(controller.plan()?.success).toBeFalse();
    expect(controller.planError()?.detail_code).toBe(
      'PDF_MERGE_PAGE_RANGE_INVALID',
    );

    void controller.run();
    flushMicrotasks();

    expect(runJob).not.toHaveBeenCalled();
  }));

  it('enriches the queue and reports rejected and duplicate intakes', fakeAsync(() => {
    controller.onPathsSelected([
      '/tmp/notes.txt',
      '/tmp/a.pdf',
      '/tmp/a.pdf',
    ]);
    flushMicrotasks();

    expect(controller.selectedPaths()).toEqual(['/tmp/a.pdf']);
    expect(controller.fileItems().length).toBe(1);
    expect(controller.fileItems()[0].meta).toBe('42 B');
    expect(controller.notice()?.text).toContain('pdfMerge.notice.rejectedOne');
    expect(controller.notice()?.text).toContain(
      'pdfMerge.notice.duplicateOne',
    );
    expect(planJob).toHaveBeenCalledTimes(1);
  }));

  it('runs, observes progress and settles the terminal result', fakeAsync(() => {
    controller.onPathsSelected(['/tmp/a.pdf']);
    flushMicrotasks();

    void controller.run();
    flushMicrotasks();

    expect(runJob).toHaveBeenCalledOnceWith(REQUEST);
    expect(controller.activeJobId()).toBe('job-1');
    expect(controller.isPolling()).toBeTrue();

    jobProgress.next({
      jobId: 'job-1',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: {
        current: 1,
        total: 2,
        stage: 'merge',
        message: 'Merging 1 of 2',
      },
    });

    expect(controller.jobResult()?.progress.stage).toBe('merge');

    jobState.next(jobResult('success'));
    flushMicrotasks();

    expect(controller.terminalJob()?.status).toBe('success');
    expect(controller.isPolling()).toBeFalse();
    expect(controller.activeJobId()).toBe('');
    expect(controller.outputInfos()['/tmp/a_merged.pdf'].sizeBytes).toBe(42);
  }));

  it('cancels the active job and surfaces a failed cancel', fakeAsync(() => {
    controller.activeJobId.set('job-1');
    cancelJob.and.resolveTo({
      success: false,
      message: 'Could not cancel.',
      jobId: 'job-1',
    });

    void controller.cancel();
    flushMicrotasks();

    expect(cancelJob).toHaveBeenCalledOnceWith('job-1');
    expect(controller.actionMessage()).toBe('Could not cancel.');
  }));

  it('resumes an active background job after navigation', fakeAsync(() => {
    const task: ActivityTask = {
      id: 'job-resumed',
      kind: 'tool',
      title: 'PDF Merge',
      route: '/pdf-merge',
      toolId: 'tool.pdf.merge',
      status: 'running',
      stage: 'merge',
      message: 'Merging 1 of 2',
      current: 1,
      total: 2,
      percent: 50,
      startedAt: 1,
      endedAt: null,
      etaSeconds: 3,
      inputCount: 1,
      outputCount: 0,
      errorMessage: '',
    };
    (activity.activeJobForTool as jasmine.Spy).and.returnValue(task);

    expect(controller.resume()).toBeTrue();
    flushMicrotasks();

    expect(controller.activeJobId()).toBe('job-resumed');
    expect(controller.jobResult()?.status).toBe('running');
    expect(controller.isPolling()).toBeTrue();
  }));

  it('verifies a terminal job and marks verification unavailable without outputs', async () => {
    controller.jobResult.set(jobResult('success'));

    const report = await controller.verifyOutputs();
    expect(verifyJob).toHaveBeenCalledOnceWith('job-1');
    expect(report?.outputs.length).toBe(1);
    expect(controller.verification()?.outputs.length).toBe(1);
    expect(controller.verificationUnavailable()).toBeFalse();

    verifyJob.and.resolveTo({
      success: false,
      message: 'no outputs',
      outputs: [],
    } as VerificationReport);

    const missing = await controller.verifyOutputs();
    expect(missing).toBeNull();
    expect(controller.verification()).toBeNull();
    expect(controller.verificationUnavailable()).toBeTrue();
  });

  it('undoes a job and swaps the action to restore', async () => {
    controller.jobResult.set(jobResult('success'));

    const result = await controller.undo();
    expect(undoJob).toHaveBeenCalledOnceWith('job-1');
    expect(result?.message).toBe('Moved to trash.');
    expect(controller.undoMode()).toBe('restore');
    expect(controller.undoMessage()).toBe('Moved to trash.');

    await controller.restore();
    expect(restoreJob).toHaveBeenCalledOnceWith('job-1');
    expect(controller.undoMode()).toBe('undo');
    expect(controller.undoMessage()).toBe('Restored.');
  });

  it('treats Windows and UNC paths case-insensitively', () => {
    expect(toolPathKey('C:\\Docs\\A.pdf')).toBe('c:/docs/a.pdf');
    expect(toolPathKey('//Server/Share/A.pdf')).toBe('//server/share/a.pdf');
    expect(toolPathKey('/tmp/A.pdf')).toBe('/tmp/A.pdf');
  });
});
