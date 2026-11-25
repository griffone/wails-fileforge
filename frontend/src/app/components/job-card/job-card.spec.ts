import { ComponentFixture, TestBed } from '@angular/core/testing';
import { JobCard } from './job-card';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { JobResult } from '../../services/wails';

describe('JobCard', () => {
  let fixture: ComponentFixture<JobCard>;
  let component: JobCard;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [JobCard] }).compileComponents();
    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(JobCard);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
  });

  it('maps uploading payload to uploading stage with bytes info', () => {
    const job: JobResult = {
      jobId: 'j1',
      success: false,
      message: 'uploading',
      toolId: 't1',
      status: 'running',
      progress: { current: 0, total: 0, stage: 'uploading', message: 'sending', etaSeconds: 30 } as any,
      items: [],
      startedAt: Date.now(),
    };
    // inject bytes
    (job.progress as any).bytesTransferred = 512;
    (job.progress as any).bytesTotal = 1024;
    component.job = job;
    fixture.detectChanges();

    expect(component.currentStage()).toBe('uploading');
    expect(component.bytesInfo()).toContain('512');
    expect(component.overallPercent()).toBe(0);
  });

  it('maps processing payload to processing stage with item counts', () => {
    const job: JobResult = {
      jobId: 'j2',
      success: false,
      message: 'processing',
      toolId: 't1',
      status: 'running',
      progress: { current: 2, total: 5, stage: 'processing', message: 'working' },
      items: [
        { inputPath: '/a', outputPath: '/a', success: true, message: 'ok' } as any,
        { inputPath: '/b', outputPath: '/b', success: false, message: 'fail' } as any,
      ],
      startedAt: Date.now(),
    };

    component.job = job;
    fixture.detectChanges();

    expect(component.currentStage()).toBe('processing');
    expect(component.processingInfo()).toContain('2');
    expect(component.overallPercent()).toBe(Math.round((2 / 5) * 100));
  });

  it('renders the stage, fallback message and ETA in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    component.job = {
      jobId: 'j3',
      success: false,
      message: '',
      toolId: 't1',
      status: 'running',
      progress: {
        current: 1,
        total: 2,
        stage: 'processing',
        message: '',
        etaSeconds: 90,
      },
      items: [],
      startedAt: Date.now(),
    };
    fixture.detectChanges();

    const stage = fixture.nativeElement.querySelector('.job-card-stage') as HTMLElement;
    expect(stage.textContent).toContain('Procesando');

    const message = fixture.nativeElement.querySelector('.job-card-message') as HTMLElement;
    expect(message.textContent).toContain('Trabajando en tus archivos');

    const eta = fixture.nativeElement.querySelector('.job-card-eta') as HTMLElement;
    expect(eta.textContent).toContain('Tiempo restante: 1m 30s');

    expect(component.progressValueText()).toBe('Procesando 50%');
  });
});
