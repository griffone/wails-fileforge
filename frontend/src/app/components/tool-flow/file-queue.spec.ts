import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FileQueue } from './file-queue';
import { FileQueueItem } from './tool-flow.types';

describe('FileQueue', () => {
  let fixture: ComponentFixture<FileQueue>;
  let component: FileQueue;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [FileQueue] }).compileComponents();
    fixture = TestBed.createComponent(FileQueue);
    component = fixture.componentInstance;
  });

  it('renders the empty state copy', () => {
    fixture.detectChanges();

    const empty = fixture.nativeElement.querySelector('.file-queue-empty') as HTMLElement;
    expect(empty.textContent).toContain('No files yet');
    expect(empty.textContent).toContain('Drop files above to get started.');
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
});
