import { TestBed } from '@angular/core/testing';
import { Subject } from 'rxjs';

import { Stage } from '../../../bindings/fileforge-desktop/internal/update/models';
import {
  UPDATE_CHECK_INTERVAL_MS,
  UPDATE_LAST_CHECK_STORAGE_KEY,
  UpdateStateService,
  isUpdateCheckDue,
  updateUnavailableReason,
} from './update-state.service';
import { Wails } from './wails';
import type {
  AppInfoResponse,
  UpdateProgressEvent,
  UpdateRelease,
  UpdateStatus,
} from './wails';

const APP_INFO: AppInfoResponse = {
  success: true,
  message: '',
  version: '1.2.0',
  commit: 'a1b2c3d',
  builtAt: '2026-11-26T12:00:00Z',
  release: true,
  updateKeyConfigured: true,
};

const RELEASE: UpdateRelease = {
  version: '1.3.0',
  tag: 'v1.3.0',
  asset: {
    name: 'FileForge-1.3.0-x86_64.AppImage',
    url: 'https://example.test/FileForge.AppImage',
    size: 1024,
    kind: 'appimage',
  },
  verification: {},
};

function updateStatus(overrides: Partial<UpdateStatus> = {}): UpdateStatus {
  return {
    success: true,
    message: '',
    state: 'idle',
    currentVersion: '1.2.0',
    updatesEnabled: true,
    updateAvailable: false,
    consentGranted: false,
    ...overrides,
  };
}

class WailsStub {
  readonly updateState$ = new Subject<UpdateStatus>();
  readonly updateProgress$ = new Subject<UpdateProgressEvent>();

  isRuntimeAvailable = jasmine
    .createSpy('isRuntimeAvailable')
    .and.returnValue(true);
  getAppInfo = jasmine.createSpy('getAppInfo').and.resolveTo(APP_INFO);
  getUpdateStatus = jasmine
    .createSpy('getUpdateStatus')
    .and.resolveTo(updateStatus());
  checkForUpdates = jasmine
    .createSpy('checkForUpdates')
    .and.resolveTo(updateStatus({ state: 'available', updateAvailable: true }));
  downloadUpdate = jasmine
    .createSpy('downloadUpdate')
    .and.resolveTo(updateStatus({ state: 'ready' }));
  grantInstallConsent = jasmine
    .createSpy('grantInstallConsent')
    .and.resolveTo(updateStatus({ state: 'ready', consentGranted: true }));
  installUpdate = jasmine
    .createSpy('installUpdate')
    .and.resolveTo(updateStatus({ state: 'installed' }));
}

describe('UpdateStateService', () => {
  let wails: WailsStub;
  let service: UpdateStateService;

  function lastCheckAt(): number | null {
    const raw = localStorage.getItem(UPDATE_LAST_CHECK_STORAGE_KEY);
    if (raw === null) {
      return null;
    }
    return Number(raw);
  }

  beforeEach(() => {
    try {
      localStorage.removeItem(UPDATE_LAST_CHECK_STORAGE_KEY);
    } catch {
      // Storage is available in Karma; keep the spec resilient.
    }

    wails = new WailsStub();
    TestBed.configureTestingModule({
      providers: [{ provide: Wails, useValue: wails }],
    });
    service = TestBed.inject(UpdateStateService);
  });

  it('decides the 24 hour window from the stored timestamp', () => {
    const now = Date.now();

    expect(isUpdateCheckDue(null, now)).toBeTrue();
    expect(isUpdateCheckDue(now, now)).toBeFalse();
    expect(
      isUpdateCheckDue(now - UPDATE_CHECK_INTERVAL_MS - 1000, now),
    ).toBeTrue();
    expect(isUpdateCheckDue(Number.NaN, now)).toBeTrue();
    expect(
      isUpdateCheckDue(now + UPDATE_CHECK_INTERVAL_MS * 2, now),
    ).toBeTrue();
  });

  it('classifies every backend gate reason', () => {
    expect(
      updateUnavailableReason(
        'This is a development build. Updates are only available in release builds.',
      ),
    ).toBe('dev');
    expect(
      updateUnavailableReason(
        'This build has no update signing key pinned, so updates are disabled.',
      ),
    ).toBe('noKey');
    expect(
      updateUnavailableReason(
        'This installation is managed by your package manager. Update FileForge with it.',
      ),
    ).toBe('packageManager');
    expect(
      updateUnavailableReason('Updates are not supported on this platform.'),
    ).toBe('platform');
    expect(updateUnavailableReason('')).toBe('generic');
    expect(
      updateUnavailableReason('', { ...APP_INFO, release: false }),
    ).toBe('dev');
    expect(
      updateUnavailableReason('', { ...APP_INFO, updateKeyConfigured: false }),
    ).toBe('noKey');
  });

  it('runs the launch check once per session and stores the timestamp', async () => {
    await service.ensureLaunchCheck();
    await service.ensureLaunchCheck();

    expect(wails.isRuntimeAvailable).toHaveBeenCalledTimes(1);
    expect(wails.getAppInfo).toHaveBeenCalledTimes(1);
    expect(wails.checkForUpdates).toHaveBeenCalledTimes(1);
    expect(service.launchCheckRequested()).toBeTrue();

    const stored = lastCheckAt();
    expect(stored).not.toBeNull();
    expect(stored! > 0).toBeTrue();
  });

  it('skips the launch check inside the 24 hour window', async () => {
    localStorage.setItem(UPDATE_LAST_CHECK_STORAGE_KEY, String(Date.now()));

    await service.ensureLaunchCheck();

    expect(wails.checkForUpdates).not.toHaveBeenCalled();
    expect(service.launchCheckRequested()).toBeTrue();
  });

  it('never checks from a development build', async () => {
    wails.getAppInfo.and.resolveTo({ ...APP_INFO, release: false });

    await service.ensureLaunchCheck();

    expect(wails.checkForUpdates).not.toHaveBeenCalled();
    expect(lastCheckAt()).toBeNull();
  });

  it('never checks when the runtime is unavailable', async () => {
    wails.isRuntimeAvailable.and.returnValue(false);

    await service.ensureLaunchCheck();

    expect(wails.getAppInfo).not.toHaveBeenCalled();
    expect(wails.checkForUpdates).not.toHaveBeenCalled();
  });

  it('stays silent when the launch check fails', async () => {
    wails.checkForUpdates.and.rejectWith(new Error('offline'));

    await expectAsync(service.ensureLaunchCheck()).toBeResolved();

    expect(service.status()).toBeNull();
    expect(lastCheckAt()).not.toBeNull();
  });

  it('stays silent when the launch check reports a failure', async () => {
    wails.checkForUpdates.and.resolveTo(
      updateStatus({
        success: false,
        state: 'error',
        error: 'The release feed returned 503.',
      }),
    );

    await service.ensureLaunchCheck();

    expect(service.status()).toBeNull();
  });

  it('always reports a manual check, including its failure', async () => {
    wails.checkForUpdates.and.resolveTo(
      updateStatus({
        success: false,
        state: 'error',
        error: 'The release feed returned 503.',
        stage: Stage.StageCheck,
      }),
    );

    await service.checkForUpdates({ manual: true });

    expect(service.status()?.state).toBe('error');
    expect(service.status()?.error).toContain('503');
    expect(lastCheckAt()).not.toBeNull();
  });

  it('applies status and progress events from the backend', () => {
    wails.updateState$.next(
      updateStatus({ state: 'available', updateAvailable: true, release: RELEASE }),
    );
    expect(service.status()?.state).toBe('available');

    wails.updateProgress$.next({
      stage: Stage.StageDownload,
      written: 5,
      total: 10,
      percent: 0,
    });

    expect(service.progress()?.percent).toBe(50);
    expect(service.progress()?.written).toBe(5);
    expect(service.progress()?.total).toBe(10);
  });

  it('ignores progress that does not belong to a download', () => {
    wails.updateProgress$.next({
      stage: Stage.StageCheck,
      written: 5,
      total: 10,
      percent: 50,
    });

    expect(service.progress()).toBeNull();
  });

  it('forwards download, consent and install calls', async () => {
    await service.download();
    await service.grantConsent();
    await service.install();

    expect(wails.downloadUpdate).toHaveBeenCalledTimes(1);
    expect(wails.grantInstallConsent).toHaveBeenCalledTimes(1);
    expect(wails.installUpdate).toHaveBeenCalledTimes(1);
    expect(service.status()?.state).toBe('installed');
  });

  it('keeps the status unknown when the cached read fails', async () => {
    wails.getUpdateStatus.and.resolveTo(
      updateStatus({ success: false, state: 'error' }),
    );

    const status = await service.refresh();

    expect(status).toBeNull();
    expect(service.status()).toBeNull();
  });

  it('never downloads or installs on its own', async () => {
    await service.ensureLaunchCheck();
    await service.refresh();

    expect(wails.downloadUpdate).not.toHaveBeenCalled();
    expect(wails.grantInstallConsent).not.toHaveBeenCalled();
    expect(wails.installUpdate).not.toHaveBeenCalled();
  });
});
