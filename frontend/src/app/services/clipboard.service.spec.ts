import { TestBed } from '@angular/core/testing';

import {
  ClipboardService,
  RUNTIME_CLIPBOARD,
  RuntimeClipboard,
} from './clipboard.service';

describe('ClipboardService', () => {
  let runtime: jasmine.SpyObj<RuntimeClipboard>;
  let service: ClipboardService;
  const originalClipboard = Object.getOwnPropertyDescriptor(
    navigator,
    'clipboard',
  );

  function configure(): ClipboardService {
    TestBed.configureTestingModule({
      providers: [{ provide: RUNTIME_CLIPBOARD, useValue: runtime }],
    });

    return TestBed.inject(ClipboardService);
  }

  function setBrowserClipboard(value: unknown): void {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value,
    });
  }

  beforeEach(() => {
    runtime = jasmine.createSpyObj<RuntimeClipboard>('RuntimeClipboard', [
      'SetText',
    ]);
    runtime.SetText.and.resolveTo();
    service = configure();
  });

  afterEach(() => {
    TestBed.resetTestingModule();
    if (originalClipboard) {
      Object.defineProperty(navigator, 'clipboard', originalClipboard);
    } else {
      delete (navigator as unknown as { clipboard?: unknown }).clipboard;
    }
  });

  it('copies through the Wails runtime first', async () => {
    const browserWriteText = jasmine.createSpy('browserWriteText').and.resolveTo();
    setBrowserClipboard({ writeText: browserWriteText });

    await expectAsync(service.writeText('/tmp/photos/a.bin')).toBeResolvedTo(
      true,
    );

    expect(runtime.SetText).toHaveBeenCalledWith('/tmp/photos/a.bin');
    expect(browserWriteText).not.toHaveBeenCalled();
  });

  it('falls back to navigator.clipboard when the runtime fails', async () => {
    runtime.SetText.and.rejectWith(new Error('runtime unavailable'));
    const browserWriteText = jasmine.createSpy('browserWriteText').and.resolveTo();
    setBrowserClipboard({ writeText: browserWriteText });

    await expectAsync(service.writeText('/tmp/photos/a.bin')).toBeResolvedTo(
      true,
    );

    expect(runtime.SetText).toHaveBeenCalledWith('/tmp/photos/a.bin');
    expect(browserWriteText).toHaveBeenCalledWith('/tmp/photos/a.bin');
  });

  it('returns false when the runtime and the browser API both fail', async () => {
    runtime.SetText.and.rejectWith(new Error('runtime unavailable'));
    const browserWriteText = jasmine
      .createSpy('browserWriteText')
      .and.rejectWith(new Error('clipboard unavailable'));
    setBrowserClipboard({ writeText: browserWriteText });

    await expectAsync(service.writeText('/tmp/photos/a.bin')).toBeResolvedTo(
      false,
    );

    expect(browserWriteText).toHaveBeenCalled();
  });

  it('returns false when the runtime fails and no browser API exists', async () => {
    runtime.SetText.and.rejectWith(new Error('runtime unavailable'));
    setBrowserClipboard(undefined);

    await expectAsync(service.writeText('/tmp/photos/a.bin')).toBeResolvedTo(
      false,
    );
  });

  it('rejects empty text without touching either clipboard', async () => {
    const browserWriteText = jasmine.createSpy('browserWriteText').and.resolveTo();
    setBrowserClipboard({ writeText: browserWriteText });

    await expectAsync(service.writeText('')).toBeResolvedTo(false);

    expect(runtime.SetText).not.toHaveBeenCalled();
    expect(browserWriteText).not.toHaveBeenCalled();
  });
});
