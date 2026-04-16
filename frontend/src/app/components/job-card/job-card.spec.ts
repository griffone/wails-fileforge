import { ComponentFixture, TestBed } from '@angular/core/testing';
import { JobCard } from './job-card';
import { JobResultV1 } from '../../services/wails';

describe('JobCard', () => {
  let fixture: ComponentFixture<JobCard>;
  let component: JobCard;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [JobCard] }).compileComponents();
    fixture = TestBed.createComponent(JobCard);
    component = fixture.componentInstance;
  });

  it('maps uploading payload to uploading stage with bytes info', () => {
    const job: JobResultV1 = {
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
    const job: JobResultV1 = {
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
});
