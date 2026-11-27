import { en, es } from '../i18n';
import {
  ResolvedShortcutBinding,
  SHORTCUTS,
  ShortcutDefinition,
  ShortcutPlatform,
  detectShortcutPlatform,
  findShortcut,
  findShortcutById,
  formatShortcutLabel,
  isDialogOpen,
  isEditableShortcutTarget,
  resolveShortcutBinding,
  shortcutGroups,
} from './shortcuts';

function eventFor(
  definition: ShortcutDefinition,
  platform: ShortcutPlatform = 'windows',
  overrides: Partial<{
    key: string;
    primary: boolean;
    shift: boolean;
    alt: boolean;
  }> = {},
): KeyboardEvent {
  const binding: ResolvedShortcutBinding = resolveShortcutBinding(
    definition,
    platform,
  );
  const primary = overrides.primary ?? binding.primary;
  return new KeyboardEvent('keydown', {
    key: overrides.key ?? binding.key,
    ctrlKey: primary,
    metaKey: false,
    shiftKey: overrides.shift ?? binding.shift ?? false,
    altKey: overrides.alt ?? binding.alt,
    cancelable: true,
  });
}

describe('shortcut registry', () => {
  it('gives every entry an id, a binding and translated copy', () => {
    for (const definition of SHORTCUTS) {
      expect(definition.id).withContext('id').toBeTruthy();
      expect(definition.binding.key).withContext(definition.id).toBeTruthy();
      expect(en[definition.labelKey])
        .withContext(`${definition.id} label`)
        .toBeTruthy();
      expect(es[definition.labelKey])
        .withContext(`${definition.id} label es`)
        .toBeTruthy();
      expect(en[definition.descriptionKey])
        .withContext(`${definition.id} description`)
        .toBeTruthy();
      expect(es[definition.descriptionKey])
        .withContext(`${definition.id} description es`)
        .toBeTruthy();
    }
  });

  it('has no duplicate ids', () => {
    const ids = SHORTCUTS.map((definition) => definition.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('has no two shortcuts with the same binding in one scope', () => {
    const seen = new Set<string>();
    for (const definition of SHORTCUTS) {
      const binding = resolveShortcutBinding(definition, 'windows');
      const signature = `${definition.scope}:${binding.key}:${binding.primary}:${binding.shift}:${binding.alt}`;
      expect(seen.has(signature))
        .withContext(`duplicate binding for ${definition.id}`)
        .toBeFalse();
      seen.add(signature);
    }
  });

  it('resolves every registry entry through the dispatcher', () => {
    for (const definition of SHORTCUTS) {
      const match = findShortcut(eventFor(definition), 'windows');
      expect(match?.id).withContext(definition.id).toBe(definition.id);
    }
  });

  it('labels modifiers per platform', () => {
    const palette = findShortcutById('palette.open');
    const settings = findShortcutById('settings.open');
    const run = findShortcutById('page.run');
    const sheet = findShortcutById('shortcutSheet.open');

    expect(formatShortcutLabel(palette, 'mac')).toBe('⌘K');
    expect(formatShortcutLabel(palette, 'windows')).toBe('Ctrl+K');
    expect(formatShortcutLabel(settings, 'mac')).toBe('⌘,');
    expect(formatShortcutLabel(settings, 'windows')).toBe('Ctrl+,');
    expect(formatShortcutLabel(run, 'mac')).toBe('⌘↵');
    expect(formatShortcutLabel(run, 'windows')).toBe('Ctrl+Enter');
    expect(formatShortcutLabel(sheet, 'mac')).toBe('?');
    expect(formatShortcutLabel(sheet, 'windows')).toBe('?');
  });

  it('accepts ? whether or not Shift is reported', () => {
    const sheet = findShortcutById('shortcutSheet.open');
    expect(findShortcut(eventFor(sheet, 'windows'), 'windows')?.id).toBe(
      'shortcutSheet.open',
    );
    expect(
      findShortcut(eventFor(sheet, 'windows', { shift: true }), 'windows')?.id,
    ).toBe('shortcutSheet.open');
  });

  it('rejects an extra Shift on a letter binding', () => {
    const palette = findShortcutById('palette.open');
    expect(
      findShortcut(eventFor(palette, 'windows', { shift: true }), 'windows'),
    ).toBeNull();
  });

  it('groups the sheet by scope in a stable order', () => {
    const groups = shortcutGroups();
    expect(groups.map((group) => group.scope)).toEqual([
      'global',
      'page',
      'dialog',
    ]);
    expect(groups[0].entries.map((entry) => entry.id)).toEqual([
      'palette.open',
      'settings.open',
      'shortcutSheet.open',
    ]);
    expect(groups[1].entries.map((entry) => entry.id)).toEqual([
      'page.openFiles',
      'page.run',
    ]);
    expect(groups[2].entries.map((entry) => entry.id)).toEqual(['dialog.close']);
  });

  it('detects macOS from the navigator', () => {
    expect(
      detectShortcutPlatform({
        platform: 'MacIntel',
        userAgent: 'Mozilla/5.0 (Macintosh)',
      }),
    ).toBe('mac');
    expect(
      detectShortcutPlatform({ platform: 'Win32', userAgent: 'Windows NT' }),
    ).toBe('windows');
    expect(detectShortcutPlatform(null)).toBe('windows');
  });

  it('treats form controls and contenteditable as editable', () => {
    expect(isEditableShortcutTarget(document.createElement('input'))).toBeTrue();
    expect(
      isEditableShortcutTarget(document.createElement('textarea')),
    ).toBeTrue();
    expect(isEditableShortcutTarget(document.createElement('select'))).toBeTrue();
    expect(isEditableShortcutTarget(document.createElement('button'))).toBeFalse();

    const editable = document.createElement('div');
    editable.setAttribute('contenteditable', 'true');
    const child = document.createElement('span');
    editable.appendChild(child);
    expect(isEditableShortcutTarget(child)).toBeTrue();
  });

  it('sees an open role=dialog', () => {
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    document.body.appendChild(dialog);

    expect(isDialogOpen(document)).toBeTrue();
    dialog.remove();
    expect(isDialogOpen(document)).toBeFalse();
  });
});
