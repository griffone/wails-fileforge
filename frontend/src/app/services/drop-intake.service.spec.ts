import { TestBed } from '@angular/core/testing';

import { DropIntakeService } from './drop-intake.service';

describe('DropIntakeService', () => {
  let service: DropIntakeService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(DropIntakeService);
  });

  it('returns queued paths only for the matching route', () => {
    service.queue('/image-crop', ['/tmp/a.png', '/tmp/b.png']);

    expect(service.take('/image-converter')).toEqual([]);
    expect(service.take('/image-crop')).toEqual(['/tmp/a.png', '/tmp/b.png']);
  });

  it('clears the intake after a successful take', () => {
    service.queue('/pdf-merge', ['/tmp/a.pdf']);

    expect(service.take('/pdf-merge')).toEqual(['/tmp/a.pdf']);
    expect(service.take('/pdf-merge')).toEqual([]);
  });

  it('normalises routes, trims paths and drops blanks and duplicates', () => {
    service.queue('/video-trim/', ['  /tmp/clip.mp4  ', '', '/tmp/clip.mp4', '/tmp/other.mp4']);

    expect(service.take('video-trim')).toEqual(['/tmp/clip.mp4', '/tmp/other.mp4']);
  });

  it('replaces a previous pending intake', () => {
    service.queue('/image-converter', ['/tmp/one.png']);
    service.queue('/pdf-split', ['/tmp/two.pdf']);

    expect(service.take('/image-converter')).toEqual([]);
    expect(service.take('/pdf-split')).toEqual(['/tmp/two.pdf']);
  });

  it('ignores an intake without paths', () => {
    service.queue('/image-converter', ['  ', '']);

    expect(service.take('/image-converter')).toEqual([]);
  });
});
