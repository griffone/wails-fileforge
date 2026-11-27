import { TestBed } from '@angular/core/testing';

import {
  PREFERENCES_STORAGE_KEY,
  TOOL_OPTIONS_STORAGE_PREFIX,
  PreferencesService,
  toolOptionsStorageKey,
} from './preferences.service';

describe('PreferencesService', () => {
  function resetStorage(): void {
    try {
      localStorage.removeItem(PREFERENCES_STORAGE_KEY);
      const keys: string[] = [];
      for (let index = 0; index < localStorage.length; index += 1) {
        const key = localStorage.key(index);
        if (key?.startsWith(TOOL_OPTIONS_STORAGE_PREFIX)) {
          keys.push(key);
        }
      }
      for (const key of keys) {
        localStorage.removeItem(key);
      }
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
      upscaleAnimeModelPath: '',
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
      upscaleAnimeModelPath: '',
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
      upscaleAnimeModelPath: '',
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

  it('stores and clears the optional anime upscale model path', () => {
    const service = TestBed.inject(PreferencesService);

    service.setUpscaleAnimeModelPath('  /models/anime.onnx  ');
    expect(service.upscaleAnimeModelPath()).toBe('/models/anime.onnx');
    expect(
      JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')
        .upscaleAnimeModelPath,
    ).toBe('/models/anime.onnx');

    service.setWhisperPath('/opt/whisper-cli');
    service.clearUpscaleAnimeModelPath();

    expect(service.upscaleAnimeModelPath()).toBe('');
    expect(service.whisperPath()).toBe('/opt/whisper-cli');
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
        upscaleAnimeModelPath: '',
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

  it('round-trips one tool options document under its own key', () => {
    const service = TestBed.inject(PreferencesService);

    service.saveToolOptions('tool.a', 1, { quality: 80, strip: false });

    const key = toolOptionsStorageKey('tool.a');
    expect(key).toBe('ff-tool-options:tool.a');
    expect(JSON.parse(localStorage.getItem(key) ?? '{}')).toEqual({
      schemaVersion: 1,
      values: { quality: 80, strip: false },
    });
    expect(
      service.readToolOptions('tool.a', 1, ['quality', 'strip']),
    ).toEqual({ quality: 80, strip: false });
  });

  it('reloads tool options in a new instance', () => {
    TestBed.inject(PreferencesService).saveToolOptions('tool.a', 2, {
      mode: 'fast',
    });

    TestBed.resetTestingModule();
    TestBed.configureTestingModule({});

    expect(
      TestBed.inject(PreferencesService).readToolOptions('tool.a', 2, ['mode']),
    ).toEqual({ mode: 'fast' });
  });

  it('ignores a tool options document from another schema version', () => {
    const service = TestBed.inject(PreferencesService);
    localStorage.setItem(
      toolOptionsStorageKey('tool.a'),
      JSON.stringify({ schemaVersion: 2, values: { mode: 'fast' } }),
    );

    expect(service.readToolOptions('tool.a', 1, ['mode'])).toBeNull();
  });

  it('treats a corrupt tool options payload as absent', () => {
    const service = TestBed.inject(PreferencesService);
    localStorage.setItem(toolOptionsStorageKey('tool.a'), '{not json');

    expect(() =>
      service.readToolOptions('tool.a', 1, ['mode']),
    ).not.toThrow();
    expect(service.readToolOptions('tool.a', 1, ['mode'])).toBeNull();
  });

  it('treats a non-record payload as absent', () => {
    const service = TestBed.inject(PreferencesService);
    localStorage.setItem(toolOptionsStorageKey('tool.a'), JSON.stringify([]));

    expect(service.readToolOptions('tool.a', 1, ['mode'])).toBeNull();
  });

  it('drops unknown keys from a stored tool options payload', () => {
    const service = TestBed.inject(PreferencesService);
    localStorage.setItem(
      toolOptionsStorageKey('tool.a'),
      JSON.stringify({
        schemaVersion: 1,
        values: { mode: 'fast', injected: true },
      }),
    );

    expect(service.readToolOptions('tool.a', 1, ['mode'])).toEqual({
      mode: 'fast',
    });
  });

  it('isolates tool options per tool id', () => {
    const service = TestBed.inject(PreferencesService);

    service.saveToolOptions('tool.a', 1, { mode: 'fast' });
    service.saveToolOptions('tool.b', 1, { mode: 'slow' });

    expect(service.readToolOptions('tool.a', 1, ['mode'])).toEqual({
      mode: 'fast',
    });
    expect(service.readToolOptions('tool.b', 1, ['mode'])).toEqual({
      mode: 'slow',
    });

    service.clearToolOptions('tool.a');

    expect(service.readToolOptions('tool.a', 1, ['mode'])).toBeNull();
    expect(service.readToolOptions('tool.b', 1, ['mode'])).toEqual({
      mode: 'slow',
    });
  });

  it('ignores tool options with an empty tool id', () => {
    const service = TestBed.inject(PreferencesService);

    service.saveToolOptions('   ', 1, { mode: 'fast' });

    expect(service.readToolOptions('   ', 1, ['mode'])).toBeNull();
  });

  it('keeps tool options working when storage writes are blocked', () => {
    const service = TestBed.inject(PreferencesService);
    spyOn(Storage.prototype, 'setItem').and.throwError('blocked');

    expect(() =>
      service.saveToolOptions('tool.a', 1, { mode: 'fast' }),
    ).not.toThrow();
  });
});
