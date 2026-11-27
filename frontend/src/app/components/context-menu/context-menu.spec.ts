import {
  Component,
  ElementRef,
  ViewChild,
} from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ContextMenu } from './context-menu';
import { ContextMenuItem } from './context-menu.types';

@Component({
  standalone: true,
  imports: [ContextMenu],
  template: `
    <button #opener type="button" class="opener">Opener</button>
    <app-context-menu
      [open]="open"
      [x]="x"
      [y]="y"
      [items]="items"
      [label]="label"
      [opener]="openerElement"
      (select)="selected.push($event)"
      (closed)="onClosed()"
    ></app-context-menu>
  `,
})
class HostComponent {
  @ViewChild('opener', { static: true })
  openerRef!: ElementRef<HTMLButtonElement>;

  open = false;
  x = 0;
  y = 0;
  label = 'File actions';
  openerElement: HTMLElement | null = null;
  selected: string[] = [];
  closedCount = 0;

  items: ContextMenuItem[] = [
    { id: 'open', label: 'Open' },
    { id: 'reveal', label: 'Reveal in folder' },
    { id: 'copyPath', label: 'Copy path' },
    { id: 'remove', label: 'Remove from queue', danger: true },
  ];

  onClosed(): void {
    this.closedCount += 1;
    this.open = false;
  }
}

describe('ContextMenu', () => {
  let fixture: ComponentFixture<HostComponent>;
  let host: HostComponent;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [HostComponent],
    }).compileComponents();
    fixture = TestBed.createComponent(HostComponent);
    host = fixture.componentInstance;
    host.openerElement = host.openerRef.nativeElement;
  });

  afterEach(() => {
    fixture.destroy();
  });

  function menu(): HTMLElement | null {
    return fixture.nativeElement.querySelector('.context-menu');
  }

  function buttons(): HTMLButtonElement[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll('.context-menu-item'),
    );
  }

  function openMenu(x = 120, y = 80): void {
    host.x = x;
    host.y = y;
    host.open = true;
    fixture.detectChanges();
  }

  function press(key: string, target: EventTarget | null = menu()): KeyboardEvent {
    const event = new KeyboardEvent('keydown', {
      key,
      bubbles: true,
      cancelable: true,
    });
    (target as HTMLElement).dispatchEvent(event);
    return event;
  }

  it('renders nothing while closed', () => {
    fixture.detectChanges();

    expect(menu()).toBeNull();
  });

  it('opens at the requested point with menu and menuitem roles', () => {
    openMenu(120, 80);

    const element = menu();
    expect(element).not.toBeNull();
    expect(element?.getAttribute('role')).toBe('menu');
    expect(element?.getAttribute('aria-label')).toBe('File actions');
    expect(element?.style.left).toBe('120px');
    expect(element?.style.top).toBe('80px');

    const items = buttons();
    expect(items.length).toBe(4);
    for (const item of items) {
      expect(item.getAttribute('role')).toBe('menuitem');
      expect(item.tabIndex).toBe(0);
    }
    expect(items[3].getAttribute('aria-disabled')).toBeNull();
    expect(items[3].classList).toContain('is-danger');
  });

  it('renders disabled items instead of hiding them', () => {
    host.items = [
      { id: 'open', label: 'Open', disabled: true },
      { id: 'remove', label: 'Remove', disabled: true },
    ];
    openMenu();

    const items = buttons();
    expect(items.length).toBe(2);
    expect(items[0].disabled).toBeTrue();
    expect(items[1].disabled).toBeTrue();
    expect(items[0].getAttribute('aria-disabled')).toBe('true');
  });

  it('focuses the first enabled item on open', () => {
    host.items = [
      { id: 'open', label: 'Open', disabled: true },
      { id: 'reveal', label: 'Reveal in folder' },
      { id: 'copyPath', label: 'Copy path' },
    ];
    openMenu();

    expect(document.activeElement).toBe(buttons()[1]);
  });

  it('moves with ArrowDown and ArrowUp, wrapping around enabled items', () => {
    openMenu();

    press('ArrowDown');
    expect(document.activeElement).toBe(buttons()[1]);

    press('ArrowUp');
    expect(document.activeElement).toBe(buttons()[0]);

    press('ArrowUp');
    expect(document.activeElement).toBe(buttons()[3]);
  });

  it('skips disabled items while navigating', () => {
    host.items = [
      { id: 'open', label: 'Open' },
      { id: 'reveal', label: 'Reveal', disabled: true },
      { id: 'copyPath', label: 'Copy path' },
    ];
    openMenu();

    press('ArrowDown');
    expect(document.activeElement).toBe(buttons()[2]);
  });

  it('jumps to the first and last enabled item with Home and End', () => {
    host.items = [
      { id: 'open', label: 'Open', disabled: true },
      { id: 'reveal', label: 'Reveal' },
      { id: 'copyPath', label: 'Copy path' },
      { id: 'remove', label: 'Remove', disabled: true },
    ];
    openMenu();

    press('End');
    expect(document.activeElement).toBe(buttons()[2]);

    press('Home');
    expect(document.activeElement).toBe(buttons()[1]);
  });

  it('activates the focused item with Enter and space', () => {
    openMenu();

    const enter = press('Enter', document.activeElement);
    expect(enter.defaultPrevented).toBeTrue();
    expect(host.selected).toEqual(['open']);

    fixture.detectChanges();
    openMenu();
    press('ArrowDown');
    press(' ', document.activeElement);
    expect(host.selected).toEqual(['open', 'reveal']);
  });

  it('closes on Escape, emits closed and restores focus to the opener', () => {
    host.openerRef.nativeElement.focus();
    openMenu();

    // The first item owns focus, so the opener is no longer active.
    expect(document.activeElement).toBe(buttons()[0]);

    press('Escape');

    expect(host.closedCount).toBe(1);
    expect(document.activeElement).toBe(host.openerRef.nativeElement);
  });

  it('closes on an outside pointer down', () => {
    openMenu();

    document.body.dispatchEvent(
      new PointerEvent('pointerdown', { bubbles: true }),
    );

    expect(host.closedCount).toBe(1);
  });

  it('does not close on a pointer down inside the menu', () => {
    openMenu();

    buttons()[0].dispatchEvent(
      new PointerEvent('pointerdown', { bubbles: true }),
    );

    expect(host.closedCount).toBe(0);
  });

  it('closes on scroll', () => {
    openMenu();

    window.dispatchEvent(new Event('scroll'));

    expect(host.closedCount).toBe(1);
  });

  it('does not trap Tab so items stay reachable from the keyboard', () => {
    openMenu();

    const event = press('Tab');

    expect(event.defaultPrevented).toBeFalse();
    expect(host.closedCount).toBe(0);
  });

  it('closes when focus leaves the menu', () => {
    openMenu();

    buttons()[buttons().length - 1].focus();
    host.openerRef.nativeElement.focus();

    expect(host.closedCount).toBe(1);
  });

  it('never emits a disabled item', () => {
    host.items = [
      { id: 'open', label: 'Open', disabled: true },
      { id: 'remove', label: 'Remove' },
    ];
    openMenu();

    buttons()[0].click();

    expect(host.selected).toEqual([]);
  });

  it('emits the item id and closes when an item is chosen', () => {
    openMenu();

    buttons()[2].click();

    expect(host.selected).toEqual(['copyPath']);
    expect(host.closedCount).toBe(1);
    expect(document.activeElement).toBe(host.openerRef.nativeElement);
  });
});
