import {
  ComponentFixture,
  fakeAsync,
  flushMicrotasks,
  TestBed,
} from '@angular/core/testing';

import { Settings } from './settings';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import { JobResultV1, Wails } from '../../services/wails';

const ONBOARDING_KEY = 'ff-onboarding-dismissed';

describe('Settings', () => {
  let fixture: ComponentFixture<Settings>;
  let component: Settings;
  let wailsSpy: jasmine.SpyObj<Wails>;

  function element(): HTMLElement {
    return fixture.nativeElement as HTMLElement;
  }

  function button(selector: string): HTMLButtonElement {
    const found = element().querySelector<HTMLButtonElement>(selector);
    if (!found) {
      throw new Error(`Missing button: ${selector}`);
    }
    return found;
  }

  function storedPreferences(): unknown {
    return JSON.parse(localStorage.getItem(PREFERENCES_STORAGE_KEY) ?? '{}');
  }

  function sampleJob(): JobResultV1 {
    return {
      jobId: 'job-settings-1',
      success: true,
      message: 'done',
      toolId: 'tool.image.convert',
      status: 'success',
      progress: {
        current: 1,
        total: 1,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath: '/tmp/photo.png',
          outputPath: '/tmp/photo.webp',
          outputs: ['/tmp/photo.webp'],
          success: true,
          message: 'conversion successful',
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
  }

  function resetStorage(): void {
    try {
      localStorage.removeItem(PREFERENCES_STORAGE_KEY);
      localStorage.removeItem('ff-recent-conversions');
      localStorage.removeItem('ff-theme');
      localStorage.removeItem(ONBOARDING_KEY);
    } catch {
      // Storage is available in Karma; guard keeps the spec resilient.
    }
    document.documentElement.removeAttribute('data-theme');
  }

  beforeEach(async () => {
    resetStorage();

    wailsSpy = jasmine.createSpyObj<Wails>('Wails', ['openDirectoryDialog']);
    wailsSpy.openDirectoryDialog.and.resolveTo('');

    await TestBed.configureTestingModule({
      imports: [Settings],
      providers: [{ provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(Settings);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
    resetStorage();
  });

  it('defaults to keeping outputs next to the original', () => {
    expect(component.defaultOutputDir()).toBe('');
    expect(button('.settings-next-to-original').getAttribute('aria-pressed')).toBe(
      'true',
    );
    expect(button('.settings-choose-folder').getAttribute('aria-pressed')).toBe(
      'false',
    );
    expect(element().querySelector('.settings-path')).toBeNull();
  });

  it('persists a chosen folder and clears it back to original folders', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('  /tmp/exports  ');

    button('.settings-choose-folder').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.defaultOutputDir()).toBe('/tmp/exports');
    expect(storedPreferences()).toEqual({ defaultOutputDir: '/tmp/exports' });
    expect(
      element().querySelector('.settings-path')?.textContent,
    ).toContain('/tmp/exports');
    expect(button('.settings-choose-folder').getAttribute('aria-pressed')).toBe(
      'true',
    );

    button('.settings-reset-output').click();
    fixture.detectChanges();

    expect(component.defaultOutputDir()).toBe('');
    expect(storedPreferences()).toEqual({ defaultOutputDir: '' });
    expect(element().querySelector('.settings-path')).toBeNull();
    expect(button('.settings-next-to-original').getAttribute('aria-pressed')).toBe(
      'true',
    );
  }));

  it('keeps the saved folder when the picker is cancelled', fakeAsync(() => {
    wailsSpy.openDirectoryDialog.and.resolveTo('/tmp/exports');
    button('.settings-choose-folder').click();
    flushMicrotasks();

    wailsSpy.openDirectoryDialog.and.resolveTo('');
    button('.settings-choose-folder').click();
    flushMicrotasks();
    fixture.detectChanges();

    expect(component.defaultOutputDir()).toBe('/tmp/exports');
  }));

  it('switches the theme and reflects it with aria-pressed', () => {
    button('.settings-theme-dark').click();
    fixture.detectChanges();

    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(button('.settings-theme-dark').getAttribute('aria-pressed')).toBe(
      'true',
    );
    expect(button('.settings-theme-light').getAttribute('aria-pressed')).toBe(
      'false',
    );

    button('.settings-theme-light').click();
    fixture.detectChanges();

    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    expect(button('.settings-theme-light').getAttribute('aria-pressed')).toBe(
      'true',
    );
  });

  it('shows the saved conversion count and clears the history', () => {
    const recent = TestBed.inject(RecentConversionsService);
    recent.record(sampleJob());
    fixture.detectChanges();

    expect(component.recentCount()).toBe(1);
    expect(
      element().querySelector('.settings-history-meta')?.textContent,
    ).toContain('1');

    button('.settings-clear-history').click();
    fixture.detectChanges();

    expect(component.recentCount()).toBe(0);
    expect(recent.recent().length).toBe(0);
    expect(element().textContent).toContain('Recent conversions cleared.');
  });

  it('resets the Home onboarding dismissal', () => {
    localStorage.setItem(ONBOARDING_KEY, '1');

    button('.settings-reset-onboarding').click();
    fixture.detectChanges();

    expect(localStorage.getItem(ONBOARDING_KEY)).toBeNull();
    expect(element().textContent).toContain(
      'Onboarding will show again on Home.',
    );
  });
});
