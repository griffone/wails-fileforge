import { TestBed } from '@angular/core/testing';
import { SplitModalComponent } from './split-modal.component';

describe('SplitModalComponent (skeleton)', () => {
  beforeEach(() => TestBed.configureTestingModule({ imports: [SplitModalComponent] }));

  it('should create component', () => {
    const fixture = TestBed.createComponent(SplitModalComponent as any);
    const comp = fixture.componentInstance;
    expect(comp).toBeTruthy();
  });
});
