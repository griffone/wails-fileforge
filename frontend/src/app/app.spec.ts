import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { EMPTY } from 'rxjs';

import { App } from './app';
import { GlobalDropService } from './services/global-drop.service';
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
