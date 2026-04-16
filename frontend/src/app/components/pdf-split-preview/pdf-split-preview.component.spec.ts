import { ComponentFixture, TestBed } from '@angular/core/testing';
import { PdfSplitPreviewComponent } from './pdf-split-preview.component';

describe('PdfSplitPreviewComponent (skeleton)', () => {
  let component: PdfSplitPreviewComponent;
  let fixture: ComponentFixture<PdfSplitPreviewComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [PdfSplitPreviewComponent],
    }).compileComponents();
    fixture = TestBed.createComponent(PdfSplitPreviewComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
});
