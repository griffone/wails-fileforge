import { TestBed } from '@angular/core/testing';

import { Wails } from './wails';
import { WailsIpc } from './wails-ipc';

describe('Wails', () => {
  let service: Wails;
  let ipc: WailsIpc;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(Wails);
    ipc = TestBed.inject(WailsIpc);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should return safe fallback when listTools fails', async () => {
    spyOn(ipc, 'listTools').and.rejectWith(new Error('ipc unavailable'));

    const result = await service.listTools();

    expect(result.success).toBeFalse();
    expect(result.tools).toEqual([]);
    expect(result.message).toContain('ipc unavailable');
  });

  it('should return canonical coded fallback when runJob fails', async () => {
    spyOn(ipc, 'runJob').and.rejectWith(new Error('timeout'));

    const result = await service.runJob({
      toolId: 'tool.pdf.merge',
      mode: 'single',
      inputPaths: ['/tmp/input-a.pdf', '/tmp/input-b.pdf'],
      outputDir: '/tmp',
      options: { outputPath: '/tmp/out.pdf' },
    });

    expect(result.success).toBeFalse();
    expect(result.status).toBe('failed');
    expect(result.message).toContain('timeout');
    expect(result.error?.code).toBe('EXEC_IO_TRANSIENT');
    expect(result.error?.detail_code).toBe('IPC_RUN_FAILED');
  });

  it('should return canonical coded fallback when getPdfPreviewSource fails', async () => {
    spyOn(ipc, 'getPdfPreviewSource').and.rejectWith(new Error('ipc down'));

    const result = await service.getPdfPreviewSource('/tmp/in.pdf');

    expect(result.success).toBeFalse();
    expect(result.error?.code).toBe('EXEC_IO_TRANSIENT');
    expect(result.error?.detail_code).toBe('PDF_PREVIEW_READ_FAILED');
    expect(result.error?.message).toContain('ipc down');
  });

  it('should return safe fallbacks for the plan and inspect calls', async () => {
    spyOn(ipc, 'planJob').and.rejectWith(new Error('ipc down'));
    spyOn(ipc, 'inspectFiles').and.rejectWith(new Error('ipc down'));
    spyOn(ipc, 'openPath').and.rejectWith(new Error('ipc down'));
    spyOn(ipc, 'revealInFolder').and.rejectWith(new Error('ipc down'));

    const plan = await service.planJob({
      toolId: 'tool.image.convert',
      mode: 'batch',
      inputPaths: ['/tmp/a.png'],
      outputDir: '',
      options: {},
    });
    expect(plan.success).toBeFalse();
    expect(plan.outputs).toEqual([]);
    expect(plan.error?.detail_code).toBe('IPC_PLAN_FAILED');

    const inspect = await service.inspectFiles(['/tmp/a.png']);
    expect(inspect.success).toBeFalse();
    expect(inspect.files).toEqual([]);

    const opened = await service.openPath('/tmp/a.png');
    expect(opened.success).toBeFalse();
    expect(opened.message).toContain('ipc down');

    const revealed = await service.revealInFolder('/tmp/a.png');
    expect(revealed.success).toBeFalse();
    expect(revealed.message).toContain('ipc down');
  });

  it('registers a media path and falls back safely when IPC fails', async () => {
    const call = spyOn(ipc, 'registerMediaPath').and.resolveTo({
      success: true,
      url: '/media/abc',
      message: 'ok',
    });

    const registered = await service.registerMediaPath('/tmp/a.mp4');
    expect(registered.success).toBeTrue();
    expect(registered.url).toBe('/media/abc');
    expect(call).toHaveBeenCalledWith('/tmp/a.mp4');

    call.and.rejectWith(new Error('ipc down'));
    const failed = await service.registerMediaPath('/tmp/a.mp4');
    expect(failed.success).toBeFalse();
    expect(failed.url).toBe('');
    expect(failed.message).toContain('ipc down');
  });

  it('normalizes a probed link and falls back safely when IPC fails', async () => {
    const call = spyOn(ipc, 'probeDownloadLink').and.resolveTo({
      success: true,
      message: 'ok',
      url: 'https://example.test/watch',
      id: 'abc',
      title: 'Fixture',
      uploader: 'Uploader',
      thumbnailUrl: '',
      durationSeconds: 10,
      isPlaylist: false,
      entries: [],
      video: [],
      audio: [],
    });

    const probed = await service.probeDownloadLink('https://example.test/watch');
    expect(probed.success).toBeTrue();
    expect(probed.id).toBe('abc');
    expect(probed.entries).toEqual([]);
    expect(probed.video).toEqual([]);
    expect(probed.audio).toEqual([]);
    expect(call).toHaveBeenCalledWith('https://example.test/watch');

    call.and.rejectWith(new Error('ipc down'));
    const failed = await service.probeDownloadLink('https://example.test/watch');
    expect(failed.success).toBeFalse();
    expect(failed.title).toBe('');
    expect(failed.message).toContain('ipc down');
  });

  it('returns a data URL thumbnail when the preview succeeds', async () => {
    spyOn(service, 'StartPreview').and.resolveTo({ success: true, jobID: 'preview-1' });
    spyOn(service, 'GetPreviewStatus').and.resolveTo({ status: 'succeeded' });
    spyOn(service, 'GetPreview').and.resolveTo({
      success: true,
      data: 'YWJj',
      contentType: 'image/webp',
    });

    const thumbnail = await service.getThumbnail('/tmp/a.png', 64);

    expect(thumbnail).toBe('data:image/webp;base64,YWJj');
  });

  it('returns null and caches failed thumbnail previews', async () => {
    const start = spyOn(service, 'StartPreview').and.resolveTo({ success: false });

    expect(await service.getThumbnail('/tmp/a.png')).toBeNull();
    expect(await service.getThumbnail('/tmp/a.png')).toBeNull();

    expect(start).toHaveBeenCalledTimes(1);
  });

  it('returns a safe fallback when the update status cannot be read', async () => {
    spyOn(ipc, 'getUpdateStatus').and.rejectWith(new Error('ipc down'));

    const status = await service.getUpdateStatus();

    expect(status.success).toBeFalse();
    expect(status.state).toBe('error');
    expect(status.updatesEnabled).toBeFalse();
    // The panel renders a translated error; no English IPC detail leaks in.
    expect(status.message).toBe('');
  });

  it('forwards every update action to the generated bindings', async () => {
    const check = spyOn(ipc, 'checkForUpdates').and.resolveTo({
      success: true,
      message: '',
      state: 'available',
      currentVersion: '1.2.0',
      updatesEnabled: true,
      updateAvailable: true,
      consentGranted: false,
    });
    const download = spyOn(ipc, 'downloadUpdate').and.resolveTo({
      success: true,
      message: '',
      state: 'downloading',
      currentVersion: '1.2.0',
      updatesEnabled: true,
      updateAvailable: true,
      consentGranted: false,
    });
    const consent = spyOn(ipc, 'grantInstallConsent').and.resolveTo({
      success: true,
      message: '',
      state: 'ready',
      currentVersion: '1.2.0',
      updatesEnabled: true,
      updateAvailable: true,
      consentGranted: true,
    });
    const install = spyOn(ipc, 'installUpdate').and.resolveTo({
      success: true,
      message: '',
      state: 'installing',
      currentVersion: '1.2.0',
      updatesEnabled: true,
      updateAvailable: true,
      consentGranted: false,
    });

    expect((await service.checkForUpdates()).state).toBe('available');
    expect((await service.downloadUpdate()).state).toBe('downloading');
    expect((await service.grantInstallConsent()).consentGranted).toBeTrue();
    expect((await service.installUpdate()).state).toBe('installing');

    expect(check).toHaveBeenCalledTimes(1);
    expect(download).toHaveBeenCalledTimes(1);
    expect(consent).toHaveBeenCalledTimes(1);
    expect(install).toHaveBeenCalledTimes(1);
  });
});
