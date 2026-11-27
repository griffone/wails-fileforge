import type { LucideIconData } from 'lucide-angular';

/**
 * One row of a context menu, already translated by the caller.
 *
 * Items are data, so the same component serves the file queue, result rows and
 * any future surface without a second rendering path. A disabled item stays in
 * the list on purpose: the user learns that the action exists and why it does
 * not apply, instead of watching entries appear and disappear.
 */
export interface ContextMenuItem {
  /** Stable identifier emitted through `select`. */
  id: string;
  /** Visible label and accessible name. */
  label: string;
  icon?: LucideIconData;
  disabled?: boolean;
  /** Destructive styling only; the component never decides semantics. */
  danger?: boolean;
}
