import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FileDrop } from './file-drop';
import { By } from '@angular/platform-browser';

describe('FileDrop', () => {
  let fixture: ComponentFixture<FileDrop>;
  let component: FileDrop;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [FileDrop],
    }).compileComponents();

    fixture = TestBed.createComponent(FileDrop);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('emits file paths when files selected', () => {
    const spy = jasmine.createSpy('files');
    component.filePaths.subscribe(spy);

    const fakeFile = new File(['x'], 'a.txt');
    const input = fixture.debugElement.query(By.css('input')).nativeElement as HTMLInputElement;
    const dt = new DataTransfer();
    dt.items.add(fakeFile);
    input.files = dt.files;

    input.dispatchEvent(new Event('change'));
    fixture.detectChanges();

    expect(spy).toHaveBeenCalled();
    const emitted = spy.calls.mostRecent().args[0] as string[];
    expect(emitted.length).toBe(1);
    expect(emitted[0]).toContain('a.txt');
  });
});
