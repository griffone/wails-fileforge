import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ImageConverter } from './image-converter';

describe('ImageConverter', () => {
  let component: ImageConverter;
  let fixture: ComponentFixture<ImageConverter>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ImageConverter]
    })
    .compileComponents();

    fixture = TestBed.createComponent(ImageConverter);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
});
