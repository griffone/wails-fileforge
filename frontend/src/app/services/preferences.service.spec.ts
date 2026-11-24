import { TestBed } from '@angular/core/testing';

import {
  PREFERENCES_STORAGE_KEY,
  PreferencesService,
} from './preferences.service';

describe('PreferencesService', () => {
  function resetStorage(): void {
    try {
      localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    } catch {
      // Storage is available in Karma; guard keeps the spec resilient.
    }
  }

  beforeEach(() => {
    resetStorage();
    TestBed.configureTestingModule({});
  });

  afterEach(() => {
    resetStorage();
  });

  it('starts with no default output folder', () => {
    const service = TestBed.inject(PreferencesService);

    expect(service.defaultOutputDir()).toBe('');
  });

  it('starts with no whisper path overrides', () => {
    const service = TestBed.inject(PreferencesService);

    expect(service.whisperPath()).toBe('');
    expect(service.whisperModelPath()).toBe('');
  });

  it('stores a trimmed folder and persists it', () => {
    const service = TestBed.inject(PreferencesService);

    service.setDefaultOutputDir('  /tmp/exports  ');

    expect(service.defaultOutputDir()).toBe('/tmp/exports');
    expect(JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')).toEqual({
      defaultOutputDir: '/tmp/exports',
      whisperPath: '',
      whisperModelPath: '',
      language: '',
    });
  });

  it('reloads the saved folder in a new instance', () => {
    TestBed.inject(PreferencesService).setDefaultOutputDir('/tmp/exports');

    TestBed.resetTestingModule();
    TestBed.configureTestingModule({});

    expect(TestBed.inject(PreferencesService).defaultOutputDir()).toBe(
      '/tmp/exports',
    );
  });

  it('clears the folder in memory and in storage', () => {
    const service = TestBed.inject(PreferencesService);
    service.setDefaultOutputDir('/tmp/exports');

    service.clearDefaultOutputDir();

    expect(service.defaultOutputDir()).toBe('');
    expect(JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')).toEqual({
      defaultOutputDir: '',
      whisperPath: '',
      whisperModelPath: '',
      language: '',
    });
  });

  it('treats an empty folder as next to the original', () => {
    const service = TestBed.inject(PreferencesService);

    service.setDefaultOutputDir('   ');

    expect(service.defaultOutputDir()).toBe('');
  });

  it('stores trimmed whisper paths and persists them', () => {
    const service = TestBed.inject(PreferencesService);

    service.setWhisperPath('  /opt/whisper-cli  ');
    service.setWhisperModelPath('  /models/ggml-base.bin  ');

    expect(service.whisperPath()).toBe('/opt/whisper-cli');
    expect(service.whisperModelPath()).toBe('/models/ggml-base.bin');
    expect(JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')).toEqual({
      defaultOutputDir: '',
      whisperPath: '/opt/whisper-cli',
      whisperModelPath: '/models/ggml-base.bin',
      language: '',
    });
  });

  it('reloads the saved whisper paths in a new instance', () => {
    const service = TestBed.inject(PreferencesService);
    service.setWhisperPath('/opt/whisper-cli');
    service.setWhisperModelPath('/models/ggml-base.bin');

    TestBed.resetTestingModule();
    TestBed.configureTestingModule({});

    const reloaded = TestBed.inject(PreferencesService);
    expect(reloaded.whisperPath()).toBe('/opt/whisper-cli');
    expect(reloaded.whisperModelPath()).toBe('/models/ggml-base.bin');
  });

  it('clears whisper paths without touching the output folder', () => {
    const service = TestBed.inject(PreferencesService);
    service.setDefaultOutputDir('/tmp/exports');
    service.setWhisperPath('/opt/whisper-cli');
    service.setWhisperModelPath('/models/ggml-base.bin');

    service.clearWhisperPaths();

    expect(service.whisperPath()).toBe('');
    expect(service.whisperModelPath()).toBe('');
    expect(service.defaultOutputDir()).toBe('/tmp/exports');
  });

  it('keeps whisper overrides when the output folder is cleared', () => {
    const service = TestBed.inject(PreferencesService);
    service.setDefaultOutputDir('/tmp/exports');
    service.setWhisperPath('/opt/whisper-cli');
    service.setWhisperModelPath('/models/ggml-base.bin');

    service.clearDefaultOutputDir();

    expect(service.defaultOutputDir()).toBe('');
    expect(service.whisperPath()).toBe('/opt/whisper-cli');
    expect(service.whisperModelPath()).toBe('/models/ggml-base.bin');
  });

  it('treats empty whisper paths as backend defaults', () => {
    const service = TestBed.inject(PreferencesService);

    service.setWhisperPath('   ');
    service.setWhisperModelPath('   ');

    expect(service.whisperPath()).toBe('');
    expect(service.whisperModelPath()).toBe('');
  });

  it('starts with no language preference so the browser decides', () => {
    const service = TestBed.inject(PreferencesService);

    expect(service.language()).toBe('');
  });

  it('persists and reloads the chosen language', () => {
    const service = TestBed.inject(PreferencesService);

    service.setLanguage('es');

    expect(service.language()).toBe('es');
    expect(
      JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')
        .language,
    ).toBe('es');

    TestBed.resetTestingModule();
    TestBed.configureTestingModule({});

    expect(TestBed.inject(PreferencesService).language()).toBe('es');
  });

  it('rejects an unknown stored language', () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ language: 'fr' }),
    );

    const service = TestBed.inject(PreferencesService);
    expect(service.language()).toBe('');
  });

  it('falls back to empty defaults for malformed storage', () => {
    localStorage.setItem(PREFERENCES_STORAGE_KEY, '{not json');

    const service = TestBed.inject(PreferencesService);
    expect(service.defaultOutputDir()).toBe('');
    expect(service.whisperPath()).toBe('');
    expect(service.whisperModelPath()).toBe('');
  });

  it('ignores unexpected value shapes in storage', () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({
        defaultOutputDir: 42,
        whisperPath: 7,
        whisperModelPath: false,
      }),
    );

    const service = TestBed.inject(PreferencesService);
    expect(service.defaultOutputDir()).toBe('');
    expect(service.whisperPath()).toBe('');
    expect(service.whisperModelPath()).toBe('');
  });

  it('keeps working when storage reads are blocked', () => {
    spyOn(Storage.prototype, 'getItem').and.throwError('blocked');

    const service = TestBed.inject(PreferencesService);

    expect(service.defaultOutputDir()).toBe('');
    service.setDefaultOutputDir('/tmp/exports');
    service.setWhisperPath('/opt/whisper-cli');
    expect(service.defaultOutputDir()).toBe('/tmp/exports');
    expect(service.whisperPath()).toBe('/opt/whisper-cli');
  });
});
