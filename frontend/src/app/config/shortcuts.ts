import type { TranslationKey } from '../i18n';

/**
 * One keyboard shortcut, as data.
 *
 * The shell dispatch, the shortcut sheet and the tests all read this list, so a
 * binding cannot be documented in one place and implemented in another.
 */
export type ShortcutScope = 'global' | 'page' | 'dialog';

/**
 * `primary` means Cmd on macOS and Ctrl everywhere else. `windows` is the
 * Ctrl-style label used by Windows and Linux desktops.
 */
export type ShortcutPlatform = 'mac' | 'windows';

export interface ShortcutBinding {
  /** `KeyboardEvent.key`, lowercase for letters. */
  key: string;
  /** Cmd on macOS, Ctrl elsewhere. */
  primary?: boolean;
  /**
   * Enforced only when present. Leave it out for keys that already need Shift
   * to produce `key` (for example `?` on a US layout).
   */
  shift?: boolean;
  alt?: boolean;
  /** Platform override, for a binding that is genuinely different on macOS. */
  mac?: { key?: string };
}

export interface ShortcutDefinition {
  id: ShortcutId;
  scope: ShortcutScope;
  binding: ShortcutBinding;
  labelKey: TranslationKey;
  descriptionKey: TranslationKey;
  /** Repeating the key is harmless (it opens an already-open surface). */
  repeatable?: boolean;
}

export const SHORTCUT_IDS = [
  'palette.open',
  'settings.open',
  'shortcutSheet.open',
  'page.openFiles',
  'page.run',
  'dialog.close',
] as const;

export type ShortcutId = (typeof SHORTCUT_IDS)[number];

export const SHORTCUTS = [
  {
    id: 'palette.open',
    scope: 'global',
    binding: { key: 'k', primary: true, shift: false, alt: false },
    labelKey: 'shortcuts.palette.open.label',
    descriptionKey: 'shortcuts.palette.open.description',
    repeatable: true,
  },
  {
    id: 'settings.open',
    scope: 'global',
    binding: { key: ',', primary: true, shift: false, alt: false },
    labelKey: 'shortcuts.settings.open.label',
    descriptionKey: 'shortcuts.settings.open.description',
    repeatable: true,
  },
  {
    id: 'shortcutSheet.open',
    scope: 'global',
    binding: { key: '?' },
    labelKey: 'shortcuts.sheet.open.label',
    descriptionKey: 'shortcuts.sheet.open.description',
    repeatable: true,
  },
  {
    id: 'page.openFiles',
    scope: 'page',
    binding: { key: 'o', primary: true, shift: false, alt: false },
    labelKey: 'shortcuts.page.openFiles.label',
    descriptionKey: 'shortcuts.page.openFiles.description',
  },
  {
    id: 'page.run',
    scope: 'page',
    binding: { key: 'Enter', primary: true, shift: false, alt: false },
    labelKey: 'shortcuts.page.run.label',
    descriptionKey: 'shortcuts.page.run.description',
  },
  {
    id: 'dialog.close',
    scope: 'dialog',
    binding: { key: 'Escape' },
    labelKey: 'shortcuts.dialog.close.label',
    descriptionKey: 'shortcuts.dialog.close.description',
  },
] as const satisfies readonly ShortcutDefinition[];

/** Sheet order: app-wide first, then the page, then the dialog itself. */
export const SHORTCUT_SCOPES: readonly ShortcutScope[] = [
  'global',
  'page',
  'dialog',
];

export interface ResolvedShortcutBinding {
  key: string;
  primary: boolean;
  shift?: boolean;
  alt: boolean;
}

const DEFAULT_NAV: Pick<Navigator, 'platform' | 'userAgent'> | undefined =
  typeof navigator === 'undefined' ? undefined : navigator;

export function detectShortcutPlatform(
  nav:
    | Pick<Navigator, 'platform' | 'userAgent'>
    | null
    | undefined = DEFAULT_NAV,
): ShortcutPlatform {
  if (!nav) {
    return 'windows';
  }
  const platform = nav.platform ?? '';
  return /mac/i.test(platform) || /mac os x/i.test(nav.userAgent ?? '')
    ? 'mac'
    : 'windows';
}

export function resolveShortcutBinding(
  definition: ShortcutDefinition,
  platform: ShortcutPlatform,
): ResolvedShortcutBinding {
  const binding = definition.binding;
  return {
    key:
      platform === 'mac' && binding.mac?.key ? binding.mac.key : binding.key,
    primary: binding.primary === true,
    shift: binding.shift,
    alt: binding.alt === true,
  };
}

export function findShortcutById(id: ShortcutId): ShortcutDefinition {
  const shortcut = SHORTCUTS.find((candidate) => candidate.id === id);
  if (!shortcut) {
    throw new Error(`Unknown shortcut id: ${id}`);
  }
  return shortcut;
}

function matchesKey(event: KeyboardEvent, key: string): boolean {
  if (key.length === 1) {
    return (
      event.key.length === 1 &&
      event.key.toLowerCase() === key.toLowerCase()
    );
  }
  return event.key === key;
}

export function matchesShortcut(
  event: KeyboardEvent,
  definition: ShortcutDefinition,
  platform: ShortcutPlatform,
): boolean {
  const resolved = resolveShortcutBinding(definition, platform);
  if (!matchesKey(event, resolved.key)) {
    return false;
  }

  // Cmd and Ctrl are both accepted as "primary": Ctrl+K is still the shortcut
  // on macOS keyboards set up for Windows muscle memory, and Meta+K reaches the
  // same handler on Linux. The labels stay platform-correct.
  const primaryPressed = event.metaKey || event.ctrlKey;
  if (resolved.primary !== primaryPressed) {
    return false;
  }

  if (resolved.shift !== undefined && resolved.shift !== event.shiftKey) {
    return false;
  }

  if (resolved.alt !== event.altKey) {
    return false;
  }

  return true;
}

export function findShortcut(
  event: KeyboardEvent,
  platform: ShortcutPlatform,
  scope?: ShortcutScope,
): ShortcutDefinition | null {
  for (const definition of SHORTCUTS) {
    if (scope && definition.scope !== scope) {
      continue;
    }
    if (matchesShortcut(event, definition, platform)) {
      return definition;
    }
  }
  return null;
}

const MAC_KEY_LABELS: Record<string, string> = {
  Enter: '↵',
  Escape: 'Esc',
  Tab: '⇥',
};

const WINDOWS_KEY_LABELS: Record<string, string> = {
  Escape: 'Esc',
  Tab: 'Tab',
};

function keyLabel(key: string, platform: ShortcutPlatform): string {
  const labels = platform === 'mac' ? MAC_KEY_LABELS : WINDOWS_KEY_LABELS;
  if (labels[key]) {
    return labels[key];
  }
  return key.length === 1 ? key.toUpperCase() : key;
}

/** Platform-correct label: `⌘K` on macOS, `Ctrl+K` on Windows and Linux. */
export function formatShortcutLabel(
  definition: ShortcutDefinition,
  platform: ShortcutPlatform,
): string {
  const resolved = resolveShortcutBinding(definition, platform);
  const key = keyLabel(resolved.key, platform);

  if (platform === 'mac') {
    const modifiers = [
      resolved.primary ? '⌘' : '',
      resolved.alt ? '⌥' : '',
      resolved.shift ? '⇧' : '',
    ].join('');
    return `${modifiers}${key}`;
  }

  const modifiers: string[] = [];
  if (resolved.primary) {
    modifiers.push('Ctrl');
  }
  if (resolved.alt) {
    modifiers.push('Alt');
  }
  if (resolved.shift) {
    modifiers.push('Shift');
  }
  return [...modifiers, key].join('+');
}

export function isEditableShortcutTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }

  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') {
    return true;
  }

  return (
    target.isContentEditable ||
    target.closest(
      '[contenteditable=""], [contenteditable="true"], [contenteditable="plaintext-only"]',
    ) !== null
  );
}

/**
 * True while any modal dialog is open. Dialogs own their own keys (the palette
 * closes on Escape, the sheet traps Tab), so app and page shortcuts stay out of
 * their way instead of racing them.
 */
export function isDialogOpen(
  doc: Document | null | undefined = typeof document === 'undefined'
    ? null
    : document,
): boolean {
  return doc?.querySelector('[role="dialog"]') != null;
}

export function shouldIgnoreShortcutEvent(
  event: KeyboardEvent,
  definition: ShortcutDefinition,
  doc?: Document | null,
): boolean {
  if (event.defaultPrevented) {
    return true;
  }
  if (event.repeat && definition.repeatable !== true) {
    return true;
  }
  if (isEditableShortcutTarget(event.target)) {
    return true;
  }
  return isDialogOpen(doc);
}

export interface ShortcutGroup {
  scope: ShortcutScope;
  entries: readonly ShortcutDefinition[];
}

export function shortcutGroups(): ShortcutGroup[] {
  return SHORTCUT_SCOPES.map((scope) => ({
    scope,
    entries: SHORTCUTS.filter((definition) => definition.scope === scope),
  }));
}
