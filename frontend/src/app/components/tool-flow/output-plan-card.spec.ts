import { ComponentFixture, TestBed } from '@angular/core/testing';
import { PlanJobResponse } from '../../services/wails';
import { OutputPlanCard } from './output-plan-card';

describe('OutputPlanCard', () => {
  let fixture: ComponentFixture<OutputPlanCard>;
  let component: OutputPlanCard;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [OutputPlanCard] }).compileComponents();
    fixture = TestBed.createComponent(OutputPlanCard);
    component = fixture.componentInstance;
  });

  function planWith(outputs: PlanJobResponse['outputs']): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs,
      warnings: [],
    };
  }

  it('renders planned names, directories and the adjusted warning', () => {
    component.plan = planWith([
      {
        inputPath: '/tmp/in/photo.jpg',
        outputPath: '/tmp/in/photo.webp',
        outputKind: 'file',
        exists: false,
        adjusted: true,
      },
      {
        inputPath: '/tmp/in/scan.png',
        outputPath: '/tmp/in/scan.webp',
        outputKind: 'file',
        exists: false,
        adjusted: false,
      },
    ]);
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Save to');
    expect(text).toContain('photo.webp');
    expect(text).toContain('scan.webp');
    expect(text).toContain('/tmp/in');
    expect(text).toContain('Existing names will be adjusted to avoid overwriting.');
    expect(fixture.nativeElement.querySelector('.badge-warning')?.textContent).toContain(
      'Adjusted',
    );
  });

  it('renders directory outputs as folders with their file count', () => {
    component.plan = planWith([
      {
        inputPath: '/tmp/in/a.pdf',
        outputPath: '/tmp/in/a_pages',
        outputKind: 'directory',
        outputCount: 6,
        exists: false,
        adjusted: false,
      },
      {
        inputPath: '/tmp/in/b.pdf',
        outputPath: '/tmp/out/b_pages',
        outputKind: 'directory',
        outputCount: 1,
        exists: false,
        adjusted: false,
      },
    ]);
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('a_pages');
    expect(text).toContain('b_pages');
    expect(fixture.nativeElement.querySelectorAll('.output-plan-item').length).toBe(2);
    expect(
      fixture.nativeElement.querySelectorAll('.output-plan-item .badge-neutral')
        .length,
    ).toBe(2);
    expect(text).toContain('Folder');
    expect(text).toContain('6 files');
    expect(text).toContain('1 file');
  });

  it('makes a single output directory the primary line', () => {
    component.plan = planWith([
      {
        inputPath: '/tmp/in/a.pdf',
        outputPath: '/tmp/in/a_pages',
        outputKind: 'directory',
        outputCount: 12,
        exists: false,
        adjusted: false,
      },
    ]);
    fixture.detectChanges();

    const primary = fixture.nativeElement.querySelector('.output-plan-primary') as HTMLElement;
    expect(primary).not.toBeNull();
    expect(primary.textContent).toContain('a_pages');
    expect(primary.textContent).toContain('/tmp/in');
    expect(primary.textContent).toContain('12 files');
    expect(fixture.nativeElement.querySelector('.output-plan-list')).toBeNull();
  });

  it('collapses extra planned outputs', () => {
    component.plan = planWith(
      Array.from({ length: 6 }, (_, index) => ({
        inputPath: `/tmp/in/${index}.png`,
        outputPath: `/tmp/in/${index}.webp`,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
    );
    fixture.detectChanges();

    const names = fixture.nativeElement.querySelectorAll('.output-plan-name');
    expect(names.length).toBe(4);
    expect(fixture.nativeElement.textContent).toContain('+ 2 more');
  });

  it('shows every output when the visible limit covers them', () => {
    component.visibleLimit = 6;
    component.plan = planWith(
      Array.from({ length: 6 }, (_, index) => ({
        inputPath: `/tmp/in/${index}.png`,
        outputPath: `/tmp/in/${index}.webp`,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
    );
    fixture.detectChanges();

    const names = fixture.nativeElement.querySelectorAll('.output-plan-name');
    expect(names.length).toBe(6);
    expect(fixture.nativeElement.querySelector('.output-plan-more')).toBeNull();
    expect(fixture.nativeElement.textContent).not.toContain('more');
  });

  it('shows the working state while loading', () => {
    component.loading = true;
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Working out the output…');
  });

  it('renders a plain-language banner for plan errors', () => {
    component.plan = {
      success: false,
      message: 'exists',
      outputs: [],
      error: {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'IMAGE_OUTPUT_EXISTS',
        message: 'output exists',
      },
    };
    fixture.detectChanges();

    const banner = fixture.nativeElement.querySelector('.banner-danger') as HTMLElement;
    expect(banner.textContent).toContain('A file with that name already exists.');
    expect(banner.textContent).toContain("FileForge won't overwrite it.");
  });

  it('emits changeDestination from the Change button', () => {
    const spy = jasmine.createSpy('changeDestination');
    component.changeDestination.subscribe(spy);
    fixture.detectChanges();

    (fixture.nativeElement.querySelector('.output-plan-change') as HTMLButtonElement).click();

    expect(spy).toHaveBeenCalledTimes(1);
  });
});
