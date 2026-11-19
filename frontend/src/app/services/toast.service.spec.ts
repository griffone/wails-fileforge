import { fakeAsync, TestBed, tick } from '@angular/core/testing';

import { ToastService } from './toast.service';

describe('ToastService', () => {
  let service: ToastService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(ToastService);
  });

  it('pushes toasts newest first', () => {
    service.push({ tone: 'success', title: 'First', body: 'one' }, 0);
    service.push({ tone: 'info', title: 'Second', body: 'two' }, 0);

    expect(service.toasts().map((toast) => toast.title)).toEqual([
      'Second',
      'First',
    ]);
    expect(service.toasts()[0].body).toBe('two');
  });

  it('caps the queue at three and drops the oldest toast', () => {
    for (const title of ['A', 'B', 'C', 'D']) {
      service.push({ tone: 'info', title, body: '' }, 0);
    }

    expect(service.toasts().length).toBe(3);
    expect(service.toasts().map((toast) => toast.title)).toEqual([
      'D',
      'C',
      'B',
    ]);
  });

  it('assigns increasing ids', () => {
    const first = service.push({ tone: 'info', title: 'A', body: '' }, 0);
    const second = service.push({ tone: 'info', title: 'B', body: '' }, 0);

    expect(second).toBeGreaterThan(first);
  });

  it('dismisses a specific toast', () => {
    const keep = service.push({ tone: 'info', title: 'Keep', body: '' }, 0);
    const drop = service.push({ tone: 'info', title: 'Drop', body: '' }, 0);

    service.dismiss(drop);

    expect(service.toasts().map((toast) => toast.id)).toEqual([keep]);
  });

  it('auto-dismisses after the ttl', fakeAsync(() => {
    service.push({ tone: 'success', title: 'Done', body: 'ok' }, 5000);

    expect(service.toasts().length).toBe(1);

    tick(4999);
    expect(service.toasts().length).toBe(1);

    tick(1);
    expect(service.toasts().length).toBe(0);
  }));

  it('clears every toast', () => {
    service.push({ tone: 'info', title: 'A', body: '' }, 0);
    service.push({ tone: 'info', title: 'B', body: '' }, 0);

    service.clear();

    expect(service.toasts()).toEqual([]);
  });
});
