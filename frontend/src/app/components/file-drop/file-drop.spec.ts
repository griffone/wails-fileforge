import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FileDrop } from './file-drop';

describe('FileDrop', () => {
  let fixture: ComponentFixture<FileDrop>;
  let component: FileDrop;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [FileDrop] }).compileComponents();
    fixture = TestBed.createComponent(FileDrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  function surface(): HTMLElement {
    return fixture.nativeElement.querySelector('.file-drop') as HTMLElement;
  }

  it('should emit filesSelected when input change', () => {
    const spy = jasmine.createSpy('filesSelected');
    component.filesSelected.subscribe(spy);
    const file = new File(['x'], 'a.pdf', { type: 'application/pdf' });
    component['emitFiles']({ item: (i: number) => (i === 0 ? file : null), length: 1 } as unknown as FileList);
    expect(spy).toHaveBeenCalled();
  });

  it('derives a readable format hint from accept', () => {
    component.accept = '.pdf,application/pdf';
    fixture.detectChanges();

    expect(component.formatsHint()).toBe('PDF');
    expect(surface().textContent).toContain('Accepts PDF');
  });

  it('keeps a custom hint over the derived formats', () => {
    component.accept = 'image/*';
    component.hint = 'Drop a PDF export here';
    fixture.detectChanges();

    const hint = fixture.nativeElement.querySelector('.file-drop-hint') as HTMLElement;
    expect(hint.textContent?.trim()).toBe('Drop a PDF export here');
  });

  it('wires aria-describedby to the hint line', () => {
    const hint = fixture.nativeElement.querySelector('.file-drop-hint') as HTMLElement;
    expect(surface().getAttribute('role')).toBe('button');
    expect(surface().getAttribute('aria-describedby')).toBe(hint.id);
    expect(hint.id).toBe(component.hintId);
  });

  it('opens the picker on Space without scrolling', () => {
    const trigger = spyOn(component, 'triggerFileInput');
    const event = new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true });

    surface().dispatchEvent(event);

    expect(event.defaultPrevented).toBeTrue();
    expect(trigger).toHaveBeenCalledTimes(1);
  });

  it('opens the picker on Enter', () => {
    const trigger = spyOn(component, 'triggerFileInput');
    surface().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));

    expect(trigger).toHaveBeenCalledTimes(1);
  });

  it('reflects the drag state on the surface', () => {
    surface().dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true }));
    fixture.detectChanges();

    expect(component.dragOver).toBeTrue();
    expect(surface().classList).toContain('is-dragging');
    expect(fixture.nativeElement.classList).toContain('is-dragging');

    surface().dispatchEvent(new DragEvent('dragleave', { bubbles: true, cancelable: true }));
    fixture.detectChanges();

    expect(component.dragOver).toBeFalse();
    expect(surface().classList).not.toContain('is-dragging');
    expect(fixture.nativeElement.classList).not.toContain('is-dragging');
  });
});
