import { ComponentFixture, TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';

import { SettingsUpdates } from './settings-updates';
import { Stage } from '../../../../../bindings/fileforge-desktop/internal/update/models';
import { I18nService } from '../../../services/i18n.service';
import { UpdateStateService } from '../../../services/update-state.service';
import type {
  AppInfoResponse,
  UpdateProgressEvent,
  UpdateRelease,
  UpdateStatus,
} from '../../../services/wails';

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
  name: 'FileForge 1.3.0',
  notes: 'Faster PDF merge.\nFixes the OCR language picker.',
  asset: {
    name: 'FileForge-1.3.0-x86_64.AppImage',
    url: 'https://example.test/FileForge.AppImage',
    size: 24 * 1024 * 1024,
    kind: 'appimage',
  },
  verification: { digest: 'ZGlnZXN0', signature: 'c2lnbmF0dXJl' },
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

function availableStatus(overrides: Partial<UpdateStatus> = {}): UpdateStatus {
  return updateStatus({
    state: 'available',
    updateAvailable: true,
    method: 'appimage',
    release: RELEASE,
    ...overrides,
  });
}

class UpdateStateStub {
  readonly status = signal<UpdateStatus | null>(null);
  readonly progress = signal<UpdateProgressEvent | null>(null);
  readonly busy = signal(false);

  refresh = jasmine.createSpy('refresh').and.resolveTo(null);
  checkForUpdates = jasmine
    .createSpy('checkForUpdates')
    .and.resolveTo(null);
  download = jasmine.createSpy('download').and.resolveTo(null);
  grantConsent = jasmine.createSpy('grantConsent').and.resolveTo(null);
  install = jasmine.createSpy('install').and.resolveTo(null);
}

describe('SettingsUpdates', () => {
  let fixture: ComponentFixture<SettingsUpdates>;
  let component: SettingsUpdates;
  let updates: UpdateStateStub;
  let i18n: I18nService;

  function element(): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function button(selector: string): HTMLButtonElement {
    const found = element().querySelector<HTMLButtonElement>(selector);
    if (!found) {
      throw new Error(`Missing button: ${selector}`);
    }
    return found;
  }

  function text(): string {
    return element().textContent ?? '';
  }

  beforeEach(async () => {
    try {
      localStorage.clear();
    } catch {
      // Storage is available in Karma; keep the spec resilient.
    }

    updates = new UpdateStateStub();

    await TestBed.configureTestingModule({
      imports: [SettingsUpdates],
      providers: [{ provide: UpdateStateService, useValue: updates }],
    }).compileComponents();

    i18n = TestBed.inject(I18nService);
    fixture = TestBed.createComponent(SettingsUpdates);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('appInfo', APP_INFO);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    i18n.setLocale('en');
  });

  it('reads the cached status on load', () => {
    expect(updates.refresh).toHaveBeenCalledTimes(1);
  });

  it('renders each unavailable reason and hides the download action', () => {
    const cases: Array<[string, string]> = [
      [
        'This is a development build. Updates are only available in release builds.',
        'Updates are off in this development build',
      ],
      [
        'This build has no update signing key pinned, so updates are disabled.',
        'no update signing key pinned',
      ],
      [
        'This file was installed by your package manager. Update FileForge with it.',
        'Update it from there',
      ],
      [
        'Updates are not supported on this platform.',
        'not supported on this platform',
      ],
    ];

    for (const [reason, expected] of cases) {
      updates.status.set(
        updateStatus({ state: 'unavailable', updatesEnabled: false, reason }),
      );
      fixture.detectChanges();

      expect(text()).toContain(expected);
      expect(element().querySelector('.settings-updates-download')).toBeNull();
      expect(element().querySelector('.settings-updates-install')).toBeNull();
      expect(element().querySelector('.settings-updates-check')).toBeNull();
    }
  });

  it('renders the available version, release notes and asset size', () => {
    updates.status.set(availableStatus());
    fixture.detectChanges();

    expect(text()).toContain('Version 1.3.0 is available');
    expect(text()).toContain('Faster PDF merge.');
    expect(text()).toContain('Fixes the OCR language picker.');
    expect(text()).toContain('Download size: 24 MB');
    expect(element().querySelector('.settings-updates-notes')).not.toBeNull();
  });

  it('truncates long release notes and expands them on demand', () => {
    const notes = 'n'.repeat(600);
    updates.status.set(
      availableStatus({
        release: { ...RELEASE, notes },
      }),
    );
    fixture.detectChanges();

    const notesElement = element().querySelector('.settings-updates-notes');
    expect(notesElement?.textContent?.trim().endsWith('…')).toBeTrue();
    expect(notesElement?.textContent?.length).toBeLessThan(600);

    button('.settings-updates-notes-toggle').click();
    fixture.detectChanges();

    expect(notesElement?.textContent?.trim()).toBe(notes);
    expect(button('.settings-updates-notes-toggle').textContent).toContain(
      'Show less',
    );
  });

  it('starts the download only from the button', () => {
    updates.status.set(availableStatus());
    fixture.detectChanges();

    expect(updates.download).not.toHaveBeenCalled();

    button('.settings-updates-download').click();

    expect(updates.download).toHaveBeenCalledTimes(1);
  });

  it('renders download progress from the event stream without a cancel button', () => {
    updates.status.set(
      availableStatus({ state: 'downloading', updateAvailable: true }),
    );
    updates.progress.set({
      stage: Stage.StageDownload,
      written: 12 * 1024 * 1024,
      total: 24 * 1024 * 1024,
      percent: 50,
    });
    fixture.detectChanges();

    const bar = element().querySelector('.settings-updates-progress');
    expect(bar).not.toBeNull();
    expect(bar?.getAttribute('aria-valuenow')).toBe('50');
    expect(
      bar?.querySelector('.progress-fill')?.getAttribute('style'),
    ).toContain('50%');
    expect(text()).toContain('50%');
    expect(text()).toContain('12 MB of 24 MB');
    expect(
      element().querySelector('.settings-cancel-update'),
    ).toBeNull();
    expect(text()).toContain('cannot be cancelled from FileForge');
  });

  it('requires the consent step before granting or installing', () => {
    updates.status.set(updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }));
    updates.grantConsent.and.resolveTo(
      updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }),
    );
    updates.install.and.resolveTo(
      updateStatus({ state: 'installed', method: 'appimage', release: RELEASE }),
    );
    fixture.detectChanges();

    // Opening the confirmation alone calls nothing.
    button('.settings-updates-install').click();
    fixture.detectChanges();

    expect(text()).toContain('Install FileForge 1.3.0?');
    expect(text()).toContain('replace its AppImage file');
    expect(text()).toContain('restart');
    expect(updates.grantConsent).not.toHaveBeenCalled();
    expect(updates.install).not.toHaveBeenCalled();

    // The confirm button stays disabled until the consequence is accepted.
    const confirm = button('.settings-updates-consent-confirm');
    expect(confirm.disabled).toBeTrue();
    confirm.click();
    fixture.detectChanges();
    expect(updates.grantConsent).not.toHaveBeenCalled();

    const ack = element().querySelector<HTMLInputElement>(
      '.settings-updates-ack-input',
    );
    ack!.checked = true;
    ack!.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(button('.settings-updates-consent-confirm').disabled).toBeFalse();
  });

  it('grants consent only from the accepted confirmation, then installs', async () => {
    const order: string[] = [];
    updates.status.set(updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }));
    updates.grantConsent.and.callFake(() => {
      order.push('consent');
      return Promise.resolve(
        updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }),
      );
    });
    updates.install.and.callFake(() => {
      order.push('install');
      return Promise.resolve(
        updateStatus({
          state: 'installed',
          method: 'appimage',
          release: RELEASE,
        }),
      );
    });
    fixture.detectChanges();

    button('.settings-updates-install').click();
    fixture.detectChanges();

    const ack = element().querySelector<HTMLInputElement>(
      '.settings-updates-ack-input',
    );
    ack!.checked = true;
    ack!.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    button('.settings-updates-consent-confirm').click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(updates.grantConsent).toHaveBeenCalledTimes(1);
    expect(updates.install).toHaveBeenCalledTimes(1);
    expect(order).toEqual(['consent', 'install']);
  });

  it('does not install when the backend refuses consent', async () => {
    updates.status.set(updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }));
    updates.grantConsent.and.resolveTo(
      updateStatus({ success: false, state: 'error', error: 'no verified update' }),
    );
    fixture.detectChanges();

    button('.settings-updates-install').click();
    fixture.detectChanges();

    const ack = element().querySelector<HTMLInputElement>(
      '.settings-updates-ack-input',
    );
    ack!.checked = true;
    ack!.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    button('.settings-updates-consent-confirm').click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(updates.grantConsent).toHaveBeenCalledTimes(1);
    expect(updates.install).not.toHaveBeenCalled();
  });

  it('names the installer consequence on Windows', () => {
    updates.status.set(
      updateStatus({
        state: 'ready',
        method: 'windows-installer',
        release: RELEASE,
      }),
    );
    fixture.detectChanges();

    button('.settings-updates-install').click();
    fixture.detectChanges();

    expect(text()).toContain('open the update installer');
    expect(text()).toContain('FileForge will close');
    expect(button('.settings-updates-consent-confirm').textContent).toContain(
      'open the installer',
    );
  });

  it('cancelling the confirmation calls nothing', () => {
    updates.status.set(updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }));
    fixture.detectChanges();

    button('.settings-updates-install').click();
    fixture.detectChanges();
    button('.settings-updates-consent-cancel').click();
    fixture.detectChanges();

    expect(updates.grantConsent).not.toHaveBeenCalled();
    expect(updates.install).not.toHaveBeenCalled();
    expect(element().querySelector('.settings-updates-consent')).toBeNull();
    expect(element().querySelector('.settings-updates-install')).not.toBeNull();
  });

  it('renders the up-to-date state and checks again from the button', () => {
    updates.status.set(updateStatus({ state: 'up-to-date' }));
    fixture.detectChanges();

    expect(text()).toContain('FileForge 1.2.0 is up to date.');

    button('.settings-updates-check-again').click();

    expect(updates.checkForUpdates).toHaveBeenCalledWith({ manual: true });
  });

  it('shows the backend error and retries the failing step', () => {
    updates.status.set(
      availableStatus({
        success: false,
        state: 'error',
        updateAvailable: true,
        error: 'The artifact signature is invalid.',
        stage: Stage.StageVerify,
      }),
    );
    fixture.detectChanges();

    expect(text()).toContain('The artifact signature is invalid.');
    expect(element().querySelector('.settings-updates-error')).not.toBeNull();

    button('.settings-updates-retry').click();

    expect(updates.download).toHaveBeenCalledTimes(1);
    expect(updates.checkForUpdates).not.toHaveBeenCalled();
  });

  it('falls back to the translated error when the backend sends none', () => {
    updates.status.set(updateStatus({ success: false, state: 'error' }));
    fixture.detectChanges();

    expect(text()).toContain('The update operation failed.');
    expect(element().querySelector('.settings-updates-retry')).not.toBeNull();
  });

  it('retries the check when the failure was not a download', () => {
    updates.status.set(
      updateStatus({
        success: false,
        state: 'error',
        error: 'The release feed returned 503.',
        stage: Stage.StageCheck,
      }),
    );
    fixture.detectChanges();

    button('.settings-updates-retry').click();

    expect(updates.checkForUpdates).toHaveBeenCalledWith({ manual: true });
    expect(updates.download).not.toHaveBeenCalled();
  });

  it('renders the update states in Spanish', () => {
    i18n.setLocale('es');

    updates.status.set(
      updateStatus({
        state: 'unavailable',
        updatesEnabled: false,
        reason:
          'This is a development build. Updates are only available in release builds.',
      }),
    );
    fixture.detectChanges();

    expect(text()).toContain('compilación de desarrollo');
    expect(text()).toContain('Actualizaciones');

    updates.status.set(availableStatus());
    fixture.detectChanges();
    expect(text()).toContain('La versión 1.3.0 está disponible');
    expect(text()).toContain('Tamaño de la descarga: 24 MB');

    updates.status.set(updateStatus({ state: 'up-to-date' }));
    fixture.detectChanges();
    expect(text()).toContain('FileForge 1.2.0 está actualizado.');

    updates.status.set(updateStatus({ state: 'ready', method: 'appimage', release: RELEASE }));
    fixture.detectChanges();
    button('.settings-updates-install').click();
    fixture.detectChanges();
    expect(text()).toContain('¿Instalar FileForge 1.3.0?');
    expect(text()).toContain('se reiniciará');
  });
});
