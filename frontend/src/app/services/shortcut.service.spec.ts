import { PageShortcutTarget, ShortcutService } from './shortcut.service';

function target(
  answers: { openFiles?: boolean; run?: boolean } = {},
): PageShortcutTarget & {
  requestOpenFiles: jasmine.Spy;
  requestRun: jasmine.Spy;
} {
  return {
    toolId: 'tool.test',
    requestOpenFiles: jasmine
      .createSpy('requestOpenFiles')
      .and.returnValue(answers.openFiles ?? true),
    requestRun: jasmine.createSpy('requestRun').and.returnValue(answers.run ?? true),
  };
}

function keydown(
  key: string,
  modifiers: Partial<{
    ctrlKey: boolean;
    metaKey: boolean;
    shiftKey: boolean;
    altKey: boolean;
  }> = {},
): KeyboardEvent {
  return new KeyboardEvent('keydown', {
    key,
    cancelable: true,
    ...modifiers,
  });
}

describe('ShortcutService', () => {
  let service: ShortcutService;

  beforeEach(() => {
    service = new ShortcutService();
  });

  it('routes Ctrl+Enter and Ctrl+O to the active page', () => {
    const page = target();
    service.registerPage(page);

    const run = keydown('Enter', { ctrlKey: true });
    expect(service.handlePageShortcut(run)).toBeTrue();
    expect(page.requestRun).toHaveBeenCalledTimes(1);
    expect(run.defaultPrevented).toBeTrue();

    const open = keydown('o', { metaKey: true });
    expect(service.handlePageShortcut(open)).toBeTrue();
    expect(page.requestOpenFiles).toHaveBeenCalledTimes(1);
    expect(open.defaultPrevented).toBeTrue();
  });

  it('does not consume the key when the page cannot honour it', () => {
    const page = target({ run: false });
    service.registerPage(page);

    const event = keydown('Enter', { ctrlKey: true });
    expect(service.handlePageShortcut(event)).toBeFalse();
    expect(event.defaultPrevented).toBeFalse();
  });

  it('ignores page shortcuts when no page is active', () => {
    const event = keydown('Enter', { ctrlKey: true });
    expect(
      service.handlePageShortcut(event, document),
    ).toBeFalse();
    expect(event.defaultPrevented).toBeFalse();
  });

  it('ignores the shortcut while focus is in a form control', () => {
    const page = target();
    service.registerPage(page);
    const input = document.createElement('input');
    document.body.appendChild(input);

    let handled: boolean | null = null;
    input.addEventListener('keydown', (event) => {
      handled = service.handlePageShortcut(event as KeyboardEvent);
    });
    input.dispatchEvent(keydown('Enter', { ctrlKey: true }));

    expect(handled).toBeFalse();
    expect(page.requestRun).not.toHaveBeenCalled();
    input.remove();
  });

  it('ignores the shortcut while a dialog is open', () => {
    const page = target();
    service.registerPage(page);
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.appendChild(dialog);

    expect(
      service.handlePageShortcut(keydown('Enter', { ctrlKey: true })),
    ).toBeFalse();
    expect(page.requestRun).not.toHaveBeenCalled();
    dialog.remove();
  });

  it('ignores a default-prevented event', () => {
    const page = target();
    service.registerPage(page);
    const event = keydown('Enter', { ctrlKey: true });
    event.preventDefault();

    expect(service.handlePageShortcut(event, document)).toBeFalse();
    expect(page.requestRun).not.toHaveBeenCalled();
  });

  it('ignores key repeat for non-idempotent page actions', () => {
    const page = target();
    service.registerPage(page);

    const event = new KeyboardEvent('keydown', {
      key: 'Enter',
      ctrlKey: true,
      repeat: true,
      cancelable: true,
    });

    expect(service.handlePageShortcut(event, document)).toBeFalse();
    expect(page.requestRun).not.toHaveBeenCalled();
  });

  it('keeps the most recently registered page and never lets a stale one clear it', () => {
    const first = target();
    const second = target();

    service.registerPage(first);
    service.registerPage(second);
    expect(service.activePageTarget()).toBe(second);

    service.unregisterPage(first);
    expect(service.activePageTarget()).toBe(second);

    service.unregisterPage(second);
    expect(service.activePageTarget()).toBeNull();
  });
});
