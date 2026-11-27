import { DOCUMENT } from '@angular/common';
import {
  AfterViewChecked,
  Component,
  ElementRef,
  EventEmitter,
  HostListener,
  Input,
  OnChanges,
  Output,
  SimpleChanges,
  ViewChild,
  computed,
  inject,
} from '@angular/core';
import { LucideAngularModule, X } from 'lucide-angular';

import {
  ShortcutScope,
  findShortcutById,
  formatShortcutLabel,
  matchesShortcut,
  shortcutGroups,
} from '../../config/shortcuts';
import { I18nService } from '../../services/i18n.service';
import { ShortcutService } from '../../services/shortcut.service';
import type { TranslationKey } from '../../i18n';

const CLOSE_SHORTCUT = findShortcutById('dialog.close');

const SCOPE_LABEL_KEYS: Record<ShortcutScope, TranslationKey> = {
  global: 'shortcuts.scope.global',
  page: 'shortcuts.scope.page',
  dialog: 'shortcuts.scope.dialog',
};

/**
 * The shortcut sheet: a modal generated from `config/shortcuts.ts`.
 *
 * Every row, its group and its key label come from the registry, so the sheet
 * can never drift from what the shell actually dispatches. Escape is scoped to
 * this dialog: the sheet matches the registry's `dialog.close` binding itself
 * instead of relying on a global Escape handler.
 */
@Component({
  selector: 'app-shortcut-sheet',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './shortcut-sheet.html',
  styleUrl: './shortcut-sheet.css',
})
export class ShortcutSheet implements OnChanges, AfterViewChecked {
  readonly i18n = inject(I18nService);
  private readonly shortcuts = inject(ShortcutService);
  private readonly document = inject(DOCUMENT);

  @Input() open = false;

  @Output() closed = new EventEmitter<void>();

  readonly closeIcon = X;
  readonly titleId = 'shortcut-sheet-title';

  @ViewChild('closeButton')
  private closeButton?: ElementRef<HTMLButtonElement>;

  readonly groups = computed(() =>
    shortcutGroups().map((group) => ({
      scope: group.scope,
      label: this.i18n.t(SCOPE_LABEL_KEYS[group.scope]),
      entries: group.entries.map((entry) => ({
        id: entry.id,
        label: this.i18n.t(entry.labelKey),
        description: this.i18n.t(entry.descriptionKey),
        keys: formatShortcutLabel(entry, this.shortcuts.platform),
      })),
    })),
  );

  readonly closeKey = formatShortcutLabel(
    CLOSE_SHORTCUT,
    this.shortcuts.platform,
  );

  readonly closeHint = computed(() => this.i18n.t('shortcuts.sheet.hint'));

  private focusPending = false;
  private previouslyFocused: HTMLElement | null = null;

  ngOnChanges(changes: SimpleChanges): void {
    if (!changes['open']) {
      return;
    }

    if (this.open) {
      this.previouslyFocused = this.activeElement();
      this.focusPending = true;
      return;
    }

    this.previouslyFocused = null;
  }

  ngAfterViewChecked(): void {
    if (!this.focusPending) {
      return;
    }

    this.focusPending = false;
    this.closeButton?.nativeElement.focus();
  }

  onBackdropClick(event: MouseEvent): void {
    if (event.target === event.currentTarget) {
      this.close();
    }
  }

  close(): void {
    this.closed.emit();
    this.restoreFocus();
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.open) {
      return;
    }

    if (matchesShortcut(event, CLOSE_SHORTCUT, this.shortcuts.platform)) {
      event.preventDefault();
      event.stopPropagation();
      this.close();
      return;
    }

    if (event.key === 'Tab') {
      // The close button is the only focus target, so Tab cannot leave the
      // dialog; keep focus exactly there.
      event.preventDefault();
      this.closeButton?.nativeElement.focus();
    }
  }

  private restoreFocus(): void {
    const target = this.previouslyFocused;
    this.previouslyFocused = null;
    if (target?.isConnected) {
      target.focus();
    }
  }

  private activeElement(): HTMLElement | null {
    const active = this.document?.activeElement;
    return active instanceof HTMLElement ? active : null;
  }
}
