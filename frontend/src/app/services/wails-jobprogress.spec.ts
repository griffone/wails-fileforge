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

    // Spy on Events.On to capture the registered handler and invoke it.
    // Cast to any to avoid strict Wails types in tests. Use `any` so the
    // test can invoke the captured handler without TypeScript callable errors.
    let registeredHandler: any = null;
    spyOn(Events as any, 'On').and.callFake((topic: string, handler: any) => {
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

    // simulate native event — Wails event shape: { name, data }
    // Invoke the captured handler with the expected payload envelope.
    registeredHandler?.({ name: 'jobs/progress/v1', data: payload });
  });
});
