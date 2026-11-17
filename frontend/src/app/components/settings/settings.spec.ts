import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
} from '@angular/core/testing';

import { Settings } from './settings';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import {
  JobResultV1,
  LocalCapabilitiesResponseV1,
  TranscriptionCapabilitiesResponseV1,
  Wails,
} from '../../services/wails';

const ONBOARDING_KEY = 'ff-onboarding-dismissed';

const CAPABILITIES: LocalCapabilitiesResponseV1 = {
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
      id: 'libreoffice',
      label: 'libreoffice',
      available: false,
      required: false,
      detail: 'DOCX to PDF/A archival export',
      suggestion: 'Install LibreOffice for PDF/A DOCX exports.',
    },
  ],
  ocrLanguages: ['afr'],
  recommendedOcrLanguage: 'afr',
};

const TRANSCRIPTION: TranscriptionCapabilitiesResponseV1 = {
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

describe('Settings', () => {
  let fixture: ComponentFixture<Settings>;
  let component: Settings;
  let wailsSpy: jasmine.SpyObj<Wails>;

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

  function sampleJob(): JobResultV1 {
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
      'getLocalCapabilitiesV1',
      'getTranscriptionCapabilitiesV1',
    ]);
    wailsSpy.openDirectoryDialog.and.resolveTo('');
    wailsSpy.openFileDialog.and.resolveTo('');
    wailsSpy.getLocalCapabilitiesV1.and.resolveTo(CAPABILITIES);
    wailsSpy.getTranscriptionCapabilitiesV1.and.resolveTo(TRANSCRIPTION);

    await TestBed.configureTestingModule({
      imports: [Settings],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(Settings);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    resetStorage();
  });

  it('defaults to keeping outputs next to the original', () => {
    expect(component.defaultOutputDir()).toBe('');
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
    expect(text).toContain('Install LibreOffice for PDF/A DOCX exports.');
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
    wailsSpy.getLocalCapabilitiesV1.and.resolveTo({
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

    expect(wailsSpy.getLocalCapabilitiesV1).toHaveBeenCalledTimes(1);

    button('.settings-refresh-capabilities').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getLocalCapabilitiesV1).toHaveBeenCalledTimes(2);
  }));

  it('shows a friendly IPC error and recovers with Retry', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.getLocalCapabilitiesV1.and.resolveTo({
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

    wailsSpy.getLocalCapabilitiesV1.and.resolveTo(CAPABILITIES);

    button('.settings-retry-capabilities').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(wailsSpy.getLocalCapabilitiesV1).toHaveBeenCalledTimes(3);
    expect(element().textContent).toContain('OCR page rasterizer');
  }));

  it('shows the Whisper runtime, model and resolved paths', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    const text = element().textContent ?? '';
    expect(wailsSpy.getTranscriptionCapabilitiesV1).toHaveBeenCalledWith('', '');
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
    });
    expect(wailsSpy.getTranscriptionCapabilitiesV1).toHaveBeenCalledWith(
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
    });
    expect(wailsSpy.getTranscriptionCapabilitiesV1).toHaveBeenCalledWith(
      '/opt/whisper-cli',
      '/models/ggml-medium.bin',
    );
    expect(button('.settings-use-detected-whisper').disabled).toBeFalse();
  }));

  it('ignores a cancelled whisper file picker', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    button('.settings-choose-whisper').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.whisperPath()).toBe('');
    expect(localStorage.getItem(PREFERENCES_STORAGE_KEY)).toBeNull();
    expect(wailsSpy.getTranscriptionCapabilitiesV1).toHaveBeenCalledTimes(1);
  }));

  it('clears whisper overrides back to the detected paths', fakeAsync(() => {
    flushMicrotasks();
    fixture.detectChanges();

    wailsSpy.openFileDialog.and.resolveTo('/opt/whisper-cli');
    button('.settings-choose-whisper').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(button('.settings-use-detected-whisper').disabled).toBeFalse();

    expect(wailsSpy.getTranscriptionCapabilitiesV1).toHaveBeenCalledWith(
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
    });
    expect(
      wailsSpy.getTranscriptionCapabilitiesV1.calls.mostRecent().args,
    ).toEqual(['', '']);
    expect(button('.settings-use-detected-whisper').disabled).toBeTrue();
  }));

  it('shows setup guidance when the Whisper model is missing', fakeAsync(() => {
    wailsSpy.getTranscriptionCapabilitiesV1.and.resolveTo({
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
});
