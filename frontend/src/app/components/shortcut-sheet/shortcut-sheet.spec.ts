import { ComponentFixture, TestBed } from '@angular/core/testing';

import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { ShortcutService } from '../../services/shortcut.service';
import { ShortcutSheet } from './shortcut-sheet';

describe('ShortcutSheet', () => {
  let fixture: ComponentFixture<ShortcutSheet>;
  let component: ShortcutSheet;

  async function createSheet(
    platform: 'mac' | 'windows' = 'windows',
  ): Promise<void> {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    TestBed.resetTestingModule();

    await TestBed.configureTestingModule({
      imports: [ShortcutSheet],
      providers: [{ provide: ShortcutService, useValue: { platform } }],
    }).compileComponents();

    fixture = TestBed.createComponent(ShortcutSheet);
    component = fixture.componentInstance;
  }

  beforeEach(async () => {
    await createSheet();
  });

  afterEach(() => {
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    document.documentElement.lang = 'en';
  });

  function openSheet(): void {
    fixture.componentRef.setInput('open', true);
    fixture.detectChanges();
  }

  function groupLabels(): string[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll(
        '.shortcut-group-label',
      ) as NodeListOf<HTMLElement>,
    ).map((label) => label.textContent?.trim() ?? '');
  }

  function keyLabels(): string[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll(
        '.shortcut-key',
      ) as NodeListOf<HTMLElement>,
    ).map((key) => key.textContent?.trim() ?? '');
  }

  it('stays closed until it is opened', () => {
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[role="dialog"]')).toBeNull();
  });

  it('lists every registered shortcut grouped by scope', () => {
    openSheet();

    expect(groupLabels()).toEqual(['App-wide', 'Tool pages', 'Dialogs']);
    expect(fixture.nativeElement.querySelectorAll('.shortcut-row').length).toBe(
      6,
    );
    expect(
      fixture.nativeElement.querySelector('.shortcut-sheet')?.textContent,
    ).toContain('Command palette');
    expect(
      fixture.nativeElement.querySelector('.shortcut-sheet')?.textContent,
    ).toContain('Open files');
  });

  it('focuses the close button and traps Tab inside the dialog', () => {
    openSheet();
    const close = fixture.nativeElement.querySelector(
      '.sheet-close',
    ) as HTMLButtonElement;

    expect(document.activeElement).toBe(close);

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }),
    );

    expect(document.activeElement).toBe(close);
    expect(close.closest('[role="dialog"]')).toBeTruthy();
  });

  it('closes on Escape from the registry binding', () => {
    const closed = jasmine.createSpy('closed');
    component.closed.subscribe(closed);
    openSheet();

    const event = new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    });
    document.dispatchEvent(event);

    expect(closed).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBeTrue();
  });

  it('restores focus to the element that opened it', () => {
    const opener = document.createElement('button');
    document.body.appendChild(opener);
    opener.focus();
    expect(document.activeElement).toBe(opener);

    openSheet();
    const close = fixture.nativeElement.querySelector(
      '.sheet-close',
    ) as HTMLButtonElement;
    expect(document.activeElement).toBe(close);

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(document.activeElement).toBe(opener);
    opener.remove();
  });

  it('renders macOS modifier labels', async () => {
    await createSheet('mac');
    openSheet();

    expect(keyLabels()[0]).toBe('⌘K');
    expect(keyLabels()[4]).toBe('⌘↵');
  });

  it('renders Ctrl-style labels on Windows and Linux', () => {
    openSheet();

    expect(keyLabels()[0]).toBe('Ctrl+K');
    expect(keyLabels()[4]).toBe('Ctrl+Enter');
  });

  it('renders Spanish copy', () => {
    TestBed.inject(I18nService).setLocale('es');
    fixture.detectChanges();
    openSheet();

    expect(groupLabels()).toEqual([
      'Toda la aplicación',
      'Páginas de herramientas',
      'Diálogos',
    ]);
    expect(
      fixture.nativeElement.querySelector('.shortcut-sheet')?.textContent,
    ).toContain('Atajos de teclado');
    expect(
      fixture.nativeElement.querySelector('.shortcut-sheet')?.textContent,
    ).toContain('Abrir archivos');
  });
});
