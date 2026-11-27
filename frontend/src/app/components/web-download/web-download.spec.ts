import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
  tick,
} from '@angular/core/testing';
import { Subject } from 'rxjs';

import { WebDownload } from './web-download';
import { I18nService } from '../../services/i18n.service';
import { clearToolStorage } from '../../../testing/tool-storage';
import {
  DownloadAudioOption,
  DownloadLinkInfoResponse,
  DownloadVideoOption,
  JobProgressEvent,
  JobRequest,
  JobStatusResponse,
  PlanJobResponse,
  Wails,
} from '../../services/wails';

const LINK_A = 'https://example.test/watch?v=alpha';
const LINK_B = 'https://example.test/watch?v=beta';
const PLAYLIST = 'https://example.test/playlist?list=fixture';

function videoOption(
  height: number,
  sizeBytes: number,
  fps = 30,
): DownloadVideoOption {
  return {
    height,
    ext: 'mp4',
    vcodec: 'avc1.640028',
    fps,
    sizeBytes,
    hdr: false,
  };
}

function audioOption(bitrateKbps: number): DownloadAudioOption {
  return {
    ext: 'm4a',
    acodec: 'mp4a.40.2',
    bitrateKbps,
    sizeBytes: 1024,
  };
}

function probeVideo(
  url: string,
  title: string,
  id: string,
  heights: number[],
): DownloadLinkInfoResponse {
  return {
    success: true,
    message: 'Link formats loaded.',
    url,
    id,
    title,
    uploader: 'Fixture',
    thumbnailUrl: '',
    durationSeconds: 90,
    isPlaylist: false,
    entries: [],
    video: heights.map((height) =>
      height === 1080
        ? videoOption(1080, 320 * 1024 * 1024)
        : videoOption(height, 150 * 1024 * 1024),
    ),
    audio: [audioOption(128)],
  };
}

function probeFailure(url: string, message: string): DownloadLinkInfoResponse {
  return {
    success: false,
    message,
    url,
    id: '',
    title: '',
    uploader: '',
    thumbnailUrl: '',
    durationSeconds: 0,
    isPlaylist: false,
    entries: [],
    video: [],
    audio: [],
  };
}

describe('WebDownload', () => {
  let fixture: ComponentFixture<WebDownload>;
  let component: WebDownload;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let jobProgressSubject: Subject<JobProgressEvent>;

  function planResponse(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath) => ({
        inputPath,
        outputPath: `/tmp/downloads/${inputPath.split('=').pop()}.mp4`,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
      effectiveOptions: request.options,
      warnings: [],
    };
  }

  function runningStatus(jobId: string): JobStatusResponse {
    return {
      success: true,
      message: 'running',
      found: true,
      result: {
        jobId,
        success: false,
        message: 'running',
        toolId: 'tool.web.download',
        status: 'running',
        progress: {
          current: 0,
          total: 1,
          stage: 'web.download',
          message: 'Downloading…',
        },
        items: [],
        startedAt: 1,
      },
    };
  }

  function lastPlanRequest(): JobRequest {
    return wailsSpy.planJob.calls.mostRecent().args[0] as JobRequest;
  }

  function selectOptions(index: number): string[] {
    const select = fixture.nativeElement.querySelectorAll('.row-quality')[
      index
    ] as HTMLSelectElement | undefined;
    if (!select) {
      return [];
    }
    return Array.from(select.options).map((option) => option.text.trim());
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
      'openDirectoryDialog',
      'probeDownloadLink',
      'listTools',
      'validateJob',
    ]);

    jobProgressSubject = new Subject<JobProgressEvent>();
    (wailsSpy as unknown as { jobProgress$: unknown }).jobProgress$ =
      jobProgressSubject.asObservable();

    wailsSpy.planJob.and.callFake((request: JobRequest) =>
      Promise.resolve(planResponse(request)),
    );
    wailsSpy.inspectFiles.and.resolveTo({
      success: true,
      message: 'ok',
      files: [],
    });
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-download',
      status: 'queued',
    });
    wailsSpy.getJobStatus.and.callFake((jobId: string) =>
      Promise.resolve(runningStatus(jobId)),
    );
    wailsSpy.cancelJob.and.resolveTo({
      success: true,
      message: 'cancelled',
      jobId: 'job-download',
    });
    wailsSpy.openPath.and.resolveTo({ success: true, message: 'opened' });
    wailsSpy.revealInFolder.and.resolveTo({
      success: true,
      message: 'revealed',
    });
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/downloads');
    wailsSpy.probeDownloadLink.and.callFake((url: string) => {
      if (url === LINK_A) {
        return Promise.resolve(
          probeVideo(LINK_A, 'Alpha clip', 'alpha', [1080, 720]),
        );
      }
      if (url === LINK_B) {
        return Promise.resolve(probeVideo(LINK_B, 'Beta clip', 'beta', [720]));
      }
      return Promise.resolve(probeVideo(url, url, 'entry', [720]));
    });
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
      imports: [WebDownload],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(WebDownload);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('starts empty and cannot run', () => {
    expect(component.rows()).toEqual([]);
    expect(component.canRun()).toBeFalse();
    expect(component.canPlan()).toBeFalse();
    expect(component.primaryLabel()).toBe('Download');
    expect(component.actionHelper()).toBe('Add at least one link to download.');
  });

  it('probes two links and sends the exact policy request once', fakeAsync(() => {
    component.outputDir.set('/tmp/downloads');
    component.draft.set(`${LINK_A}\n${LINK_B}`);
    component.addLinks();
    flushMicrotasks();
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.rows().length).toBe(2);
    expect(component.rows()[0].status).toBe('ready');
    expect(component.rows()[0].title).toBe('Alpha clip');
    expect(wailsSpy.probeDownloadLink).toHaveBeenCalledTimes(2);

    expect(selectOptions(0)).toEqual([
      'Best available',
      '1080p · MP4 · 30 fps · 320 MB',
      '720p · MP4 · 30 fps · 150 MB',
    ]);
    expect(selectOptions(1)).toEqual([
      'Best available',
      '720p · MP4 · 30 fps · 150 MB',
    ]);

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    const request = lastPlanRequest();
    expect(request.toolId).toBe('tool.web.download');
    expect(request.mode).toBe('batch');
    expect(request.inputPaths).toEqual([LINK_A, LINK_B]);
    expect(request.outputDir).toBe('/tmp/downloads');
    expect(request.options).toEqual({
      kind: 'video',
      maxHeight: 0,
      audioBitrateKbps: 0,
      items: [
        { url: LINK_A, title: 'Alpha clip', id: 'alpha', ext: 'mp4' },
        { url: LINK_B, title: 'Beta clip', id: 'beta', ext: 'mp4' },
      ],
    });

    expect(component.canRun()).toBeTrue();
    void component.run();
    flushMicrotasks();

    expect(wailsSpy.planJob).toHaveBeenCalledTimes(1);
    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
  }));

  it('keeps a failed probe row, names it and blocks the run', fakeAsync(() => {
    component.outputDir.set('/tmp/downloads');
    wailsSpy.probeDownloadLink.and.callFake((url: string) =>
      Promise.resolve(
        probeFailure(
          url,
          'yt-dlp does not support this site or link',
        ),
      ),
    );

    component.draft.set(LINK_A);
    component.addLinks();
    flushMicrotasks();
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.rows().length).toBe(1);
    expect(component.rows()[0].status).toBe('failed');
    expect(component.rows()[0].message).toBe(
      'yt-dlp does not support this site or link',
    );
    expect(component.statusNotice()?.tone).toBe('danger');
    expect(component.statusNotice()?.text).toContain(LINK_A);
    expect(component.statusNotice()?.text).toContain('could not be read');
    expect(component.canRun()).toBeFalse();
    expect(component.actionHelper()).toBe(
      'Retry or remove the links that could not be read.',
    );

    void component.run();
    flushMicrotasks();
    expect(wailsSpy.runJob).not.toHaveBeenCalled();

    // A retry re-probes and unblocks the row.
    wailsSpy.probeDownloadLink.and.resolveTo(
      probeVideo(LINK_A, 'Alpha clip', 'alpha', [1080]),
    );
    component.retry(LINK_A);
    flushMicrotasks();
    tick(200);
    flushMicrotasks();

    expect(component.rows()[0].status).toBe('ready');
    expect(component.canRun()).toBeTrue();
  }));

  it('expands a playlist in place into its links', fakeAsync(() => {
    component.outputDir.set('/tmp/downloads');
    wailsSpy.probeDownloadLink.and.callFake((url: string) => {
      if (url === PLAYLIST) {
        return Promise.resolve({
          ...probeVideo(PLAYLIST, 'Fixture playlist', 'PL1', []),
          isPlaylist: true,
          video: [],
          audio: [],
          entries: [
            { url: LINK_A, title: 'Alpha clip', durationSeconds: 90 },
            { url: LINK_B, title: 'Beta clip', durationSeconds: 45 },
          ],
        });
      }
      if (url === LINK_A) {
        return Promise.resolve(
          probeVideo(LINK_A, 'Alpha clip', 'alpha', [1080]),
        );
      }
      return Promise.resolve(probeVideo(LINK_B, 'Beta clip', 'beta', [720]));
    });

    component.draft.set(PLAYLIST);
    component.addLinks();
    flushMicrotasks();
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.rows().map((row) => row.url)).toEqual([LINK_A, LINK_B]);
    expect(component.rows().every((row) => row.fromPlaylist)).toBeTrue();
    expect(component.rows().every((row) => row.status === 'ready')).toBeTrue();
    expect(component.statusNotice()?.text).toBe(
      'Playlist expanded into 2 links.',
    );
    expect(component.selectedPaths()).toEqual([LINK_A, LINK_B]);
  }));

  it('switches to audio and sends bitrate targets with mp3 items', fakeAsync(() => {
    component.outputDir.set('/tmp/downloads');
    component.draft.set(LINK_A);
    component.addLinks();
    flushMicrotasks();
    tick(200);
    flushMicrotasks();

    component.setKind('audio');
    tick(200);
    flushMicrotasks();
    fixture.detectChanges();

    const request = lastPlanRequest();
    expect(request.options).toEqual({
      kind: 'audio',
      maxHeight: 0,
      audioBitrateKbps: 0,
      items: [{ url: LINK_A, title: 'Alpha clip', id: 'alpha', ext: 'mp3' }],
    });
    expect(selectOptions(0)).toEqual([
      'Best (VBR)',
      '320 kbps',
      '256 kbps',
      '192 kbps',
      '128 kbps',
    ]);
    expect(selectOptions(0).some((label) => label.includes('p ·'))).toBeFalse();
  }));

  it('keeps one policy per run and applies a row quality to every link', fakeAsync(() => {
    component.outputDir.set('/tmp/downloads');
    wailsSpy.probeDownloadLink.and.callFake((url: string) =>
      Promise.resolve(
        probeVideo(
          url,
          url === LINK_A ? 'Alpha clip' : 'Beta clip',
          url === LINK_A ? 'alpha' : 'beta',
          [1080, 720],
        ),
      ),
    );

    component.draft.set(`${LINK_A}\n${LINK_B}`);
    component.addLinks();
    flushMicrotasks();
    tick(200);
    flushMicrotasks();

    component.onQualityChange(
      { target: { value: '1080' } } as unknown as Event,
      LINK_A,
    );
    expect(component.qualityConflict()).toBeTrue();
    expect(component.canRun()).toBeFalse();

    component.applyQualityToAll(LINK_A);
    tick(200);
    flushMicrotasks();

    expect(component.qualityConflict()).toBeFalse();
    expect(component.qualityFor(LINK_B)).toBe(1080);
    expect(lastPlanRequest().options['maxHeight']).toBe(1080);
    expect(component.canRun()).toBeTrue();
  }));

  it('removes a row and keeps the queue in sync', fakeAsync(() => {    component.outputDir.set('/tmp/downloads');
    component.draft.set(`${LINK_A}\n${LINK_B}`);
    component.addLinks();
    flushMicrotasks();
    tick(200);
    flushMicrotasks();

    component.removeRow(LINK_A);
    flushMicrotasks();
    tick(200);
    flushMicrotasks();

    expect(component.rows().map((row) => row.url)).toEqual([LINK_B]);
    expect(component.selectedPaths()).toEqual([LINK_B]);
    expect(lastPlanRequest().inputPaths).toEqual([LINK_B]);
  }));

  it('ignores a duplicate link with a notice', fakeAsync(() => {
    component.draft.set(`${LINK_A}\n${LINK_A}`);
    component.addLinks();
    flushMicrotasks();

    expect(component.rows().length).toBe(1);
    expect(component.statusNotice()?.text).toBe(
      '1 duplicate link ignored.',
    );
    expect(wailsSpy.probeDownloadLink).toHaveBeenCalledTimes(1);
  }));
});
