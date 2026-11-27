import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { ToolWindowResponse } from '../../services/wails';
import { WindowLauncher } from '../../services/window-launcher.service';
import { OpenWindowAction } from './open-window-action';

@Component({ template: '' })
class BlankPage {}

describe('OpenWindowAction', () => {
  let fixture: ComponentFixture<OpenWindowAction>;
  let launcher: jasmine.SpyObj<WindowLauncher>;
  let router: Router;

  function stubResponse(route: string): ToolWindowResponse {
    return {
      success: true,
      message: 'opened',
      route,
      windowName: `tool:${route.replace(/^\//, '')}`,
      created: true,
    };
  }

  beforeEach(async () => {
    launcher = jasmine.createSpyObj<WindowLauncher>('WindowLauncher', ['open']);
    launcher.open.and.callFake((route: string) => Promise.resolve(stubResponse(route)));

    await TestBed.configureTestingModule({
      imports: [OpenWindowAction],
      providers: [
        provideRouter([
          { path: 'image-crop', component: BlankPage },
          { path: 'settings', component: BlankPage },
        ]),
        { provide: WindowLauncher, useValue: launcher },
      ],
    }).compileComponents();

    router = TestBed.inject(Router);
  });

  async function renderAt(url: string): Promise<void> {
    await router.navigateByUrl(url);
    fixture = TestBed.createComponent(OpenWindowAction);
    fixture.detectChanges();
  }

  function button(): HTMLButtonElement | null {
    return fixture.nativeElement.querySelector('.open-window');
  }

  it('opens the tool of the current route', async () => {
    await renderAt('/image-crop');

    button()?.click();
    await fixture.whenStable();

    expect(launcher.open).toHaveBeenCalledOnceWith('/image-crop');
  });

  it('renders the action on a tool page', async () => {
    await renderAt('/image-crop');

    expect(button()).not.toBeNull();
    expect(button()?.textContent).toContain('Open in new window');
  });

  it('renders nothing outside a tool page', async () => {
    await renderAt('/settings');

    expect(button()).toBeNull();
    expect(fixture.nativeElement.textContent?.trim()).toBe('');
  });

  it('disables the button while the window request is in flight', async () => {
    await renderAt('/image-crop');

    let release: (response: ToolWindowResponse) => void = () => undefined;
    launcher.open.and.returnValue(
      new Promise<ToolWindowResponse>((resolve) => {
        release = resolve;
      }),
    );

    button()?.click();
    fixture.detectChanges();
    expect(button()?.disabled).toBeTrue();

    release(stubResponse('/image-crop'));
    await fixture.whenStable();
    fixture.detectChanges();

    expect(button()?.disabled).toBeFalse();
    expect(launcher.open).toHaveBeenCalledOnceWith('/image-crop');
  });
});
