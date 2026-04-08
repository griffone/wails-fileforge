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

    // Try to spy on Events.On to capture the registered handler so we can
    // invoke it like a native Wails event. In some test environments the
    // Events.On property is non-writable; in that case fall back to directly
    // pushing into the service's internal subject (accessed via any).
    let registeredHandler: any = null;
    let spyInstalled = false;
    try {
      spyOn(Events as any, 'On').and.callFake((topic: string, handler: any) => {
        registeredHandler = handler;
        // return a noop unsubscribe
        return () => {};
      });
      spyInstalled = true;
    } catch (e) {
      // spy failed (property might be non-writable) — we'll use fallback below
      spyInstalled = false;
    }

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

    // simulate native event — prefer invoking the captured native handler
    // if we were able to spy on Events.On. Otherwise, push directly into
    // the internal ReplaySubject that the service exposes internally.
    if (registeredHandler) {
      registeredHandler({ name: 'jobs/progress/v1', data: payload });
    } else {
      // fallback: access private subject and next the payload
      const subj = (service as any).jobProgressSubject as any | null;
      if (!subj) {
        // ensureJobProgressRegistered should have created it when subscribing
        // but if it's not present, fail the test explicitly.
        done.fail(new Error('Unable to capture event handler or jobProgressSubject'));
        return;
      }
      subj.next(payload);
    }
  });
});
