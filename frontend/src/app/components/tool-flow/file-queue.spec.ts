import { ComponentFixture, TestBed } from '@angular/core/testing';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { ClipboardService } from '../../services/clipboard.service';
import { Wails } from '../../services/wails';
import { FileQueue } from './file-queue';
import { FileQueueItem } from './tool-flow.types';

describe('FileQueue', () => {
  let fixture: ComponentFixture<FileQueue>;
  let component: FileQueue;
  let clipboardSpy: jasmine.SpyObj<ClipboardService>;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ language: 'en' }),
    );
    clipboardSpy = jasmine.createSpyObj<ClipboardService>('ClipboardService', [
      'writeText',
    ]);
    clipboardSpy.writeText.and.resolveTo(true);
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'openPath',
      'revealInFolder',
    ]);
    wailsSpy.openPath.and.resolveTo({ success: true, message: '' });
    wailsSpy.revealInFolder.and.resolveTo({ success: true, message: '' });

    await TestBed.configureTestingModule({
      imports: [FileQueue],
      providers: [
        { provide: ClipboardService, useValue: clipboardSpy },
        { provide: Wails, useValue: wailsSpy },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(FileQueue);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('renders the empty state copy', () => {
    fixture.detectChanges();

    const empty = fixture.nativeElement.querySelector('.file-queue-empty') as HTMLElement;
    expect(empty.textContent).toContain('No files yet');
    expect(empty.textContent).toContain('Drop files above to get started.');
  });

  it('renders the list and empty state copy in Spanish', () => {
    const i18n = TestBed.inject(I18nService);
    i18n.setLocale('es');
    component.reorderable = true;
    fixture.detectChanges();

    const list = fixture.nativeElement.querySelector('.file-queue') as HTMLElement;
    expect(list.getAttribute('aria-label')).toBe('Archivos seleccionados');

    const empty = fixture.nativeElement.querySelector('.file-queue-empty') as HTMLElement;
    expect(empty.textContent).toContain('Aún no hay archivos');
    expect(empty.textContent).toContain('Suelta archivos arriba para empezar.');

    component.items = [{ path: '/tmp/a.pdf', name: 'a.pdf' }];
    fixture.detectChanges();

    const row = fixture.nativeElement.querySelector('.file-queue-row') as HTMLElement;
    const actions = row.querySelectorAll('.file-queue-action');
    expect(actions[0].getAttribute('aria-label')).toBe('Mover a.pdf hacia arriba');
    expect(actions[1].getAttribute('aria-label')).toBe('Mover a.pdf hacia abajo');
    expect(
      row.querySelector('.file-queue-remove')?.getAttribute('aria-label'),
    ).toBe('Quitar a.pdf');
  });

  it('renders rows and emits the path on remove', () => {
    const removeSpy = jasmine.createSpy('remove');
    component.remove.subscribe(removeSpy);
    const items: FileQueueItem[] = [
      { path: '/tmp/a.png', name: 'a.png', meta: '1.5 KB · 800 × 600' },
      { path: '/tmp/b.pdf', name: 'b.pdf', error: 'This file is damaged.' },
    ];
    component.items = items;
    fixture.detectChanges();

    const rows = fixture.nativeElement.querySelectorAll('.file-queue-row');
    expect(rows.length).toBe(2);
    expect(rows[0].textContent).toContain('a.png');
    expect(rows[0].textContent).toContain('800 × 600');
    expect(rows[1].textContent).toContain('This file is damaged.');

    const remove = rows[1].querySelector('.file-queue-remove') as HTMLButtonElement;
    expect(remove.getAttribute('aria-label')).toBe('Remove b.pdf');
    remove.click();

    expect(removeSpy).toHaveBeenCalledOnceWith('/tmp/b.pdf');
  });

  it('prefers a thumbnail image when provided', () => {
    component.items = [
      { path: '/tmp/a.png', name: 'a.png', thumbnailUrl: 'data:image/webp;base64,abc' },
    ];
    fixture.detectChanges();

    const image = fixture.nativeElement.querySelector('.file-queue-image') as HTMLImageElement;
    expect(image.src).toContain('data:image/webp;base64,abc');
    expect(image.getAttribute('alt')).toBe('');
  });

  it('emits move up/down for a reorderable queue and disables the edges', () => {
    const up = jasmine.createSpy('moveUp');
    const down = jasmine.createSpy('moveDown');
    component.moveUp.subscribe(up);
    component.moveDown.subscribe(down);
    component.reorderable = true;
    component.items = [
      { path: '/tmp/a.pdf', name: 'a.pdf' },
      { path: '/tmp/b.pdf', name: 'b.pdf' },
    ];
    fixture.detectChanges();

    const rows = fixture.nativeElement.querySelectorAll('.file-queue-row');
    const firstUp = rows[0].querySelectorAll('.file-queue-action')[0] as HTMLButtonElement;
    const firstDown = rows[0].querySelectorAll('.file-queue-action')[1] as HTMLButtonElement;
    expect(firstUp.disabled).toBeTrue();
    expect(firstDown.disabled).toBeFalse();

    firstDown.click();
    expect(down).toHaveBeenCalledOnceWith('/tmp/a.pdf');
    expect(up).not.toHaveBeenCalled();
  });

  describe('context menu', () => {
    function rowAt(index = 0): HTMLElement {
      return fixture.nativeElement.querySelectorAll('.file-queue-row')[
        index
      ] as HTMLElement;
    }

    function item(id: string): HTMLButtonElement {
      return fixture.nativeElement.querySelector(
        `.context-menu-item[data-item-id="${id}"]`,
      ) as HTMLButtonElement;
    }

    function menu(): HTMLElement | null {
      return fixture.nativeElement.querySelector('.context-menu') as HTMLElement | null;
    }

    function labels(): string[] {
      return Array.from(
        fixture.nativeElement.querySelectorAll('.context-menu-label'),
      ).map((label) => (label as HTMLElement).textContent?.trim() ?? '');
    }

    function rightClick(target: HTMLElement, x = 32, y = 44): MouseEvent {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: x,
        clientY: y,
      });
      target.dispatchEvent(event);
      fixture.detectChanges();
      return event;
    }

    function rect(left: number, bottom: number): DOMRect {
      return {
        top: bottom - 60,
        bottom,
        height: 60,
        left,
        right: left + 200,
        width: 200,
        x: left,
        y: bottom - 60,
        toJSON: () => ({}),
      } as DOMRect;
    }

    beforeEach(() => {
      component.items = [
        { path: '/tmp/a.pdf', name: 'a.pdf', meta: '1 page' },
        { path: '/tmp/b.pdf', name: 'b.pdf', meta: '2 pages' },
      ];
      fixture.detectChanges();
    });

    it('opens at the pointer with Open, Reveal, Copy path and Remove', () => {
      const removeSpy = jasmine.createSpy('remove');
      component.remove.subscribe(removeSpy);
      const clickSpy = jasmine.createSpy('click');
      const row = rowAt();
      row.addEventListener('click', clickSpy);

      const event = rightClick(row);

      expect(event.defaultPrevented).toBeTrue();
      expect(clickSpy).not.toHaveBeenCalled();
      expect(removeSpy).not.toHaveBeenCalled();

      const element = menu();
      expect(element).not.toBeNull();
      expect(element?.getAttribute('role')).toBe('menu');
      expect(element?.style.left).toBe('32px');
      expect(element?.style.top).toBe('44px');
      expect(labels()).toEqual([
        'Open',
        'Reveal in folder',
        'Copy path',
        'Remove from queue',
      ]);
    });

    it('does not select or remove when the row is right-clicked', () => {
      const removeSpy = jasmine.createSpy('remove');
      component.remove.subscribe(removeSpy);

      rightClick(rowAt(1));

      expect(removeSpy).not.toHaveBeenCalled();
      expect(component.menuPath()).toBe('/tmp/b.pdf');
    });

    it('opens from Shift+F10 on the focused row at the row position', () => {
      const row = rowAt();
      row.focus();
      spyOn(row, 'getBoundingClientRect').and.returnValue(rect(24, 96));

      row.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'F10',
          shiftKey: true,
          bubbles: true,
          cancelable: true,
        }),
      );
      fixture.detectChanges();

      const element = menu();
      expect(element?.style.left).toBe('24px');
      expect(element?.style.top).toBe('96px');
      expect(labels().length).toBe(4);
    });

    it('opens from the context-menu key', () => {
      const row = rowAt(1);
      row.focus();

      row.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'ContextMenu',
          bubbles: true,
          cancelable: true,
        }),
      );
      fixture.detectChanges();

      expect(menu()).not.toBeNull();
      expect(component.menuPath()).toBe('/tmp/b.pdf');
    });

    it('calls the clipboard service and reports a failed copy in a danger banner', async () => {
      clipboardSpy.writeText.and.resolveTo(false);
      rightClick(rowAt());

      item('copyPath').click();
      await fixture.whenStable();
      fixture.detectChanges();

      expect(clipboardSpy.writeText).toHaveBeenCalledWith('/tmp/a.pdf');
      const banner = fixture.nativeElement.querySelector(
        '.file-queue-banner',
      ) as HTMLElement;
      expect(banner.classList).toContain('banner-danger');
      expect(banner.textContent).toContain('The path could not be copied');
    });

    it('reports a successful copy without a danger banner', async () => {
      rightClick(rowAt());

      item('copyPath').click();
      await fixture.whenStable();
      fixture.detectChanges();

      const banner = fixture.nativeElement.querySelector(
        '.file-queue-banner',
      ) as HTMLElement;
      expect(banner.classList).toContain('banner-success');
      expect(banner.textContent).toContain('Path copied');
    });

    it('opens and reveals through the shared path actions', async () => {
      rightClick(rowAt());
      item('open').click();
      await fixture.whenStable();

      rightClick(rowAt());
      item('reveal').click();
      await fixture.whenStable();

      expect(wailsSpy.openPath).toHaveBeenCalledOnceWith('/tmp/a.pdf');
      expect(wailsSpy.revealInFolder).toHaveBeenCalledOnceWith('/tmp/a.pdf');
    });

    it('reports a failed open through the inline banner', async () => {
      wailsSpy.openPath.and.resolveTo({
        success: false,
        message: 'Permission denied',
      });
      rightClick(rowAt());

      item('open').click();
      await fixture.whenStable();
      fixture.detectChanges();

      const banner = fixture.nativeElement.querySelector(
        '.file-queue-banner',
      ) as HTMLElement;
      expect(banner.classList).toContain('banner-danger');
      expect(banner.textContent).toContain('Permission denied');
    });

    it('removes through the existing removal path', () => {
      const removeSpy = jasmine.createSpy('remove');
      component.remove.subscribe(removeSpy);
      rightClick(rowAt());

      item('remove').click();

      expect(removeSpy).toHaveBeenCalledOnceWith('/tmp/a.pdf');
    });

    it('disables every action for a row removed while the menu is open', () => {
      const removeSpy = jasmine.createSpy('remove');
      component.remove.subscribe(removeSpy);
      rightClick(rowAt());

      component.items = [];
      fixture.detectChanges();

      for (const id of ['open', 'reveal', 'copyPath', 'remove']) {
        expect(item(id).disabled).toBeTrue();
      }

      item('remove').click();
      item('open').click();

      expect(removeSpy).not.toHaveBeenCalled();
      expect(wailsSpy.openPath).not.toHaveBeenCalled();
    });

    it('closes on Escape and restores focus to the row', () => {
      const row = rowAt();
      rightClick(row);

      menu()?.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'Escape',
          bubbles: true,
          cancelable: true,
        }),
      );
      fixture.detectChanges();

      expect(menu()).toBeNull();
      expect(document.activeElement).toBe(row);
    });

    it('renders the Spanish menu copy', () => {
      TestBed.inject(I18nService).setLocale('es');
      fixture.detectChanges();

      rightClick(rowAt());

      expect(labels()).toEqual([
        'Abrir',
        'Mostrar en la carpeta',
        'Copiar ruta',
        'Quitar de la cola',
      ]);
    });
  });

  describe('drag reordering', () => {
    function rect(height: number): DOMRect {
      return {
        top: 0,
        bottom: height,
        height,
        left: 0,
        right: 0,
        width: 0,
        x: 0,
        y: 0,
        toJSON: () => ({}),
      } as DOMRect;
    }

    function dragEvent(
      type: string,
      transfer: DataTransfer,
      clientY = 0,
    ): DragEvent {
      return new DragEvent(type, {
        bubbles: true,
        cancelable: true,
        dataTransfer: transfer,
        clientY,
      });
    }

    function rows(): HTMLElement[] {
      return Array.from(
        fixture.nativeElement.querySelectorAll('.file-queue-row'),
      ) as HTMLElement[];
    }

    function startDrag(row: HTMLElement, transfer: DataTransfer): void {
      const handle = row.querySelector('.file-queue-drag') as HTMLElement;
      handle.dispatchEvent(dragEvent('dragstart', transfer));
    }

    beforeEach(() => {
      component.reorderable = true;
      component.items = [
        { path: '/tmp/a.pdf', name: 'a.pdf', meta: '1 page' },
        { path: '/tmp/b.pdf', name: 'b.pdf', meta: '2 pages' },
        { path: '/tmp/c.pdf', name: 'c.pdf', meta: '3 pages' },
      ];
      fixture.detectChanges();
    });

    it('emits the dragged path with its final index and shows an insertion line', () => {
      const reorder = jasmine.createSpy('reorder');
      component.reorder.subscribe(reorder);
      const transfer = new DataTransfer();
      const items = rows();

      startDrag(items[0], transfer);
      spyOn(items[2], 'getBoundingClientRect').and.returnValue(rect(60));

      items[2].dispatchEvent(dragEvent('dragover', transfer, 55));
      fixture.detectChanges();

      expect(items[0].classList).toContain('is-dragging');
      expect(items[2].classList).toContain('is-drop-after');
      expect(reorder).not.toHaveBeenCalled();

      items[2].dispatchEvent(dragEvent('drop', transfer, 55));

      expect(reorder).toHaveBeenCalledOnceWith({ path: '/tmp/a.pdf', to: 2 });
    });

    it('is a no-op when dropped on itself', () => {
      const reorder = jasmine.createSpy('reorder');
      component.reorder.subscribe(reorder);
      const transfer = new DataTransfer();
      const items = rows();

      startDrag(items[1], transfer);
      spyOn(items[1], 'getBoundingClientRect').and.returnValue(rect(60));

      items[1].dispatchEvent(dragEvent('dragover', transfer, 10));
      items[1].dispatchEvent(dragEvent('drop', transfer, 10));

      expect(reorder).not.toHaveBeenCalled();
    });

    it('changes nothing when the drag ends outside the list', () => {
      const reorder = jasmine.createSpy('reorder');
      component.reorder.subscribe(reorder);
      const transfer = new DataTransfer();
      const items = rows();

      startDrag(items[0], transfer);
      spyOn(items[1], 'getBoundingClientRect').and.returnValue(rect(60));
      items[1].dispatchEvent(dragEvent('dragover', transfer, 55));

      const handle = items[0].querySelector('.file-queue-drag') as HTMLElement;
      handle.dispatchEvent(dragEvent('dragend', transfer));
      fixture.detectChanges();

      expect(reorder).not.toHaveBeenCalled();
      expect(items[1].classList).not.toContain('is-drop-after');
    });

    it('ignores a drop when the dragged row was removed mid-drag', () => {
      const reorder = jasmine.createSpy('reorder');
      component.reorder.subscribe(reorder);
      const transfer = new DataTransfer();

      startDrag(rows()[0], transfer);
      component.items = [
        { path: '/tmp/b.pdf', name: 'b.pdf', meta: '2 pages' },
        { path: '/tmp/c.pdf', name: 'c.pdf', meta: '3 pages' },
      ];
      fixture.detectChanges();

      const items = rows();
      spyOn(items[1], 'getBoundingClientRect').and.returnValue(rect(60));
      items[1].dispatchEvent(dragEvent('dragover', transfer, 55));
      items[1].dispatchEvent(dragEvent('drop', transfer, 55));

      expect(reorder).not.toHaveBeenCalled();
    });

    it('ignores drags that did not start in the queue', () => {
      const reorder = jasmine.createSpy('reorder');
      component.reorder.subscribe(reorder);
      const transfer = new DataTransfer();
      const items = rows();

      spyOn(items[1], 'getBoundingClientRect').and.returnValue(rect(60));
      items[1].dispatchEvent(dragEvent('dragover', transfer, 55));
      items[1].dispatchEvent(dragEvent('drop', transfer, 55));

      expect(reorder).not.toHaveBeenCalled();
    });
  });
});
