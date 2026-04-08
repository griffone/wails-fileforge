import { TestBed } from '@angular/core/testing';
import { Wails, JobProgressEventV1 } from './wails';
import { Events } from '@wailsio/runtime';

describe('Wails jobProgress$', () => {
  let service: Wails;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(Wails);
  });

  it('jobProgress$ emits when native Events.On handler receives an event', (done) => {
    const payload: JobProgressEventV1 = {
      jobId: 'job-1',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: { current: 1, total: 2, stage: 'processing', message: 'half', etaSeconds: 10 },
    };

    // Spy on Events.On to capture the registered handler and invoke it
    let registeredHandler: ((ev: { data?: unknown }) => void) | null = null;
    spyOn(Events, 'On').and.callFake((topic: string, handler: (ev: { data?: unknown }) => void) => {
      registeredHandler = handler;
      // return a noop unsubscribe
      return () => {};
    });

    const sub = service.jobProgress$.subscribe((v) => {
      try {
        expect(v.jobId).toBe(payload.jobId);
        expect(v.toolId).toBe(payload.toolId);
        expect(v.status).toBe(payload.status);
        expect(v.progress.current).toBe(payload.progress.current);
        done();
      } finally {
        sub.unsubscribe();
      }
    });

    // simulate native event
    registeredHandler?.({ data: payload });
  });
});
