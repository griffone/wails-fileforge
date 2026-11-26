import { ComponentFixture, TestBed } from '@angular/core/testing';

import { VideoTargetReport } from './video-target-report';
import { I18nService } from '../../services/i18n.service';
import { JobResult } from '../../services/wails';

describe('VideoTargetReport', () => {
  let fixture: ComponentFixture<VideoTargetReport>;
  let component: VideoTargetReport;

  function jobWithDetails(
    details: Record<string, unknown> | null,
  ): JobResult {
    return {
      jobId: 'job-1',
      success: true,
      message: 'done',
      toolId: 'tool.video.convert',
      status: 'success',
      progress: {
        current: 1,
        total: 1,
        stage: 'done',
        message: 'Finished',
      },
      items: [
        {
          inputPath: '/tmp/in/clip.mov',
          outputPath: '/tmp/in/clip_converted.mp4',
          outputs: ['/tmp/in/clip_converted.mp4'],
          success: true,
          message: 'Converted',
          details,
        },
      ],
      startedAt: 1,
      endedAt: 2,
    };
  }

  function metDetails(): Record<string, unknown> {
    return {
      targetSizeBytes: 9_961_472,
      actualSizeBytes: 8_900_000,
      targetBitrateKbps: 1480,
      floorKbps: 800,
      floorApplied: false,
      targetMet: true,
      attempts: 1,
    };
  }

  function panel(): HTMLElement | null {
    return fixture.nativeElement.querySelector('.video-target-panel');
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [VideoTargetReport],
    }).compileComponents();

    TestBed.inject(I18nService).setLocale('en');
    fixture = TestBed.createComponent(VideoTargetReport);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    fixture.destroy();
    TestBed.inject(I18nService).setLocale('en');
  });

  it('renders actual size, bitrate and the met state from the details map', () => {
    component.job = jobWithDetails(metDetails());
    fixture.detectChanges();

    const element = panel();
    expect(element).not.toBeNull();
    const text = element?.textContent ?? '';
    expect(text).toContain('8.5 MB of 9.5 MB target · 1480 kbps');
    expect(text).toContain('Target met');
    expect(text).not.toContain('Not met');
    expect(
      element?.querySelector('.video-target-state')?.classList.contains(
        'is-met',
      ),
    ).toBeTrue();
    expect(element?.querySelector('.video-target-floor')).toBeNull();
  });

  it('reports a best-effort miss and the applied floor', () => {
    component.job = jobWithDetails({
      ...metDetails(),
      actualSizeBytes: 12_000_000,
      targetBitrateKbps: 1200,
      floorApplied: true,
      targetMet: false,
    });
    fixture.detectChanges();

    const element = panel();
    const text = element?.textContent ?? '';
    expect(text).toContain('11.4 MB of 9.5 MB target · 1200 kbps');
    expect(text).toContain('Not met (best effort)');
    expect(text).toContain('Floor of 800 kbps applied');
    expect(
      element?.querySelector('.video-target-state')?.classList.contains(
        'is-miss',
      ),
    ).toBeTrue();
  });

  it('renders nothing when the item carries no details map', () => {
    component.job = jobWithDetails(null);
    fixture.detectChanges();

    expect(component.rows()).toEqual([]);
    expect(panel()).toBeNull();
  });

  it('ignores a details map without the target measurements', () => {
    component.job = jobWithDetails({ attempts: 2 });
    fixture.detectChanges();

    expect(component.rows()).toEqual([]);
    expect(panel()).toBeNull();
  });

  it('renders no panel for a null job', () => {
    component.job = null;
    fixture.detectChanges();

    expect(panel()).toBeNull();
  });

  it('translates the outcome states in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    component.job = jobWithDetails({
      ...metDetails(),
      floorApplied: true,
      targetMet: false,
    });
    fixture.detectChanges();

    const text = panel()?.textContent ?? '';
    expect(text).toContain('Resultado del objetivo');
    expect(text).toContain('No alcanzado (máximo esfuerzo)');
    expect(text).toContain('Se aplicó el mínimo de 800 kbps');
  });
});
