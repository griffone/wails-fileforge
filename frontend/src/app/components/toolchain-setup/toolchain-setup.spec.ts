import { ComponentFixture, TestBed, fakeAsync, flushMicrotasks } from '@angular/core/testing';
import { computed, signal } from '@angular/core';
import { provideRouter } from '@angular/router';

import { ToolchainSetup } from './toolchain-setup';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { ToolchainStateService } from '../../services/toolchain-state.service';
import {
  ToolchainInstallEvent,
  ToolchainStatus,
} from '../../services/wails';

class ToolchainStateStub {
  readonly status = signal<ToolchainStatus | null>(null);
  readonly event = signal<ToolchainInstallEvent | null>(null);
  readonly loading = signal(false);
  readonly isBusy = computed(() => this.status()?.installing === true);

  refresh = jasmine.createSpy('refresh').and.resolveTo(undefined);
  ensureStarted = jasmine.createSpy('ensureStarted').and.resolveTo(undefined);
  install = jasmine.createSpy('install').and.resolveTo(null);
  cancel = jasmine.createSpy('cancel').and.resolveTo(null);
}

function toolchainStatus(
  overrides: Partial<ToolchainStatus> = {},
): ToolchainStatus {
  return {
    success: true,
    message: 'Local tools are not installed.',
    ready: false,
    installing: false,
    percent: 0,
    storageDir: '/tmp/fileforge/toolchain',
    components: [
      {
        id: 'whisper-cli',
        label: 'Whisper runtime',
        ready: false,
        required: true,
      },
    ],
    model: 'medium',
    ocrLanguages: ['eng', 'spa'],
    ...overrides,
  };
}

describe('ToolchainSetup', () => {
  let fixture: ComponentFixture<ToolchainSetup>;
  let stub: ToolchainStateStub;

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

  beforeEach(async () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ language: 'en' }),
    );
    stub = new ToolchainStateStub();

    await TestBed.configureTestingModule({
      imports: [ToolchainSetup],
      providers: [
        provideRouter([]),
        { provide: ToolchainStateService, useValue: stub },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(ToolchainSetup);
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    fixture.destroy();
  });

  it('renders the overlay with progress and phase while installing', fakeAsync(() => {
    stub.status.set(
      toolchainStatus({
        installing: true,
        percent: 35,
        phase: 'model',
        detail: 'Downloading the model…',
      }),
    );

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(element().querySelector('.toolchain-backdrop')).not.toBeNull();
    expect(text).toContain('FIRST RUN');
    expect(text).toContain('Installing local tools');
    expect(text).toContain('Downloading the model');
    expect(text).toContain('35%');
    expect(text).toContain(
      'Whisper · Tesseract · FFmpeg · Poppler · Ghostscript',
    );

    const fill = element().querySelector<HTMLElement>('.progress-fill');
    expect(fill?.style.width).toBe('35%');
    expect(fill?.classList.contains('is-indeterminate')).toBeFalse();
    expect(stub.ensureStarted).toHaveBeenCalled();
  }));

  it('hides the overlay when the toolchain is ready', fakeAsync(() => {
    stub.status.set(
      toolchainStatus({
        ready: true,
        percent: 100,
        phase: 'ready',
        components: [
          {
            id: 'whisper-cli',
            label: 'Whisper runtime',
            ready: true,
            required: true,
            path: '/tmp/fileforge/toolchain/whisper/whisper-cli',
          },
        ],
      }),
    );

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().querySelector('.toolchain-backdrop')).toBeNull();
    expect(stub.ensureStarted).toHaveBeenCalled();
  }));

  it('shows Retry and Continue on error; Retry starts and Continue dismisses', fakeAsync(() => {
    stub.status.set(
      toolchainStatus({
        success: false,
        error: 'The Whisper model download failed.',
        message: 'The Whisper model download failed.',
        phase: 'model',
      }),
    );

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('The Whisper model download failed.');
    expect(button('.toolchain-retry')).toBeTruthy();

    button('.toolchain-retry').click();
    expect(stub.install).toHaveBeenCalledWith({
      model: 'medium',
      ocrLanguages: ['eng', 'spa'],
    });

    button('.toolchain-continue').click();
    fixture.detectChanges();

    expect(element().querySelector('.toolchain-backdrop')).toBeNull();
  }));

  it('updates percent, phase and message from installer events', fakeAsync(() => {
    stub.status.set(
      toolchainStatus({
        installing: true,
        percent: 5,
        phase: 'micromamba',
      }),
    );
    stub.event.set({
      phase: 'model',
      percent: 63,
      message: 'Downloading the Whisper model…',
    });

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('63%');
    expect(text).toContain('Downloading the model');
    expect(text).toContain('Downloading the Whisper model…');

    const fill = element().querySelector<HTMLElement>('.progress-fill');
    expect(fill?.style.width).toBe('63%');
  }));

  it('renders the overlay copy in Spanish', fakeAsync(() => {
    TestBed.inject(I18nService).setLocale('es');
    stub.status.set(
      toolchainStatus({
        installing: true,
        percent: 35,
        phase: 'model',
        detail: 'Downloading the model…',
      }),
    );

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('PRIMERA EJECUCIÓN');
    expect(text).toContain('Instalando herramientas locales');
    expect(text).toContain('Descargando el modelo');
    expect(text).toContain('35%');
    expect(text).toContain('Cancelar');
    expect(text).toContain(
      'FileForge está preparando Whisper, Tesseract, FFmpeg y las herramientas locales de OCR y transcripción.',
    );
  }));

  it('stays hidden when IPC is unavailable (success: false preview)', fakeAsync(() => {
    stub.status.set(
      toolchainStatus({
        success: false,
        message: 'Toolchain IPC unavailable.',
        components: [],
      }),
    );

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().querySelector('.toolchain-backdrop')).toBeNull();
  }));
});
