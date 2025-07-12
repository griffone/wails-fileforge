import { TestBed } from '@angular/core/testing';

import { Wails } from './wails';

describe('Wails', () => {
  let service: Wails;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(Wails);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
});
