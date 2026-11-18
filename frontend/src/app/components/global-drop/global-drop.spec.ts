import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';

import { GlobalDrop } from './global-drop';
import { DropIntakeService } from '../../services/drop-intake.service';
import { GlobalDropService } from '../../services/global-drop.service';

describe('GlobalDrop', () => {
  let fixture: ComponentFixture<GlobalDrop>;
  let drop: GlobalDropService;
  let intake: DropIntakeService;
  let router: Router;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [GlobalDrop],
      providers: [provideRouter([])],
    }).compileComponents();

    drop = TestBed.inject(GlobalDropService);
    intake = TestBed.inject(DropIntakeService);
    router = TestBed.inject(Router);
    fixture = TestBed.createComponent(GlobalDrop);
    fixture.detectChanges();
  });

  afterEach(() => {
    fixture.destroy();
  });

  function show(paths: string[]): void {
    drop.show(paths);
    fixture.detectChanges();
  }

  function cardNames(): string[] {
    return Array.from(
      fixture.nativeElement.querySelectorAll('.drop-card-name') as NodeListOf<HTMLElement>,
    ).map((element) => element.textContent?.trim() ?? '');
  }

  function text(selector: string): string {
    return (
      (fixture.nativeElement.querySelector(selector) as HTMLElement | null)?.textContent?.trim() ??
      ''
    );
  }

  it('recommends the image tools for images', () => {
    show(['/tmp/photo.png', '/tmp/scan.jpg']);

    expect(cardNames()).toEqual([
      'Image Convert',
      'Image Crop',
      'Image Annotate',
      'Image Compress',
      'Image Watermark',
      'Images to PDF',
    ]);
    expect(text('.drop-files-count')).toBe('2 files');
    expect(text('.drop-files-types')).toBe('PNG · JPG');
  });

  it('only offers PDF Merge when there are at least two PDFs', () => {
    show(['/tmp/one.pdf']);
    expect(cardNames()).toEqual([
      'PDF Split',
      'PDF Crop',
      'PDF Optimize',
      'PDF to DOCX',
    ]);

    show(['/tmp/one.pdf', '/tmp/two.pdf']);
    expect(cardNames()).toEqual([
      'PDF Merge',
      'PDF Split',
      'PDF Crop',
      'PDF Optimize',
      'PDF to DOCX',
    ]);
  });

  it('recommends the document tools for Markdown and DOCX', () => {
    show(['/tmp/notes.md']);
    expect(cardNames()).toEqual(['Markdown → PDF']);

    show(['/tmp/letter.docx']);
    expect(cardNames()).toEqual(['DOCX → PDF']);
  });

  it('only offers Video Merge when there are at least two videos', () => {
    show(['/tmp/clip.mp4']);
    expect(cardNames()).toEqual(['Video Convert', 'Video Trim']);

    show(['/tmp/clip.mp4', '/tmp/clip.webm']);
    expect(cardNames()).toEqual(['Video Convert', 'Video Trim', 'Video Merge']);
  });

  it('combines the relevant tools for mixed file types', () => {
    show(['/tmp/photo.png', '/tmp/one.pdf', '/tmp/notes.md', '/tmp/clip.mp4']);

    expect(cardNames()).toEqual([
      'Image Convert',
      'Image Crop',
      'Image Annotate',
      'Image Compress',
      'Image Watermark',
      'PDF Split',
      'PDF Crop',
      'PDF Optimize',
      'Markdown → PDF',
      'Video Convert',
      'Video Trim',
      'Images to PDF',
      'PDF to DOCX',
    ]);
  });

  it('summarises the first four names and collapses the rest', () => {
    show(['/tmp/one.png', '/tmp/two.png', '/tmp/three.png', '/tmp/four.png', '/tmp/five.png']);

    const names = Array.from(
      fixture.nativeElement.querySelectorAll('.drop-file-name') as NodeListOf<HTMLElement>,
    ).map((element) => element.textContent?.trim() ?? '');

    expect(names).toEqual(['one.png', 'two.png', 'three.png', 'four.png', '+ 1 more']);
  });

  it('trims, de-duplicates and ignores blanks before summarising', () => {
    show([' /tmp/a.png ', '/tmp/a.png', '', '   ', '/tmp/b.png']);

    expect(drop.paths()).toEqual(['/tmp/a.png', '/tmp/b.png']);
    expect(text('.drop-files-count')).toBe('2 files');
  });

  it('queues the paths and navigates when a tool card is clicked', () => {
    const navigate = spyOn(router, 'navigateByUrl').and.resolveTo(true);
    show(['/tmp/one.pdf', '/tmp/two.pdf']);

    (fixture.nativeElement.querySelector('.drop-card') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(intake.take('/pdf-merge')).toEqual(['/tmp/one.pdf', '/tmp/two.pdf']);
    expect(navigate).toHaveBeenCalledWith('/pdf-merge');
    expect(drop.open()).toBeFalse();
  });

  it('closes from the close button', () => {
    show(['/tmp/photo.png']);

    (fixture.nativeElement.querySelector('.drop-close') as HTMLButtonElement).click();
    fixture.detectChanges();

    expect(drop.open()).toBeFalse();
    expect(fixture.nativeElement.querySelector('.drop-backdrop')).toBeNull();
  });

  it('closes on a backdrop click but not on a panel click', () => {
    show(['/tmp/photo.png']);

    (fixture.nativeElement.querySelector('#global-drop') as HTMLElement).click();
    fixture.detectChanges();
    expect(drop.open()).toBeTrue();

    (fixture.nativeElement.querySelector('.drop-backdrop') as HTMLElement).click();
    fixture.detectChanges();
    expect(drop.open()).toBeFalse();
  });

  it('closes with Escape', () => {
    show(['/tmp/photo.png']);

    document.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }),
    );
    fixture.detectChanges();

    expect(drop.open()).toBeFalse();
  });

  it('shows an empty state for unsupported files', () => {
    show(['/tmp/notes.txt', '/tmp/archive.zip']);

    expect(fixture.nativeElement.querySelector('.drop-card')).toBeNull();
    expect(text('.drop-empty-title')).toBe('No tool handles these files yet.');
    expect(text('.drop-files-warning')).toBe('2 unsupported');

    (fixture.nativeElement.querySelector('.drop-empty .btn') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(drop.open()).toBeFalse();
  });

  it('marks the panel as a native drop target', () => {
    show(['/tmp/photo.png']);

    const panel = fixture.nativeElement.querySelector('#global-drop') as HTMLElement;
    expect(panel.hasAttribute('data-file-drop-target')).toBeTrue();
    expect(panel.getAttribute('role')).toBe('dialog');
  });
});
