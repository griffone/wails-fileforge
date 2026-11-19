import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ToastHost } from './toast-host';
import { ToastService } from '../../services/toast.service';

describe('ToastHost', () => {
  let fixture: ComponentFixture<ToastHost>;
  let toastService: ToastService;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ToastHost],
    }).compileComponents();

    toastService = TestBed.inject(ToastService);
    toastService.clear();
    fixture = TestBed.createComponent(ToastHost);
    fixture.detectChanges();
  });

  afterEach(() => {
    toastService.clear();
    fixture.destroy();
  });

  function push(
    overrides: Partial<Parameters<ToastService['push']>[0]> = {},
    ttlMs = 0,
  ): void {
    toastService.push(
      {
        tone: 'success',
        title: 'Merge finished',
        body: '3 files ready',
        ...overrides,
      },
      ttlMs,
    );
    fixture.detectChanges();
  }

  function text(selector: string): string {
    return (
      (fixture.nativeElement.querySelector(selector) as HTMLElement | null)
        ?.textContent?.trim() ?? ''
    );
  }

  it('renders the toast title and body', () => {
    push();

    expect(
      (fixture.nativeElement.querySelector('.toast-host') as HTMLElement).getAttribute(
        'role',
      ),
    ).toBe('status');
    expect(text('.toast-title')).toBe('Merge finished');
    expect(text('.toast-body')).toBe('3 files ready');
  });

  it('runs the action and dismisses the toast', () => {
    const action = jasmine.createSpy('action');
    push({ actionLabel: 'View activity', action });

    (fixture.nativeElement.querySelector('.toast-action') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(action).toHaveBeenCalled();
    expect(toastService.toasts().length).toBe(0);
    expect(fixture.nativeElement.querySelector('.toast')).toBeNull();
  });

  it('dismisses from the close button', () => {
    push();

    (fixture.nativeElement.querySelector('.toast-close') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(toastService.toasts().length).toBe(0);
    expect(fixture.nativeElement.querySelector('.toast')).toBeNull();
  });
});
