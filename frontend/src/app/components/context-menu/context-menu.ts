import { DOCUMENT } from '@angular/common';
import {
  AfterViewChecked,
  Component,
  ElementRef,
  EventEmitter,
  HostListener,
  Input,
  OnChanges,
  OnDestroy,
  Output,
  SimpleChanges,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { LucideAngularModule } from 'lucide-angular';

import { ContextMenuItem } from './context-menu.types';

const VIEWPORT_MARGIN = 8;

interface MenuPosition {
  left: number;
  top: number;
}

/**
 * Shared in-app context menu.
 *
 * The owner supplies the items and the anchor point; this component only
 * renders, positions, moves focus and reports the chosen id. It is deliberately
 * not a service: every list keeps its own menu state, so opening a menu on one
 * row can never reuse a stale target from another row.
 *
 * Behaviour contract:
 * - `role="menu"` / `role="menuitem"`, with disabled items rendered disabled.
 * - Arrow keys, Home/End, Enter/Space and Escape; Tab is left to the browser so
 *   menu items remain reachable from the keyboard.
 * - Closes on Escape, outside pointer down, scroll, and when focus leaves.
 * - Restores focus to `opener` when it is still connected.
 */
@Component({
  selector: 'app-context-menu',
  standalone: true,
  imports: [LucideAngularModule],
  templateUrl: './context-menu.html',
  styleUrl: './context-menu.css',
})
export class ContextMenu implements OnChanges, AfterViewChecked, OnDestroy {
  private readonly document = inject(DOCUMENT);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  /** Open state, owned by the parent. */
  @Input() open = false;
  /** Viewport point where the menu should appear. */
  @Input() x = 0;
  @Input() y = 0;
  /** Translated items; disabled entries stay visible. */
  @Input() items: readonly ContextMenuItem[] = [];
  /** Accessible name for the menu, for example "File actions". */
  @Input() label = '';
  /** Element that opened the menu; focus returns here on close. */
  @Input() opener: HTMLElement | null = null;

  @Output() select = new EventEmitter<string>();
  @Output() closed = new EventEmitter<void>();

  readonly position = signal<MenuPosition>({ left: 0, top: 0 });

  private readonly menuElement =
    viewChild<ElementRef<HTMLElement>>('menu');

  private wasOpen = false;
  private closing = false;
  private focusPending = false;
  private placementPending = false;

  private readonly onWindowScroll = (): void => {
    if (this.open) {
      this.close();
    }
  };

  ngOnChanges(changes: SimpleChanges): void {
    if (changes['open']) {
      if (this.open && !this.wasOpen) {
        this.wasOpen = true;
        this.closing = false;
        this.focusPending = true;
        this.placementPending = true;
        window.addEventListener('scroll', this.onWindowScroll, true);
      } else if (!this.open && this.wasOpen) {
        this.wasOpen = false;
        this.teardown(!this.closing);
        this.closing = false;
      }
    }

    if (this.open && (changes['x'] || changes['y'] || changes['items'])) {
      this.placementPending = true;
    }
  }

  ngAfterViewChecked(): void {
    if (!this.open) {
      return;
    }

    if (this.placementPending) {
      this.placementPending = false;
      this.placeMenu();
    }

    if (this.focusPending) {
      this.focusPending = false;
      this.focusFirstEnabled();
    }
  }

  ngOnDestroy(): void {
    this.teardown(false);
  }

  onItemClick(item: ContextMenuItem): void {
    if (item.disabled) {
      return;
    }
    this.select.emit(item.id);
    this.close();
  }

  onKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        this.moveFocus(1);
        return;
      case 'ArrowUp':
        event.preventDefault();
        this.moveFocus(-1);
        return;
      case 'Home':
        event.preventDefault();
        this.focusEnabledAt(0);
        return;
      case 'End':
        event.preventDefault();
        this.focusEnabledAt(-1);
        return;
      case 'Enter':
      case ' ':
        this.activateFocusedItem(event);
        return;
      case 'Escape':
        event.preventDefault();
        event.stopPropagation();
        this.close();
        return;
      default:
        return;
    }
  }

  /** Tab is not trapped: leaving the menu closes it, as a desktop menu does. */
  onFocusOut(event: FocusEvent): void {
    const next = event.relatedTarget;
    if (next instanceof Node && this.host.nativeElement.contains(next)) {
      return;
    }
    this.close();
  }

  @HostListener('document:pointerdown', ['$event'])
  onDocumentPointerDown(event: PointerEvent): void {
    if (!this.open) {
      return;
    }
    const target = event.target;
    if (target instanceof Node && this.host.nativeElement.contains(target)) {
      return;
    }
    this.close();
  }

  close(): void {
    if (!this.open || this.closing) {
      return;
    }

    this.closing = true;
    this.teardown(true);
    this.closed.emit();
  }

  private teardown(restoreFocus: boolean): void {
    window.removeEventListener('scroll', this.onWindowScroll, true);
    if (restoreFocus) {
      this.restoreFocus();
    }
  }

  private restoreFocus(): void {
    const opener = this.opener;
    if (opener?.isConnected) {
      opener.focus();
    }
  }

  private placeMenu(): void {
    const element = this.menuElement()?.nativeElement;
    if (!element || typeof window === 'undefined') {
      return;
    }

    const rect = element.getBoundingClientRect();
    const maxLeft = Math.max(
      VIEWPORT_MARGIN,
      window.innerWidth - rect.width - VIEWPORT_MARGIN,
    );
    const maxTop = Math.max(
      VIEWPORT_MARGIN,
      window.innerHeight - rect.height - VIEWPORT_MARGIN,
    );

    this.position.set({
      left: Math.min(Math.max(this.x, VIEWPORT_MARGIN), maxLeft),
      top: Math.min(Math.max(this.y, VIEWPORT_MARGIN), maxTop),
    });
  }

  private enabledButtons(): HTMLButtonElement[] {
    const element = this.menuElement()?.nativeElement;
    if (!element) {
      return [];
    }
    return Array.from(
      element.querySelectorAll<HTMLButtonElement>(
        '[role="menuitem"]:not(:disabled)',
      ),
    );
  }

  private focusFirstEnabled(): void {
    const buttons = this.enabledButtons();
    if (buttons.length > 0) {
      buttons[0].focus();
      return;
    }
    this.menuElement()?.nativeElement.focus();
  }

  private focusEnabledAt(index: number): void {
    const buttons = this.enabledButtons();
    if (buttons.length === 0) {
      return;
    }
    const target = index < 0 ? buttons[buttons.length - 1] : buttons[index];
    target.focus();
  }

  private moveFocus(delta: number): void {
    const buttons = this.enabledButtons();
    if (buttons.length === 0) {
      return;
    }

    const active = this.document.activeElement;
    const current = buttons.findIndex((button) => button === active);
    const from = current < 0 ? (delta > 0 ? -1 : 0) : current;
    const next = (from + delta + buttons.length) % buttons.length;
    buttons[next].focus();
  }

  private activateFocusedItem(event: KeyboardEvent): void {
    const active = this.document.activeElement;
    if (!(active instanceof HTMLButtonElement) || active.disabled) {
      return;
    }
    if (!this.host.nativeElement.contains(active)) {
      return;
    }

    event.preventDefault();
    active.click();
  }
}
