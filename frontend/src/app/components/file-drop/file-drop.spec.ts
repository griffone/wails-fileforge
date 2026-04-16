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

  it('should emit filesSelected when input change', () => {
    const spy = jest.fn();
    component.filesSelected.subscribe(spy);
    const file = new File(['x'], 'a.pdf', { type: 'application/pdf' });
    component['emitFiles']( { item: (i: number) => (i === 0 ? file : null), length: 1 } as unknown as FileList);
    expect(spy).toHaveBeenCalled();
  });
});
