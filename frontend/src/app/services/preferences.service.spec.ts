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

  it('stores a trimmed folder and persists it', () => {
    const service = TestBed.inject(PreferencesService);

    service.setDefaultOutputDir('  /tmp/exports  ');

    expect(service.defaultOutputDir()).toBe('/tmp/exports');
    expect(JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}')).toEqual({
      defaultOutputDir: '/tmp/exports',
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
    });
  });

  it('treats an empty folder as next to the original', () => {
    const service = TestBed.inject(PreferencesService);

    service.setDefaultOutputDir('   ');

    expect(service.defaultOutputDir()).toBe('');
  });

  it('falls back to empty defaults for malformed storage', () => {
    localStorage.setItem(PREFERENCES_STORAGE_KEY, '{not json');

    expect(TestBed.inject(PreferencesService).defaultOutputDir()).toBe('');
  });

  it('ignores unexpected value shapes in storage', () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ defaultOutputDir: 42 }),
    );

    expect(TestBed.inject(PreferencesService).defaultOutputDir()).toBe('');
  });

  it('keeps working when storage reads are blocked', () => {
    spyOn(Storage.prototype, 'getItem').and.throwError('blocked');

    const service = TestBed.inject(PreferencesService);

    expect(service.defaultOutputDir()).toBe('');
    service.setDefaultOutputDir('/tmp/exports');
    expect(service.defaultOutputDir()).toBe('/tmp/exports');
  });
});
