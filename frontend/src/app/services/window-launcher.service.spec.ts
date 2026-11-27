import { TestBed } from '@angular/core/testing';

import { I18nService } from './i18n.service';
import { ToastService } from './toast.service';
import { Wails } from './wails';
import { WindowLauncher } from './window-launcher.service';

/** Mirrors the backend contract: one window per route, reuse focuses it. */
function createWindowBackend(): jasmine.SpyObj<Wails> {
  const open = new Set<string>();
  const stub = jasmine.createSpyObj<Wails>('Wails', ['openToolWindow']);
  stub.openToolWindow.and.callFake((route: string) => {
    const created = !open.has(route);
    open.add(route);
    return Promise.resolve({
      success: true,
      message: created ? 'opened' : 'focused',
      route,
      windowName: `tool:${route.replace(/^\//, '')}`,
      created,
    });
  });
  return stub;
}

describe('WindowLauncher', () => {
  let wails: jasmine.SpyObj<Wails>;

  beforeEach(() => {
    localStorage.removeItem('ff-preferences');
    wails = createWindowBackend();

    TestBed.configureTestingModule({
      providers: [{ provide: Wails, useValue: wails }],
    });
  });

  it('forwards the route to the window service', async () => {
    const launcher = TestBed.inject(WindowLauncher);

    const response = await launcher.open('/image-crop');

    expect(wails.openToolWindow).toHaveBeenCalledOnceWith('/image-crop');
    expect(response.success).toBeTrue();
    expect(response.created).toBeTrue();
  });

  it('reuses the existing window for the same route', async () => {
    const launcher = TestBed.inject(WindowLauncher);

    const first = await launcher.open('/image-crop');
    const second = await launcher.open('/image-crop');

    expect(first.created).toBeTrue();
    expect(second.created).toBeFalse();
    expect(second.windowName).toBe(first.windowName);
    expect(wails.openToolWindow).toHaveBeenCalledTimes(2);
  });

  it('shows a toast when the window cannot be opened', async () => {
    wails.openToolWindow.and.resolveTo({
      success: false,
      message: 'open tool window: "/settings" is not an openable tool page',
      route: '/settings',
      windowName: '',
      created: false,
    });
    const launcher = TestBed.inject(WindowLauncher);
    const toast = TestBed.inject(ToastService);
    const i18n = TestBed.inject(I18nService);

    const response = await launcher.open('/settings');

    expect(response.success).toBeFalse();
    expect(toast.toasts().length).toBe(1);
    expect(toast.toasts()[0].tone).toBe('danger');
    expect(toast.toasts()[0].body).toContain('is not an openable tool page');
    expect(toast.toasts()[0].title).toBe(i18n.t('window.open.failedTitle'));
  });
});
