import { TestBed } from '@angular/core/testing';

import {
  LAST_REQUEST_STORAGE_PREFIX,
  LAST_REQUEST_SCHEMA_VERSION,
  LastRequestService,
} from './last-request.service';
import type { JobRequest } from './wails';

describe('LastRequestService', () => {
  function request(overrides: Partial<JobRequest> = {}): JobRequest {
    return {
      toolId: 'tool.image.compress',
      mode: 'single',
      inputPaths: ['/tmp/a.png'],
      outputDir: '/tmp/out',
      options: { targetSizeBytes: 1048576, format: 'auto' },
      ...overrides,
    };
  }

  function clearStorage(): void {
    try {
      const keys: string[] = [];
      for (let index = 0; index < localStorage.length; index += 1) {
        const key = localStorage.key(index);
        if (key?.startsWith(LAST_REQUEST_STORAGE_PREFIX)) {
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

  function reload(): LastRequestService {
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({});
    return TestBed.inject(LastRequestService);
  }

  beforeEach(() => {
    clearStorage();
    TestBed.configureTestingModule({});
  });

  afterEach(() => {
    clearStorage();
  });

  it('saves the request with the tool id and a timestamp', () => {
    const before = Date.now();
    TestBed.inject(LastRequestService).save(request());
    const stored = TestBed.inject(LastRequestService).get('tool.image.compress');

    expect(stored).not.toBeNull();
    expect(stored?.toolId).toBe('tool.image.compress');
    expect(stored?.submittedAt).toBeGreaterThanOrEqual(before);
    expect(stored?.request.inputPaths).toEqual(['/tmp/a.png']);
    expect(stored?.request.outputDir).toBe('/tmp/out');
    expect(stored?.request.options).toEqual({
      targetSizeBytes: 1048576,
      format: 'auto',
    });
  });

  it('still offers the request after a restart', () => {
    TestBed.inject(LastRequestService).save(request());

    const restored = reload().get('tool.image.compress');

    expect(restored?.request).toEqual(request());
  });

  it('keys the stored request by tool id', () => {
    const service = TestBed.inject(LastRequestService);
    service.save(request());
    service.save(
      request({
        toolId: 'tool.pdf.optimize',
        inputPaths: ['/tmp/a.pdf'],
        mode: 'single',
      }),
    );

    expect(service.get('tool.image.compress')?.request.inputPaths).toEqual([
      '/tmp/a.png',
    ]);
    expect(service.get('tool.pdf.optimize')?.request.inputPaths).toEqual([
      '/tmp/a.pdf',
    ]);
  });

  it('keeps only the newest request for one tool', () => {
    const service = TestBed.inject(LastRequestService);
    service.save(request());
    service.save(request({ inputPaths: ['/tmp/b.png'] }));

    expect(service.get('tool.image.compress')?.request.inputPaths).toEqual([
      '/tmp/b.png',
    ]);
  });

  it('returns null for a retired payload version', () => {
    localStorage.setItem(
      `${LAST_REQUEST_STORAGE_PREFIX}tool.image.compress`,
      JSON.stringify({
        schemaVersion: LAST_REQUEST_SCHEMA_VERSION + 1,
        toolId: 'tool.image.compress',
        submittedAt: 1,
        request: request(),
      }),
    );

    expect(TestBed.inject(LastRequestService).get('tool.image.compress')).toBeNull();
  });

  it('returns null instead of throwing for a corrupt payload', () => {
    localStorage.setItem(
      `${LAST_REQUEST_STORAGE_PREFIX}tool.image.compress`,
      '{not json',
    );
    const service = TestBed.inject(LastRequestService);

    expect(() => service.get('tool.image.compress')).not.toThrow();
    expect(service.get('tool.image.compress')).toBeNull();
  });

  it('refuses to store a request without inputs', () => {
    const service = TestBed.inject(LastRequestService);
    service.save(request({ inputPaths: [] }));

    expect(service.get('tool.image.compress')).toBeNull();
  });

  it('clears one tool without touching another', () => {
    const service = TestBed.inject(LastRequestService);
    service.save(request());
    service.save(request({ toolId: 'tool.pdf.optimize' }));

    service.clear('tool.image.compress');

    expect(service.get('tool.image.compress')).toBeNull();
    expect(service.get('tool.pdf.optimize')).not.toBeNull();
  });
});
