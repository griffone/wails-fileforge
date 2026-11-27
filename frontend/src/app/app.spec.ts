import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { EMPTY } from 'rxjs';

import { App } from './app';
import { findShortcutById, formatShortcutLabel } from './config/shortcuts';
import { GlobalDropService } from './services/global-drop.service';
import { ShortcutService } from './services/shortcut.service';
import { ToolchainStatus, Wails } from './services/wails';

const TOOLCHAIN_FALLBACK: ToolchainStatus = {
  success: false,
  message: 'Toolchain IPC unavailable in the test shell.',
  ready: false,
  installing: false,
  percent: 0,
  storageDir: '',
  components: [],
  model: 'small',
  ocrLanguages: [],
};

function createWailsStub(): jasmine.SpyObj<Wails> {
  const stub = jasmine.createSpyObj<Wails>('Wails', [
    'isRuntimeAvailable',
    'getToolchainStatus',
    'getToolchainInstallState',
    'startToolchainInstall',
    'cancelToolchainInstall',
  ]);
  stub.isRuntimeAvailable.and.returnValue(false);
  stub.getToolchainStatus.and.resolveTo(TOOLCHAIN_FALLBACK);
  stub.getToolchainInstallState.and.resolveTo(TOOLCHAIN_FALLBACK);
  stub.startToolchainInstall.and.resolveTo(TOOLCHAIN_FALLBACK);
  stub.cancelToolchainInstall.and.resolveTo(TOOLCHAIN_FALLBACK);
  (stub as unknown as { toolchainInstall$: unknown }).toolchainInstall$ = EMPTY;
  return stub;
}

describe('App', () => {
  let globalDrop: GlobalDropService;

  beforeEach(async () => {
    localStorage.removeItem('ff-recent-tools');

    await TestBed.configureTestingModule({
      imports: [App],
      providers: [
        provideRouter([]),
        { provide: Wails, useValue: createWailsStub() },
      ],
    }).compileComponents();

    globalDrop = TestBed.inject(GlobalDropService);
  });

  it('creates the app shell', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    expect(fixture.componentInstance).toBeTruthy();
    expect(fixture.nativeElement.querySelector('.app-frame')).toBeTruthy();
  });

  it('renders the FileForge brand block', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('.rail-brand-wordmark')?.textContent).toContain('FILEFORGE');
    expect(compiled.querySelector('.rail-brand-mark')).toBeTruthy();
  });

  it('renders the Audio group in the rail', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const groups = Array.from(
      fixture.nativeElement.querySelectorAll('.rail-group'),
    ) as HTMLElement[];
    const audio = groups.find(
      (group) =>
        group.querySelector('.rail-group-label')?.textContent?.trim() ===
        'Audio',
    );

    expect(audio).toBeTruthy();
    const labels = Array.from(
      audio?.querySelectorAll('.nav-item-label') ?? [],
    ).map((label) => label.textContent?.trim());
    expect(labels).toEqual(['Extract', 'Convert', 'Normalize']);
  });

  it('opens the command palette with Ctrl+K', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeTruthy();
  });

  it('opens the command palette with Meta+K', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'K', metaKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeTruthy();
  });

  it('closes the command palette with Escape', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.palette')).toBeTruthy();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeNull();
  });

  it('shows the registry label in the palette trigger', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const shortcuts = TestBed.inject(ShortcutService);
    const expected = formatShortcutLabel(
      findShortcutById('palette.open'),
      shortcuts.platform,
    );

    expect(
      (fixture.nativeElement.querySelector('.palette-trigger-kbd') as HTMLElement)
        .textContent?.trim(),
    ).toBe(expected);
  });

  it('opens settings with Ctrl+,', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();
    const navigate = spyOn(TestBed.inject(Router), 'navigateByUrl').and.resolveTo(
      true,
    );

    document.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: ',',
        ctrlKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );

    expect(navigate).toHaveBeenCalledWith('/settings');
  });

  it('opens the shortcut sheet with ?', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: '?',
        shiftKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.shortcut-sheet')).toBeTruthy();
  });

  it('closes the shortcut sheet with Escape', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: '?', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.shortcut-sheet')).toBeTruthy();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.shortcut-sheet')).toBeNull();
  });

  it('ignores shell shortcuts typed into a form field', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();
    const input = document.createElement('input');
    document.body.appendChild(input);

    input.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'k',
        ctrlKey: true,
        bubbles: true,
        cancelable: true,
      }),
    );
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeNull();
    input.remove();
  });

  it('ignores shell shortcuts while a dialog is open', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'k', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    const navigate = spyOn(TestBed.inject(Router), 'navigateByUrl');

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: ',', ctrlKey: true, bubbles: true, cancelable: true }),
    );
    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: '?', shiftKey: true, bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(navigate).not.toHaveBeenCalled();
    expect(fixture.nativeElement.querySelectorAll('[role="dialog"]').length).toBe(
      1,
    );
  });

  it('ignores a default-prevented shell shortcut', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const event = new KeyboardEvent('keydown', {
      key: 'k',
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    event.preventDefault();
    document.dispatchEvent(event);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.palette')).toBeNull();
  });

  it('routes page shortcuts to the registered controller', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const shortcuts = TestBed.inject(ShortcutService);
    const page = {
      toolId: 'tool.test',
      requestOpenFiles: jasmine.createSpy('requestOpenFiles').and.returnValue(true),
      requestRun: jasmine.createSpy('requestRun').and.returnValue(true),
    };
    shortcuts.registerPage(page);

    const event = new KeyboardEvent('keydown', {
      key: 'Enter',
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    document.dispatchEvent(event);

    expect(page.requestRun).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBeTrue();
    shortcuts.unregisterPage(page);
  });

  it('opens the activity drawer from the topbar trigger', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    const trigger = fixture.nativeElement.querySelector(
      '.activity-trigger',
    ) as HTMLButtonElement;
    trigger.click();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.activity-drawer')).toBeTruthy();
  });

  it('ignores a shell drop event owned by a local file drop target', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    fixture.componentInstance.handleNativeDrop({
      data: {
        droppedFiles: ['/tmp/photo.png'],
        dropTargetDetails: { id: 'ff-drop-2' },
      },
    });
    fixture.detectChanges();

    expect(globalDrop.open()).toBeFalse();
    expect(fixture.nativeElement.querySelector('.drop-backdrop')).toBeNull();
  });

  it('opens the global drop overlay for an app shell drop', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    fixture.componentInstance.handleNativeDrop({
      data: {
        droppedFiles: ['/tmp/photo.png'],
        dropTargetDetails: { id: 'app-shell-drop' },
      },
    });
    fixture.detectChanges();

    expect(globalDrop.open()).toBeTrue();
    expect(fixture.nativeElement.querySelector('.drop-backdrop')).toBeTruthy();
    expect(fixture.nativeElement.querySelector('.drop-card-name')?.textContent).toContain(
      'Image Convert',
    );
  });

  it('opens the global drop overlay for a drop without a target', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    fixture.componentInstance.handleNativeDrop({ data: { files: ['/tmp/notes.md'] } });
    fixture.detectChanges();

    expect(globalDrop.open()).toBeTrue();
    expect(fixture.nativeElement.querySelector('.drop-card-name')?.textContent).toContain(
      'Markdown → PDF',
    );
  });

  it('ignores malformed shell drop payloads', () => {
    const fixture = TestBed.createComponent(App);
    fixture.detectChanges();

    fixture.componentInstance.handleNativeDrop({ data: { files: [42, ''] } });
    fixture.componentInstance.handleNativeDrop(null);
    fixture.detectChanges();

    expect(globalDrop.open()).toBeFalse();
  });
});
