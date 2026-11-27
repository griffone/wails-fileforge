import { TestBed } from '@angular/core/testing';

import {
  TOOL_OPTIONS_STORAGE_PREFIX,
  PreferencesService,
  toolOptionsStorageKey,
} from '../services/preferences.service';
import { createToolOptions, ToolOptionsStore } from './tool-options';

describe('createToolOptions', () => {
  type Options = {
    quality: number;
    strip: boolean;
    nested: { label: string };
  };

  function defaults(): Options {
    return { quality: 80, strip: true, nested: { label: 'auto' } };
  }

  function normalize(stored: Record<string, unknown>): Partial<Options> {
    const result: Partial<Options> = {};
    if (typeof stored['quality'] === 'number') {
      result.quality = stored['quality'];
    }
    if (typeof stored['strip'] === 'boolean') {
      result.strip = stored['strip'];
    }
    const nested = stored['nested'];
    if (typeof nested === 'object' && nested !== null) {
      const label = (nested as Record<string, unknown>)['label'];
      if (typeof label === 'string') {
        result.nested = { label };
      }
    }
    return result;
  }

  function store(toolId = 'tool.helper'): ToolOptionsStore<Options> {
    return createToolOptions(TestBed.inject(PreferencesService), {
      toolId,
      schemaVersion: 3,
      defaults,
      normalize,
    });
  }

  function clearStorage(): void {
    try {
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
    clearStorage();
    TestBed.configureTestingModule({});
  });

  afterEach(() => {
    clearStorage();
  });

  it('returns fresh defaults when nothing is stored', () => {
    const first = store().restore();
    const second = store().restore();

    expect(first).toEqual({ quality: 80, strip: true, nested: { label: 'auto' } });
    expect(second).toEqual(first);
    expect(second).not.toBe(first);
    expect(second.nested).not.toBe(first.nested);
  });

  it('saves and restores a round trip', () => {
    const options = store();
    options.save({ quality: 55, strip: false, nested: { label: 'custom' } });

    expect(store().restore()).toEqual({
      quality: 55,
      strip: false,
      nested: { label: 'custom' },
    });
  });

  it('merges only keys the defaults know about', () => {
    const service = TestBed.inject(PreferencesService);
    localStorage.setItem(
      toolOptionsStorageKey('tool.helper'),
      JSON.stringify({
        schemaVersion: 3,
        values: { quality: 60, injected: 'ignored' },
      }),
    );

    const restored = store().restore();
    expect(restored).toEqual({
      quality: 60,
      strip: true,
      nested: { label: 'auto' },
    });
    expect((restored as unknown as Record<string, unknown>)['injected']).toBeUndefined();
    // The raw document is untouched; the helper owns the projection.
    expect(service.readToolOptions('tool.helper', 3, ['quality'])).toEqual({
      quality: 60,
    });
  });

  it('falls back to defaults on a schema version mismatch', () => {
    localStorage.setItem(
      toolOptionsStorageKey('tool.helper'),
      JSON.stringify({ schemaVersion: 2, values: { quality: 10 } }),
    );

    expect(store().restore()).toEqual({
      quality: 80,
      strip: true,
      nested: { label: 'auto' },
    });
  });

  it('falls back to defaults on a corrupt payload', () => {
    localStorage.setItem(toolOptionsStorageKey('tool.helper'), '{not json');

    expect(() => store().restore()).not.toThrow();
    expect(store().restore()).toEqual({
      quality: 80,
      strip: true,
      nested: { label: 'auto' },
    });
  });

  it('falls back to defaults when normalization throws', () => {
    const options = createToolOptions<Options>(TestBed.inject(PreferencesService), {
      toolId: 'tool.throwing',
      schemaVersion: 1,
      defaults,
      normalize: () => {
        throw new Error('bad stored value');
      },
    });
    TestBed.inject(PreferencesService).saveToolOptions('tool.throwing', 1, {
      quality: 10,
    });

    expect(options.restore()).toEqual({
      quality: 80,
      strip: true,
      nested: { label: 'auto' },
    });
  });

  it('keeps each tool isolated and clears only its own document', () => {
    const first = store('tool.a');
    const second = store('tool.b');
    first.save({ quality: 10, strip: false, nested: { label: 'a' } });
    second.save({ quality: 90, strip: true, nested: { label: 'b' } });

    first.clear();

    expect(store('tool.a').restore().quality).toBe(80);
    expect(store('tool.b').restore().quality).toBe(90);
  });
});
