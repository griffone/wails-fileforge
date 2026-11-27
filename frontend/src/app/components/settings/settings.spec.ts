import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
} from '@angular/core/testing';
import { computed, signal } from '@angular/core';

import { Settings } from './settings';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import { ToolchainStateService } from '../../services/toolchain-state.service';
import {
  AppInfoResponse,
  JobResult,
  LocalCapabilitiesResponse,
  ToolchainComponent,
  ToolchainInstallEvent,
  ToolchainStatus,
  TranscriptionCapabilitiesResponse,
  UpdateStatus,
  Wails,
} from '../../services/wails';

const ONBOARDING_KEY = 'ff-onboarding-dismissed';
const TOOLCHAIN_STORAGE = '/home/user/.local/share/fileforge/toolchain';

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

const TOOLCHAIN_COMPONENTS: ToolchainComponent[] = [
  {
    id: 'micromamba',
    label: 'micromamba',
    ready: true,
    required: false,
    path: `${TOOLCHAIN_STORAGE}/micromamba`,
    version: '2.0.0',
    detail: 'Portable package manager',
  },
  {
    id: 'ffmpeg',
    label: 'ffmpeg',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/env/bin/ffmpeg`,
    version: '7.1',
  },
  {
    id: 'ffprobe',
    label: 'ffprobe',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/env/bin/ffprobe`,
    version: '7.1',
  },
  {
    id: 'tesseract',
    label: 'tesseract',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/env/bin/tesseract`,
    version: '5.5.0',
  },
  {
    id: 'pdftoppm',
    label: 'pdftoppm',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/env/bin/pdftoppm`,
  },
  {
    id: 'pdftotext',
    label: 'pdftotext',
    ready: true,
    required: false,
    path: `${TOOLCHAIN_STORAGE}/env/bin/pdftotext`,
  },
  {
    id: 'gs',
    label: 'Ghostscript',
    ready: true,
    required: false,
    path: `${TOOLCHAIN_STORAGE}/env/bin/gs`,
    version: '10.04.0',
  },
  {
    id: 'whisper-cli',
    label: 'Whisper runtime',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/whisper/whisper-cli`,
  },
  {
    id: 'whisper-model',
    label: 'Whisper model',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/models/ggml-medium.bin`,
    detail: 'Whisper medium model',
  },
  {
    id: 'tesseract-eng',
    label: 'Tesseract eng',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/tessdata/eng.traineddata`,
  },
  {
    id: 'tesseract-spa',
    label: 'Tesseract spa',
    ready: true,
    required: true,
    path: `${TOOLCHAIN_STORAGE}/tessdata/spa.traineddata`,
  },
];

function toolchainStatus(
  overrides: Partial<ToolchainStatus> = {},
): ToolchainStatus {
  return {
    success: true,
    message: 'Local tools are not installed.',
    ready: false,
    installing: false,
    percent: 0,
    phase: 'idle',
    storageDir: TOOLCHAIN_STORAGE,
    components: TOOLCHAIN_COMPONENTS,
    model: 'medium',
    ocrLanguages: ['eng', 'spa'],
    ...overrides,
  };
}

function readyToolchain(): ToolchainStatus {
  return toolchainStatus({
    ready: true,
    percent: 100,
    phase: 'ready',
    message: 'Local tools are ready.',
    detail: 'Local tools are ready.',
  });
}

const CAPABILITIES: LocalCapabilitiesResponse = {
  success: true,
  message: 'Local capabilities checked.',
  checks: [
    {
      id: 'pdftoppm',
      label: 'pdftoppm',
      available: true,
      required: true,
      detail: 'OCR page rasterizer',
    },
    {
      id: 'tesseract',
      label: 'tesseract',
      available: true,
      required: true,
      detail: 'Languages: AFR',
    },
    {
      id: 'gs',
      label: 'gs',
      available: true,
      required: false,
      detail: 'PDF compression for target sizes',
    },
    {
      id: 'pdftotext',
      label: 'pdftotext',
      available: false,
      required: false,
      detail: 'OCR text-layer verification',
      suggestion: 'Install poppler-utils for text verification.',
    },
  ],
  ocrLanguages: ['afr'],
  recommendedOcrLanguage: 'afr',
};

const TRANSCRIPTION: TranscriptionCapabilitiesResponse = {
  success: true,
  message: 'Local transcription is available.',
  available: true,
  whisperPath: '/home/user/.local/share/fileforge/whisper/whisper-cli',
  whisperPresent: true,
  modelPath: '/home/user/.local/share/fileforge/models/ggml-base.bin',
  modelPresent: true,
  modelName: 'ggml-base',
  modelSizeBytes: 142 * 1024 * 1024,
  languages: ['auto', 'en', 'es'],
  defaultLanguage: 'auto',
};

const APP_INFO: AppInfoResponse = {
  success: true,
  message: 'app info',
  version: '1.2.0',
  commit: 'a1b2c3d',
  builtAt: '2026-11-26T12:00:00Z',
  release: true,
  updateKeyConfigured: false,
};

const UPDATE_STATUS: UpdateStatus = {
  success: true,
  message: '',
  state: 'unavailable',
  currentVersion: '1.2.0',
  updatesEnabled: false,
  updateAvailable: false,
  reason:
    'This is a development build. Updates are only available in release builds.',
  consentGranted: false,
};

describe('Settings', () => {
  let fixture: ComponentFixture<Settings>;
  let component: Settings;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let toolchain: ToolchainStateStub;
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

  function storedPreferences(): unknown {
    return JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}');
  }

  function sampleJob(): JobResult {
    return {
      jobId: 'job-settings-1',
      success: true,
      message: 'done',
      toolId: 'tool.image.convert',
      status: 'success',
      progress: {
        current: 1,
        total: 1,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath: '/tmp/photo.png',
          outputPath: '/tmp/photo.webp',
          outputs: ['/tmp/photo.webp'],
          success: true,
          message: 'conversion successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
  }

  function resetStorage(): void {
    try {
      localStorage.removeItem(PREFERENCES_STORAGE_KEY);
      localStorage.removeItem('ff-recent-conversions');
      localStorage.removeItem('ff-theme');
      localStorage.removeItem(ONBOARDING_KEY);
    } catch {
      // Storage is available in Karma; guard keeps the spec resilient.
    }
    document.documentElement.removeAttribute('data-theme');
  }

  beforeEach(async () => {
    resetStorage();

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'openDirectoryDialog',
      'openFileDialog',
      'openPath',
      'getLocalCapabilities',
      'getTranscriptionCapabilities',
      'getAppInfo',
      'getUpdateStatus',
      'listJobHistory',
      'trashInfo',
    ]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.openPath.and.resolveTo({ success: true, message: 'opened' });
    wailsSpy.listJobHistory.and.resolveTo({
      success: true,
      message: '',
      entries: [],
    });
    wailsSpy.trashInfo.and.resolveTo({
      success: true,
      message: '',
      files: 0,
      bytes: 0,
    });
    wailsSpy.getLocalCapabilities.and.resolveTo(CAPABILITIES);
    wailsSpy.getTranscriptionCapabilities.and.resolveTo(TRANSCRIPTION);
    wailsSpy.getAppInfo.and.resolveTo(APP_INFO);
    wailsSpy.getUpdateStatus.and.resolveTo(UPDATE_STATUS);

    toolchain = new ToolchainStateStub();

    await TestBed.configureTestingModule({
      imports: [Settings],
      providers: [
        { provide: Wails, useValue: wailsSpy },
        { provide: ToolchainStateService, useValue: toolchain },
      ],
    }).compileComponents();

    i18n = TestBed.inject(I18nService);
    fixture = TestBed.createComponent(Settings);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    i18n.setLocale('en');
    resetStorage();
  });

  it('renders the page, a status label and the About line in Spanish', fakeAsync(() => {
    toolchain.status.set(readyToolchain());
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    i18n.setLocale('es');
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('Configuración');
    expect(text).toContain('Herramientas y modelos');
    expect(
      element().querySelector('.settings-toolchain-head .badge')?.textContent,
    ).toContain('Listo');
    expect(text).toContain(
      'FileForge Desktop · herramientas de archivo locales',
    );
  }));

  it('renders the About build identity in Spanish', fakeAsync(() => {
    i18n.setLocale('es');
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      element().querySelector('.settings-about-version')?.textContent,
    ).toContain('1.2.0');
    expect(
      element().querySelector('.settings-about-commit')?.textContent,
    ).toContain('a1b2c3d');
    expect(
      element().querySelector('.settings-about-built-at')?.textContent,
    ).toContain('2026-11-26T12:00:00Z');

    const text = element().textContent ?? '';
    expect(text).toContain('Acerca de FileForge');
    expect(text).toContain('Versión');
    expect(text).toContain('Fecha de compilación');
  }));

  it('renders the updates block inside the About panel', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    const about = element().querySelector('.settings-about-panel');
    expect(about).not.toBeNull();
    expect(about?.querySelector('.settings-updates')).not.toBeNull();
    expect(about?.textContent).toContain(
      'Updates are off in this development build',
    );
    expect(wailsSpy.getUpdateStatus).toHaveBeenCalledTimes(1);
  }));

  it('defaults to keeping outputs next to the original', () => {    expect(component.defaultOutputDir()).toBe('');
    expect(button('.settings-next-to-original').getAttribute('aria-pressed')).toBe(
      'true',
    );
    expect(button('.settings-choose-folder').getAttribute('aria-pressed')).toBe(
      'false',
    );
    expect(element().querySelector('.settings-path')).toBeNull();
  });

  it('persists a chosen folder and clears it back to original folders', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('  /tmp/exports  ');

    button('.settings-choose-folder').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.defaultOutputDir()).toBe('/tmp/exports');
    expect(storedPreferences()).toEqual({
      defaultOutputDir: '/tmp/exports',
      whisperPath: '',
      whisperModelPath: '',
      upscaleAnimeModelPath: '',
      language: '',
    });
    expect(
      element().querySelector('.settings-path')?.textContent,
    ).toContain('/tmp/exports');
    expect(button('.settings-choose-folder').getAttribute('aria-pressed')).toBe(
      'true',
    );

    button('.settings-reset-output').click();
    fixture.detectChanges();

    expect(component.defaultOutputDir()).toBe('');
    expect(storedPreferences()).toEqual({
      defaultOutputDir: '',
      whisperPath: '',
      whisperModelPath: '',
      upscaleAnimeModelPath: '',
      language: '',
    });
    expect(element().querySelector('.settings-path')).toBeNull();
    expect(button('.settings-next-to-original').getAttribute('aria-pressed')).toBe(
      'true',
    );
  }));

  it('keeps the saved folder when the picker is cancelled', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/exports');
    button('.settings-choose-folder').click();
    flushMicrotasks();

    wailsSpy.openDirectoryDialog.and.resolveTo('');
    button('.settings-choose-folder').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.defaultOutputDir()).toBe('/tmp/exports');
  }));

  it('switches the theme and reflects it with aria-pressed', () => {
    button('.settings-theme-dark').click();
    fixture.detectChanges();

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(button('.settings-theme-dark').getAttribute('aria-pressed')).toBe(
      'true',
    );
    expect(button('.settings-theme-light').getAttribute('aria-pressed')).toBe(
      'false',
    );

    button('.settings-theme-light').click();
    fixture.detectChanges();

    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(button('.settings-theme-light').getAttribute('aria-pressed')).toBe(
      'true',
    );
  });

  it('shows the saved conversion count and clears the history', () => {
    const recent = TestBed.inject(RecentConversionsService);
    recent.record(sampleJob());
    fixture.detectChanges();

    expect(component.recentCount()).toBe(1);
    expect(
      element().querySelector('.settings-history-meta')?.textContent,
    ).toContain('1');

    button('.settings-clear-history').click();
    fixture.detectChanges();

    expect(component.recentCount()).toBe(0);
    expect(recent.recent().length).toBe(0);
    expect(element().textContent).toContain('Recent conversions cleared.');
  });

  it('resets the Home onboarding dismissal', () => {
    localStorage.setItem(ONBOARDING_KEY, '1');

    button('.settings-reset-onboarding').click();
    fixture.detectChanges();

    expect(localStorage.getItem(ONBOARDING_KEY)).toBeNull();
    expect(element().textContent).toContain(
      'Onboarding will show again on Home.',
    );
  });

  it('renders local capability checks, states and OCR languages', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('Local capabilities');
    expect(element().querySelectorAll('.settings-capability-row').length).toBe(4);
    expect(text).toContain('pdftoppm');
    expect(text).toContain('OCR page rasterizer');
    expect(text).toContain('Required');
    expect(text).toContain('Optional');
    expect(text).toContain('Ready');
    expect(text).toContain('Install poppler-utils for text verification.');
    expect(text).toContain('OCR languages: AFR · recommended AFR');
    expect(
      element().querySelector('.settings-capability-lang')?.textContent,
    ).toContain('AFR');

    const rows = element().querySelectorAll('.settings-capability-row');
    expect(
      rows[0].querySelector('.status-dot')?.getAttribute('data-state'),
    ).toBe('success');
    expect(rows[0].querySelector('.badge-success')).not.toBeNull();
  }));

  it('marks a missing required tool as missing and dangerous', fakeAsync(() => {
    wailsSpy.getLocalCapabilities.and.resolveTo({
      ...CAPABILITIES,
      checks: [
        {
          id: 'ffmpeg',
          label: 'ffmpeg',
          available: false,
          required: true,
          detail: 'Video conversion, trimming and merging',
          suggestion: 'Install ffmpeg for the video tools.',
        },
      ],
    });

    component.refreshCapabilities();
    flushMicrotasks();
    fixture.detectChanges();

    const row = element().querySelector('.settings-capability-row');
    expect(row?.textContent).toContain('Missing');
    expect(row?.querySelector('.badge-danger')).not.toBeNull();
    expect(row?.querySelector('.status-dot')?.getAttribute('data-state')).toBe(
      'danger',
    );
    expect(row?.textContent).toContain('Install ffmpeg for the video tools.');
  }));

  it('refreshes local capabilities from the panel', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getLocalCapabilities).toHaveBeenCalledTimes(1);

    button('.settings-refresh-capabilities').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getLocalCapabilities).toHaveBeenCalledTimes(2);
  }));

  it('shows a friendly IPC error and recovers with Retry', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.getLocalCapabilities.and.resolveTo({
      success: false,
      message: '<!DOCTYPE html><html><body>Cannot POST /api</body></html>',
      checks: [],
      ocrLanguages: [],
    });

    button('.settings-refresh-capabilities').click();
    flushMicrotasks();
    fixture.detectChanges();

    const errorText = element().textContent ?? '';
    expect(errorText).toContain('The local capability check did not run.');
    expect(errorText).not.toContain('DOCTYPE');
    expect(errorText).not.toContain('Cannot POST');

    wailsSpy.getLocalCapabilities.and.resolveTo(CAPABILITIES);

    button('.settings-retry-capabilities').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getLocalCapabilities).toHaveBeenCalledTimes(3);
    expect(element().textContent).toContain('OCR page rasterizer');
  }));

  it('shows the Whisper runtime, model and resolved paths', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledWith('', '');
    expect(text).toContain('Transcription');
    expect(text).toContain('Whisper');
    expect(text).toContain('Ready');
    expect(text).toContain('ggml-base · 142 MB');
    expect(text).toContain('Languages: Auto · English · Spanish');
    expect(
      element().querySelector('.settings-whisper-path')?.textContent,
    ).toContain('/fileforge/whisper/whisper-cli');
    expect(
      element().querySelector('.settings-whisper-model-path')?.textContent,
    ).toContain('ggml-base.bin');
    expect(
      element()
        .querySelector('.settings-transcription-status .status-dot')
        ?.getAttribute('data-state'),
    ).toBe('success');
    expect(button('.settings-use-detected-whisper').disabled).toBeTrue();
  }));

  it('persists chosen whisper files and refreshes the status', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.openFileDialog.and.resolveTo('/opt/whisper-cli');
    button('.settings-choose-whisper').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(storedPreferences()).toEqual({
      defaultOutputDir: '',
      whisperPath: '/opt/whisper-cli',
      whisperModelPath: '',
      upscaleAnimeModelPath: '',
      language: '',
    });
    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledWith(
      '/opt/whisper-cli',
      '',
    );

    wailsSpy.openFileDialog.and.resolveTo('/models/ggml-medium.bin');
    button('.settings-choose-whisper-model').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(storedPreferences()).toEqual({
      defaultOutputDir: '',
      whisperPath: '/opt/whisper-cli',
      whisperModelPath: '/models/ggml-medium.bin',
      upscaleAnimeModelPath: '',
      language: '',
    });
    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledWith(
      '/opt/whisper-cli',
      '/models/ggml-medium.bin',
    );
    expect(button('.settings-use-detected-whisper').disabled).toBeFalse();
  }));

  it('persists the advanced illustration model override', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      element().querySelector('.settings-upscale-anime-path')?.textContent,
    ).toContain('Using the bundled illustration model.');

    wailsSpy.openFileDialog.and.resolveTo('/models/anime.onnx');
    button('.settings-choose-upscale-anime').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      element().querySelector('.settings-upscale-anime-path')?.textContent,
    ).toContain('/models/anime.onnx');
    expect(button('.settings-clear-upscale-anime').disabled).toBeFalse();

    button('.settings-clear-upscale-anime').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(
      element().querySelector('.settings-upscale-anime-path')?.textContent,
    ).toContain('Using the bundled illustration model.');
    expect(button('.settings-clear-upscale-anime').disabled).toBeTrue();
  }));

  it('ignores a cancelled whisper file picker', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    button('.settings-choose-whisper').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.whisperPath()).toBe('');
    expect(localStorage.getItem(PREFERENCES_STORAGE_KEY)).toBeNull();
    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledTimes(1);
  }));

  it('clears whisper overrides back to the detected paths', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.openFileDialog.and.resolveTo('/opt/whisper-cli');
    button('.settings-choose-whisper').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(button('.settings-use-detected-whisper').disabled).toBeFalse();

    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledWith(
      '/opt/whisper-cli',
      '',
    );

    button('.settings-use-detected-whisper').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(storedPreferences()).toEqual({
      defaultOutputDir: '',
      whisperPath: '',
      whisperModelPath: '',
      upscaleAnimeModelPath: '',
      language: '',
    });
    expect(
      wailsSpy.getTranscriptionCapabilities.calls.mostRecent().args,
    ).toEqual(['', '']);
    expect(button('.settings-use-detected-whisper').disabled).toBeTrue();
  }));

  it('shows setup guidance when the Whisper model is missing', fakeAsync(() => {
    wailsSpy.getTranscriptionCapabilities.and.resolveTo({
      ...TRANSCRIPTION,
      message: 'Local transcription is unavailable: install a ggml model.',
      available: false,
      modelPresent: false,
      modelName: '',
      modelSizeBytes: 0,
      defaultLanguage: '',
    });

    component.refreshTranscription();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('Setup needed');
    expect(text).toContain('Whisper model not found');
    expect(text).toContain(
      'Install whisper.cpp and choose a ggml model file. FileForge never downloads models automatically.',
    );
    expect(
      element()
        .querySelector('.settings-transcription-status .status-dot')
        ?.getAttribute('data-state'),
    ).toBe('warning');
  }));

  it('renders all managed components, storage and the ready badge', fakeAsync(() => {
    toolchain.status.set(readyToolchain());
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('Tools & Models');
    expect(text).toContain('Ready');
    expect(element().querySelectorAll('.settings-toolchain-row').length).toBe(
      TOOLCHAIN_COMPONENTS.length,
    );
    expect(text).toContain('Whisper runtime');
    expect(text).toContain('Tesseract spa');
    expect(
      element()
        .querySelector('.settings-toolchain-row .status-dot')
        ?.getAttribute('data-state'),
    ).toBe('success');
    expect(
      element().querySelector('.settings-toolchain-row .badge-success'),
    ).not.toBeNull();
    expect(
      element().querySelector('.settings-toolchain-storage')?.textContent,
    ).toContain(TOOLCHAIN_STORAGE);
    expect(
      element()
        .querySelector('.settings-toolchain-head .badge')
        ?.textContent,
    ).toContain('Ready');
    expect(toolchain.refresh).toHaveBeenCalled();
  }));

  it('sends the selected Whisper model to install', fakeAsync(() => {
    toolchain.status.set(toolchainStatus());
    fixture.detectChanges();
    flushMicrotasks();

    const select = element().querySelector<HTMLSelectElement>(
      '.settings-model-select',
    );
    expect(select).not.toBeNull();
    expect(select?.value).toBe('medium');

    select!.value = 'small';
    select!.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(toolchain.install).toHaveBeenCalledWith({
      model: 'small',
      ocrLanguages: ['eng', 'spa'],
      force: false,
    });
  }));

  it('sends the updated OCR language set to install', fakeAsync(() => {
    toolchain.status.set(toolchainStatus());
    fixture.detectChanges();
    flushMicrotasks();

    const spanish = element().querySelector<HTMLInputElement>(
      '.settings-ocr-spa',
    );
    expect(spanish?.checked).toBeTrue();

    spanish!.checked = false;
    spanish!.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(toolchain.install).toHaveBeenCalledWith({
      model: 'medium',
      ocrLanguages: ['eng'],
      force: false,
    });
  }));

  it('shows install progress and cancels an active install', fakeAsync(() => {
    toolchain.status.set(
      toolchainStatus({
        installing: true,
        percent: 42,
        phase: 'model',
        detail: 'Downloading the Whisper model…',
        message: 'Local tools are being installed.',
      }),
    );
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(text).toContain('Installing');
    expect(text).toContain('Downloading the model');
    expect(text).toContain('42%');
    expect(text).toContain('Downloading the Whisper model…');
    expect(
      element()
        .querySelector('.settings-toolchain-progress .progress-fill')
        ?.getAttribute('style'),
    ).toContain('42%');

    button('.settings-cancel-toolchain').click();
    expect(toolchain.cancel).toHaveBeenCalled();
  }));

  it('opens the toolchain storage folder', fakeAsync(() => {
    toolchain.status.set(readyToolchain());
    fixture.detectChanges();
    flushMicrotasks();

    button('.settings-open-storage').click();
    flushMicrotasks();

    expect(wailsSpy.openPath).toHaveBeenCalledWith(TOOLCHAIN_STORAGE);
  }));

  it('shows the install error and retries with the current options', fakeAsync(() => {
    toolchain.status.set(
      toolchainStatus({
        success: false,
        error: 'The Whisper model download failed.',
        message: 'The Whisper model download failed.',
        phase: 'model',
        percent: 55,
      }),
    );
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().textContent).toContain(
      'The Whisper model download failed.',
    );
    expect(
      element().querySelector('.settings-toolchain-head .badge-danger'),
    ).not.toBeNull();

    button('.settings-retry-toolchain').click();

    expect(toolchain.install).toHaveBeenCalledWith({
      model: 'medium',
      ocrLanguages: ['eng', 'spa'],
      force: false,
    });
  }));

  it('shows an unsupported-platform message without an install button', fakeAsync(() => {
    toolchain.status.set(
      toolchainStatus({
        message:
          'The managed toolchain is not available for linux/arm64: no verified prebuilt binaries exist for this platform.',
        detail:
          'The managed toolchain is not available for linux/arm64: no verified prebuilt binaries exist for this platform.',
      }),
    );
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(element().textContent).toContain('not available for linux/arm64');
    expect(element().querySelector('.settings-install-toolchain')).toBeNull();
  }));

  it('keeps Install / repair available when an optional pack is missing', fakeAsync(() => {
    toolchain.status.set(
      toolchainStatus({
        ready: true,
        components: [
          ...TOOLCHAIN_COMPONENTS,
          {
            id: 'upscale',
            label: 'Upscale engine',
            ready: false,
            required: false,
            detail: 'The upscale engine is not installed. Run Install / repair.',
          },
          {
            id: 'upscale-model',
            label: 'Upscale model',
            ready: false,
            required: false,
            detail: 'Upscale model is not installed. Run Install / repair.',
          },
        ],
      }),
    );

    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    const install = element().querySelector(
      '.settings-install-toolchain',
    ) as HTMLButtonElement | null;
    expect(install).not.toBeNull();
    expect(install?.textContent).toContain('Install / repair');

    install?.click();
    flushMicrotasks();

    expect(toolchain.install).toHaveBeenCalled();
  }));

  it('reloads dependent panels once the toolchain becomes ready', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();
    expect(wailsSpy.getLocalCapabilities).toHaveBeenCalledTimes(1);
    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledTimes(1);

    toolchain.status.set(readyToolchain());
    fixture.detectChanges();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getLocalCapabilities).toHaveBeenCalledTimes(2);
    expect(wailsSpy.getTranscriptionCapabilities).toHaveBeenCalledTimes(2);
  }));
});
