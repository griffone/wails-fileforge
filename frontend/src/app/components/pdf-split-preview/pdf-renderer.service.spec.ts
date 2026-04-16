import { TestBed } from '@angular/core/testing';
import { PdfRendererService } from '../../services/pdf-renderer.service';

describe('PdfRendererService (skeleton)', () => {
  let service: PdfRendererService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(PdfRendererService);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
});
