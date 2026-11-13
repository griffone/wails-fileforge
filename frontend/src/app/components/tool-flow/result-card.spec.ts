import { ComponentFixture, TestBed } from '@angular/core/testing';
import { JobResultV1 } from '../../services/wails';
import { ResultCard } from './result-card';

describe('ResultCard', () => {
  let fixture: ComponentFixture<ResultCard>;
  let component: ResultCard;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [ResultCard] }).compileComponents();
    fixture = TestBed.createComponent(ResultCard);
    component = fixture.componentInstance;
  });

  function job(overrides: Partial<JobResultV1>): JobResultV1 {
    return {
      jobId: 'job-1',
      success: false,
      message: '',
      toolId: 'tool.image.convert',
      status: 'success',
      progress: { current: 1, total: 1, stage: 'done', message: '' },
      items: [],
      startedAt: 1700000000000,
      endedAt: 1700000005000,
      ...overrides,
    };
  }

  it('renders nothing without a job', () => {
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.result-card')).toBeNull();
  });

  it('renders a partial summary and failed descriptions', () => {
    component.job = job({
      status: 'partial_success',
      items: [
        {
          inputPath: '/tmp/a.png',
          outputPath: '/tmp/a.webp',
          success: true,
          message: 'ok',
        },
        {
          inputPath: '/tmp/b.png',
          outputPath: '/tmp/b.webp',
          success: true,
          message: 'ok',
        },
        {
          inputPath: '/tmp/c.txt',
          outputPath: '',
          success: false,
          message: 'unsupported',
          error: {
            code: 'UNSUPPORTED_FORMAT',
            message: 'unsupported input',
          },
        },
      ],
    });
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(fixture.nativeElement.querySelector('.result-summary')?.textContent).toContain(
      '2 of 3 converted',
    );
    expect(text).toContain('c.txt');
    expect(text).toContain('Choose a supported format or a different file.');
    expect(fixture.nativeElement.querySelector('.result-retry')).not.toBeNull();
  });

  it('emits open and reveal for an output row', () => {
    const openSpy = jasmine.createSpy('open');
    const revealSpy = jasmine.createSpy('reveal');
    component.open.subscribe(openSpy);
    component.reveal.subscribe(revealSpy);
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.png',
          outputPath: '',
          outputs: ['/tmp/a.webp'],
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    (fixture.nativeElement.querySelector('.result-open') as HTMLButtonElement).click();
    (fixture.nativeElement.querySelector('.result-reveal') as HTMLButtonElement).click();

    expect(openSpy).toHaveBeenCalledOnceWith('/tmp/a.webp');
    expect(revealSpy).toHaveBeenCalledOnceWith('/tmp/a.webp');
  });

  it('uses the tool label and file info size in the success summary', () => {
    component.toolLabel = 'image';
    component.fileInfos = {
      '/tmp/a.webp': {
        path: '/tmp/a.webp',
        name: 'a.webp',
        directory: '/tmp',
        extension: 'webp',
        sizeBytes: 1536,
        kind: 'image',
        exists: true,
      },
    };
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.png',
          outputPath: '/tmp/a.webp',
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.result-summary')?.textContent).toContain(
      '1 image converted',
    );
    expect(fixture.nativeElement.querySelector('.result-meta')?.textContent).toContain(
      '1.5 KB',
    );
  });

  it('emits reset from Convert more', () => {
    const spy = jasmine.createSpy('reset');
    component.reset.subscribe(spy);
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.png',
          outputPath: '/tmp/a.webp',
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    (fixture.nativeElement.querySelector('.result-reset') as HTMLButtonElement).click();

    expect(spy).toHaveBeenCalledTimes(1);
  });
});
