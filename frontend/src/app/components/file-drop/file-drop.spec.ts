import { ComponentFixture, TestBed } from '@angular/core/testing';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { FileDrop } from './file-drop';

describe('FileDrop', () => {
  let fixture: ComponentFixture<FileDrop>;
  let component: FileDrop;

  beforeEach(async () => {
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ language: 'en' }),
    );
    await TestBed.configureTestingModule({ imports: [FileDrop] }).compileComponents();
    fixture = TestBed.createComponent(FileDrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
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

  it('renders the derived copy in Spanish when the locale is Spanish', () => {
    const i18n = TestBed.inject(I18nService);
    i18n.setLocale('es');
    component.accept = 'image/*';
    fixture.detectChanges();

    expect(surface().getAttribute('aria-label')).toBe(
      'Zona para soltar archivos',
    );
    expect(surface().textContent).toContain('Suelta archivos aquí');
    expect(surface().textContent).toContain('o haz clic para examinar');
    expect(surface().textContent).toContain('Acepta imágenes');
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

  it('exposes a unique native drop target id', () => {
    expect(surface().hasAttribute('data-file-drop-target')).toBeTrue();
    expect(surface().id).toBe(component.dropId);

    const second = TestBed.createComponent(FileDrop);
    second.detectChanges();
    const secondSurface = second.nativeElement.querySelector('.file-drop') as HTMLElement;
    expect(secondSurface.id).not.toBe(surface().id);
  });

  it('emits native dropped paths for its own drop target', () => {
    const spy = jasmine.createSpy('pathsSelected');
    component.pathsSelected.subscribe(spy);

    component.handleNativeDrop({
      data: {
        droppedFiles: ['/tmp/one.png', '/tmp/two.png'],
        dropTargetDetails: { id: component.dropId },
      },
    });

    expect(spy).toHaveBeenCalledOnceWith(['/tmp/one.png', '/tmp/two.png']);
    expect(component.dragOver).toBeFalse();
  });

  it('accepts a native payload without a target id', () => {
    const spy = jasmine.createSpy('pathsSelected');
    component.pathsSelected.subscribe(spy);

    component.handleNativeDrop({ data: { files: ['C:\\tmp\\one.png'] } });

    expect(spy).toHaveBeenCalledOnceWith(['C:\\tmp\\one.png']);
  });

  it('ignores native drops aimed at another drop target', () => {
    const spy = jasmine.createSpy('pathsSelected');
    component.pathsSelected.subscribe(spy);

    component.handleNativeDrop({
      data: {
        droppedFiles: ['/tmp/one.png'],
        dropTargetDetails: { id: 'ff-drop-999' },
      },
    });

    expect(spy).not.toHaveBeenCalled();
  });

  it('emits paths from browser drops when files expose a webview path', () => {
    const spy = jasmine.createSpy('pathsSelected');
    component.pathsSelected.subscribe(spy);
    const file = Object.assign(new File(['x'], 'a.png', { type: 'image/png' }), {
      path: '/tmp/a.png',
    });

    component['emitPaths']({
      item: (i: number) => (i === 0 ? file : null),
      length: 1,
    } as unknown as FileList);

    expect(spy).toHaveBeenCalledOnceWith(['/tmp/a.png']);
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

  it('asks the host for a native picker when nativePicker is enabled', () => {
    const browse = jasmine.createSpy('browseRequested');
    const inputClick = spyOn(component['fileInput']!.nativeElement, 'click');
    component.browseRequested.subscribe(browse);
    component.nativePicker = true;
    fixture.detectChanges();

    surface().click();

    expect(browse).toHaveBeenCalledTimes(1);
    expect(inputClick).not.toHaveBeenCalled();
  });

  it('renders a custom headline', () => {
    component.title = 'Drop images here';
    fixture.detectChanges();

    expect(surface().textContent).toContain('Drop images here');
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
