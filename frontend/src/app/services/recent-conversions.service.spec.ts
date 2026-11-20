import { TestBed } from '@angular/core/testing';

import { RecentConversionsService } from './recent-conversions.service';
import { JobResult } from './wails';

describe('RecentConversionsService', () => {
  const storageKey = 'ff-recent-conversions';

  function createService(): RecentConversionsService {
    TestBed.configureTestingModule({});
    return TestBed.inject(RecentConversionsService);
  }

  function job(overrides: Partial<JobResult> = {}): JobResult {
    return {
      jobId: 'job-1',
      success: true,
      message: '',
      toolId: 'tool.image.convert',
      status: 'success',
      progress: { current: 1, total: 1, stage: 'done', message: '' },
      items: [
        {
          inputPath: '/tmp/in/a.png',
          outputPath: '/tmp/out/a.webp',
          success: true,
          message: 'ok',
        },
      ],
      startedAt: 1700000000000,
      endedAt: 1700000005000,
      ...overrides,
    };
  }

  beforeEach(() => {
    localStorage.removeItem(storageKey);
  });

  afterEach(() => {
    localStorage.removeItem(storageKey);
  });

  it('records a terminal job with resolved tool metadata and outputs', () => {
    const service = createService();

    service.record(job());

    expect(service.recent().length).toBe(1);
    const entry = service.recent()[0];
    expect(entry.id).toBe('job-1');
    expect(entry.toolId).toBe('tool.image.convert');
    expect(entry.toolName).toBe('Image Convert');
    expect(entry.route).toBe('/image-converter');
    expect(entry.status).toBe('success');
    expect(entry.outputs).toEqual(['/tmp/out/a.webp']);
    expect(entry.inputCount).toBe(1);
    expect(entry.timestamp).toBe(1700000005000);
  });

  it('ignores non-terminal jobs and unknown tools', () => {
    const service = createService();

    service.record(job({ jobId: 'running', status: 'running' }));
    service.record(job({ jobId: 'queued', status: 'queued' }));
    service.record(job({ jobId: 'unknown', toolId: 'tool.unknown' }));

    expect(service.recent()).toEqual([]);
  });

  it('deduplicates by job id and prepends the newest entry', () => {
    const service = createService();

    service.record(job({ jobId: 'a', endedAt: 1000 }));
    service.record(job({ jobId: 'b', endedAt: 2000 }));
    service.record(job({ jobId: 'a', endedAt: 3000, status: 'failed' }));

    expect(service.recent().map((entry) => entry.id)).toEqual(['a', 'b']);
    expect(service.recent()[0].status).toBe('failed');
  });

  it('caps the history at eight entries', () => {
    const service = createService();

    for (let index = 0; index < 10; index += 1) {
      service.record(job({ jobId: `job-${index}`, endedAt: 1000 + index }));
    }

    expect(service.recent().length).toBe(8);
    expect(service.recent()[0].id).toBe('job-9');
    expect(service.recent()[7].id).toBe('job-2');
  });

  it('prefers item outputs, falls back to outputPath and dedupes paths', () => {
    const service = createService();

    service.record(
      job({
        items: [
          {
            inputPath: '/tmp/in/a.png',
            outputPath: '/tmp/out/a.png',
            outputs: ['/tmp/out/a.webp', '/tmp/out/a.webp'],
            success: true,
            message: 'ok',
          },
          {
            inputPath: '/tmp/in/b.png',
            outputPath: '/tmp/out/b.webp',
            success: true,
            message: 'ok',
          },
          {
            inputPath: '/tmp/in/c.png',
            outputPath: '',
            success: false,
            message: 'nope',
          },
        ],
      }),
    );

    expect(service.recent()[0].outputs).toEqual([
      '/tmp/out/a.webp',
      '/tmp/out/b.webp',
    ]);
    expect(service.recent()[0].inputCount).toBe(3);
  });

  it('caps the outputs collected from one job', () => {
    const service = createService();
    const outputs = Array.from({ length: 25 }, (_, index) => `/tmp/out/${index}.webp`);

    service.record(
      job({
        items: [
          {
            inputPath: '/tmp/in/a.png',
            outputPath: '',
            outputs,
            success: true,
            message: 'ok',
          },
        ],
      }),
    );

    expect(service.recent()[0].outputs.length).toBe(20);
  });

  it('persists metadata and reloads it into a fresh service', () => {
    const service = createService();

    service.record(job());

    const raw = localStorage.getItem(storageKey);
    expect(raw).toContain('/image-converter');
    expect(raw).toContain('/tmp/out/a.webp');

    TestBed.resetTestingModule();
    const reloaded = createService();

    expect(reloaded.recent().length).toBe(1);
    expect(reloaded.recent()[0]).toEqual(
      jasmine.objectContaining({
        id: 'job-1',
        toolName: 'Image Convert',
        route: '/image-converter',
        status: 'success',
        outputs: ['/tmp/out/a.webp'],
      }),
    );
  });

  it('clears in memory and in storage', () => {
    const service = createService();
    service.record(job());

    service.clear();

    expect(service.recent()).toEqual([]);
    expect(localStorage.getItem(storageKey)).toBe('[]');
  });

  it('ignores corrupted storage and drops stale or malformed entries', () => {
    localStorage.setItem(storageKey, '{not json');
    const corrupted = createService();
    expect(corrupted.recent()).toEqual([]);

    TestBed.resetTestingModule();
    localStorage.setItem(
      storageKey,
      JSON.stringify([
        {
          id: 'valid',
          toolId: 'tool.pdf.merge',
          route: '/pdf-merge',
          status: 'partial_success',
          outputs: ['/tmp/out/merged.pdf'],
          inputCount: 2,
          timestamp: 1700000000000,
        },
        {
          id: 'stale',
          toolId: 'tool.removed',
          route: '/removed',
          status: 'success',
          outputs: [],
          inputCount: 1,
          timestamp: 1700000000000,
        },
        {
          id: 'running',
          toolId: 'tool.pdf.merge',
          route: '/pdf-merge',
          status: 'running',
          outputs: [],
          inputCount: 1,
          timestamp: 1700000000000,
        },
        {
          id: 'no-time',
          toolId: 'tool.pdf.merge',
          route: '/pdf-merge',
          status: 'success',
          outputs: [],
          inputCount: 1,
        },
        {
          id: 'valid',
          toolId: 'tool.pdf.merge',
          route: '/pdf-merge',
          status: 'failed',
          outputs: [],
          inputCount: 1,
          timestamp: 1700000000000,
        },
        42,
      ]),
    );
    const partlyValid = createService();

    expect(partlyValid.recent().map((entry) => entry.id)).toEqual(['valid']);
    expect(partlyValid.recent()[0].toolName).toBe('PDF Merge');
    expect(partlyValid.recent()[0].status).toBe('partial_success');
  });
});
