import { ComponentFixture, TestBed } from '@angular/core/testing';
import {
  JobRequest,
  JobResult,
  PlanJobResponse,
  VerificationReport,
  Wails,
} from '../../services/wails';
import { RecentConversionsService } from '../../services/recent-conversions.service';
import {
  LAST_REQUEST_STORAGE_PREFIX,
  LastRequestService,
} from '../../services/last-request.service';
import { I18nService } from '../../services/i18n.service';
import { PREFERENCES_STORAGE_KEY } from '../../services/preferences.service';
import { ClipboardService } from '../../services/clipboard.service';
import { ResultCard } from './result-card';

describe('ResultCard', () => {
  let fixture: ComponentFixture<ResultCard>;
  let component: ResultCard;
  let recentSpy: jasmine.SpyObj<RecentConversionsService>;
  let wailsSpy: jasmine.SpyObj<Wails>;
  let clipboardSpy: jasmine.SpyObj<ClipboardService>;

  function clearLastRequests(): void {
    try {
      const keys: string[] = [];
      for (let index = 0; index < localStorage.length; index += 1) {
        const key = localStorage.key(index);
        if (key?.startsWith(LAST_REQUEST_STORAGE_PREFIX)) {
          keys.push(key);
        }
      }
      for (const key of keys) {
        localStorage.removeItem(key);
      }
    } catch {
      // Storage is available in Karma; guard keeps the spec resilient.
    }
  }

  beforeEach(async () => {
    clearLastRequests();
    localStorage.setItem(
      PREFERENCES_STORAGE_KEY,
      JSON.stringify({ language: 'en' }),
    );
    recentSpy = jasmine.createSpyObj<RecentConversionsService>(
      'RecentConversionsService',
      ['record'],
    );
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'verifyJob',
      'listJobHistory',
      'undoJob',
      'restoreJob',
      'emptyTrash',
      'listRecipes',
      'planRecipe',
      'runRecipe',
      'getRecipeStatus',
      'cancelRecipe',
      'planJob',
      'runJob',
    ]);

    wailsSpy.verifyJob.and.resolveTo({
      success: false,
      message: 'no outputs to verify',
      outputs: [],
    });
    wailsSpy.undoJob.and.resolveTo({
      success: true,
      message: 'Moved 1 file to the app trash',
      moved: ['/tmp/a.webp'],
      skipped: [],
    });
    wailsSpy.restoreJob.and.resolveTo({
      success: true,
      message: 'Restored 1 file',
      moved: ['/tmp/a.webp'],
      skipped: [],
    });
    clipboardSpy = jasmine.createSpyObj<ClipboardService>('ClipboardService', [
      'writeText',
    ]);
    clipboardSpy.writeText.and.resolveTo(true);

    await TestBed.configureTestingModule({
      imports: [ResultCard],
      providers: [
        { provide: RecentConversionsService, useValue: recentSpy },
        { provide: Wails, useValue: wailsSpy },
        { provide: ClipboardService, useValue: clipboardSpy },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(ResultCard);
    component = fixture.componentInstance;
  });

  afterEach(() => {
    TestBed.inject(I18nService).setLocale('en');
    localStorage.removeItem(PREFERENCES_STORAGE_KEY);
    clearLastRequests();
  });

  function job(overrides: Partial<JobResult>): JobResult {
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

  function pdfJob(overrides: Partial<JobResult> = {}): JobResult {
    return job({
      toolId: 'tool.pdf.optimize',
      items: [
        {
          inputPath: '/tmp/source.pdf',
          outputPath: '/tmp/out/a.pdf',
          success: true,
          message: 'ok',
        },
      ],
      ...overrides,
    });
  }

  function lastRequest(
    overrides: Partial<JobRequest> = {},
  ): JobRequest {
    return {
      toolId: 'tool.pdf.optimize',
      mode: 'single',
      inputPaths: ['/tmp/source.pdf'],
      outputDir: '/tmp/out',
      options: { compress: false, metadata: { title: 'Draft' } },
      ...overrides,
    };
  }

  function planFor(request: JobRequest): PlanJobResponse {
    return {
      success: true,
      message: 'ok',
      outputs: request.inputPaths.map((inputPath, index) => ({
        inputPath,
        outputPath: `/tmp/out/a_${index + 1}_optimized.pdf`,
        outputKind: 'file',
        exists: false,
        adjusted: false,
      })),
      warnings: [],
    };
  }

  function savedRequest(): JobRequest {
    const request = lastRequest();
    TestBed.inject(LastRequestService).save(request);
    return request;
  }

  function verificationReport(
    overrides: Partial<VerificationReport> = {},
  ): VerificationReport {
    return {
      success: true,
      message: 'verified 1 output',
      outputs: [
        {
          path: '/tmp/out/a.pdf',
          exists: true,
          sizeBytes: 8388608,
          actualBytes: 8388608,
          targetBytes: 10 * 1024 * 1024,
          kind: 'pdf',
          checks: [
            { key: 'exists', label: 'Exists', passed: true, value: 'true' },
            { key: 'page_count', label: 'Pages', passed: true, value: '12' },
            {
              key: 'target_size',
              label: 'Target size',
              passed: true,
              advisory: true,
              value: '8 MB / 10 MB',
            },
          ],
        },
      ],
      ...overrides,
    };
  }

  it('renders nothing without a job', () => {
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.result-card')).toBeNull();
    expect(recentSpy.record).not.toHaveBeenCalled();
  });

  it('records a terminal job once and ignores repeat renders', () => {
    component.job = job({ jobId: 'job-42' });
    fixture.detectChanges();

    expect(recentSpy.record).toHaveBeenCalledOnceWith(
      jasmine.objectContaining({ jobId: 'job-42' }),
    );

    component.job = job({ jobId: 'job-42' });
    fixture.detectChanges();

    expect(recentSpy.record).toHaveBeenCalledTimes(1);
  });

  it('does not record a non-terminal or missing job', () => {
    component.job = job({ jobId: 'job-running', status: 'running' });
    fixture.detectChanges();
    component.job = null;
    fixture.detectChanges();

    expect(recentSpy.record).not.toHaveBeenCalled();
  });

  it('records a new job id on the same card', () => {
    component.job = job({ jobId: 'job-a' });
    fixture.detectChanges();
    component.job = job({ jobId: 'job-b' });
    fixture.detectChanges();

    expect(recentSpy.record).toHaveBeenCalledTimes(2);
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
    expect(text).toContain("Some files couldn't be converted. Retry them below.");
    expect(fixture.nativeElement.querySelector('.result-retry')).not.toBeNull();
  });

  it('prefers a supplied partialDetail over the retry copy', () => {
    component.partialDetail =
      'Result created, but it is 12.4 MB: above the 10 MB target.';
    component.job = job({
      jobId: 'job-partial-detail',
      status: 'partial_success',
      toolId: 'recipe.email_pdf',
      items: [
        {
          inputPath: '/tmp/source.pdf',
          outputPath: '/tmp/out/a.pdf',
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    const detail = fixture.nativeElement.querySelector(
      '.result-card .panel-sub',
    ) as HTMLElement;
    expect(detail.textContent).toContain(
      'Result created, but it is 12.4 MB: above the 10 MB target.',
    );
    expect(fixture.nativeElement.textContent).not.toContain(
      'Retry them below',
    );
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

  it('uses verbPast in the success summary', () => {
    component.toolLabel = 'PDF';
    component.verbPast = 'split';
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/a_pages',
          outputs: ['/tmp/a_pages/page_001.pdf'],
          outputCount: 1,
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.result-summary')?.textContent).toContain(
      '1 PDF split',
    );
  });

  it('groups large output sets into one folder row', () => {
    const revealSpy = jasmine.createSpy('reveal');
    const openSpy = jasmine.createSpy('open');
    component.reveal.subscribe(revealSpy);
    component.open.subscribe(openSpy);
    component.toolLabel = 'PDF';
    component.verbPast = 'split';
    const outputs = Array.from(
      { length: 6 },
      (_, index) => `/tmp/out/a_pages/page_${index + 1}.pdf`,
    );
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/out/a_pages',
          outputs,
          outputCount: 6,
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    const rows = fixture.nativeElement.querySelectorAll('.result-row');
    expect(rows.length).toBe(1);
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('6 files in a_pages');
    expect(text).toContain('/tmp/out/a_pages');
    expect(text).not.toContain('page_1.pdf');
    expect(fixture.nativeElement.querySelector('.result-open')).toBeNull();

    const showFolder = fixture.nativeElement.querySelector(
      '.result-show-folder',
    ) as HTMLButtonElement;
    expect(showFolder.textContent).toContain('Show in folder');
    showFolder.click();

    expect(revealSpy).toHaveBeenCalledOnceWith('/tmp/out/a_pages');
    expect(openSpy).not.toHaveBeenCalled();
  });

  it('expands every output through the show-all toggle', () => {
    component.toolLabel = 'PDF';
    component.verbPast = 'split';
    const outputs = Array.from(
      { length: 6 },
      (_, index) => `/tmp/out/a_pages/page_${index + 1}.pdf`,
    );
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/out/a_pages',
          outputs,
          outputCount: 6,
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    const toggle = fixture.nativeElement.querySelector(
      '.result-toggle-outputs',
    ) as HTMLButtonElement;
    expect(toggle.textContent).toContain('Show all 6 outputs');
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
    expect(fixture.nativeElement.querySelectorAll('.result-row').length).toBe(1);

    toggle.click();
    fixture.detectChanges();

    expect(toggle.textContent).toContain('Show fewer outputs');
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    expect(fixture.nativeElement.querySelectorAll('.result-row').length).toBe(6);
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('page_1.pdf');
    expect(text).toContain('page_6.pdf');
    expect(text).not.toContain('6 files in a_pages');

    toggle.click();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelectorAll('.result-row').length).toBe(1);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
  });

  it('keeps individual rows for four or fewer outputs', () => {
    component.toolLabel = 'PDF';
    component.verbPast = 'split';
    const outputs = Array.from(
      { length: 4 },
      (_, index) => `/tmp/out/a_pages/page_${index + 1}.pdf`,
    );
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/out/a_pages',
          outputs,
          outputCount: 4,
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelectorAll('.result-row').length).toBe(4);
    expect(fixture.nativeElement.querySelector('.result-show-folder')).toBeNull();
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('page_1.pdf');
    expect(text).toContain('page_4.pdf');
  });

  it('shows a directory count instead of a bogus size without file info', () => {
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/out/a_pages',
          outputCount: 3,
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.result-meta')?.textContent).toContain(
      '3 files',
    );
    expect(fixture.nativeElement.textContent).not.toContain('0 B');
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

  it('renders a supplied verification report without fetching', async () => {
    component.verification = verificationReport();
    component.job = pdfJob({ jobId: 'job-verified' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.verifyJob).not.toHaveBeenCalled();

    const block = fixture.nativeElement.querySelector(
      '.verify-block',
    ) as HTMLElement;
    expect(block).not.toBeNull();
    expect(block.textContent).toContain('Verified');
    expect(block.textContent).toContain('2/2 checks passed');
    expect(block.textContent).toContain('a.pdf');
    expect(block.textContent).toContain('8 MB');
    expect(block.textContent).toContain('Checks passed: 2/2');
    expect(block.textContent).toContain('Pages · 12');
    expect(block.textContent).toContain('8 MB / target 10 MB');
    expect(block.textContent).not.toContain('Target size ·');
  });

  it('fetches verification for a normal terminal job and renders the checks', async () => {
    wailsSpy.verifyJob.and.resolveTo(verificationReport());
    component.job = pdfJob({ jobId: 'job-fetch' });
    fixture.detectChanges();

    expect(wailsSpy.verifyJob).toHaveBeenCalledOnceWith('job-fetch');

    await fixture.whenStable();
    fixture.detectChanges();

    const text = fixture.nativeElement.querySelector('.verify-block')
      ?.textContent as string;
    expect(text).toContain('Verified');
    expect(text).toContain('Pages · 12');
    expect(text).toContain('Exists · true');
  });

  it('renders the target comparison when the output carries target bytes', async () => {
    const actualBytes = 8598323;
    component.verification = {
      success: true,
      message: 'verified 1 output',
      outputs: [
        {
          path: '/tmp/out/a.pdf',
          exists: true,
          sizeBytes: actualBytes,
          actualBytes,
          targetBytes: 10 * 1024 * 1024,
          kind: 'pdf',
          checks: [
            { key: 'exists', label: 'Exists', passed: true, value: 'true' },
            {
              key: 'target_size',
              label: 'Target size',
              passed: true,
              advisory: true,
              value: '8.2 MB / 10 MB',
            },
          ],
        },
      ],
    };
    component.job = pdfJob({ jobId: 'job-target-met' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const targetRow = fixture.nativeElement.querySelector(
      '.verify-output-target',
    ) as HTMLElement | null;
    expect(targetRow).not.toBeNull();
    expect(targetRow?.textContent).toContain('8.2 MB / target 10 MB');
    expect(targetRow?.textContent).toContain('met');
    expect(
      targetRow?.classList.contains('verify-output-target-failed'),
    ).toBeFalse();
    expect(fixture.nativeElement.textContent).toContain('Verified');
    expect(fixture.nativeElement.textContent).toContain('Checks passed: 1/1');
    expect(fixture.nativeElement.textContent).not.toContain('Target size ·');
  });

  it('keeps a missed target verified and never marks the output as failed', async () => {
    component.verification = {
      success: true,
      message: 'verified 1 output with 1 advisory warning',
      warnings: ['/tmp/out/a.pdf: 2 MB exceeds the 1 MB target'],
      outputs: [
        {
          path: '/tmp/out/a.pdf',
          exists: true,
          sizeBytes: 2 * 1024 * 1024,
          actualBytes: 2 * 1024 * 1024,
          targetBytes: 1024 * 1024,
          kind: 'pdf',
          checks: [
            { key: 'exists', label: 'Exists', passed: true, value: 'true' },
            {
              key: 'target_size',
              label: 'Target size',
              passed: false,
              advisory: true,
              value: '2 MB / 1 MB',
              detail: 'output is 2 MB; target is 1 MB',
            },
          ],
        },
      ],
    };
    component.job = pdfJob({ jobId: 'job-target-missed' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const block = fixture.nativeElement.querySelector(
      '.verify-block',
    ) as HTMLElement;
    const targetRow = block.querySelector(
      '.verify-output-target',
    ) as HTMLElement | null;
    expect(targetRow).not.toBeNull();
    expect(targetRow?.textContent).toContain('2 MB / target 1 MB');
    expect(targetRow?.textContent).toContain('over target');
    expect(
      targetRow?.classList.contains('verify-output-target-failed'),
    ).toBeTrue();

    expect(block.textContent).toContain('Verified');
    expect(block.textContent).not.toContain('Needs attention');
    expect(block.textContent).toContain('Checks passed: 1/1');
    expect(block.querySelector('.verify-output-failed')).toBeNull();
    expect(block.querySelector('.verify-check-failed')).toBeNull();
    expect(block.textContent).not.toContain('Target size ·');
    expect(block.textContent).toContain('exceeds the 1 MB target');
  });

  it('does not render a target row when the job had no target', async () => {
    component.verification = {
      success: true,
      message: 'verified 1 output',
      outputs: [
        {
          path: '/tmp/out/a.pdf',
          exists: true,
          sizeBytes: 8388608,
          kind: 'pdf',
          checks: [
            { key: 'exists', label: 'Exists', passed: true, value: 'true' },
            { key: 'page_count', label: 'Pages', passed: true, value: '12' },
          ],
        },
      ],
    };
    component.job = pdfJob({ jobId: 'job-without-target' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(
      fixture.nativeElement.querySelector('.verify-output-target'),
    ).toBeNull();
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Verified');
    expect(text).toContain('Pages · 12');
  });

  it('does not fabricate a pass when verification is unavailable', async () => {
    wailsSpy.verifyJob.and.resolveTo({
      success: false,
      message: 'verification service unavailable',
      outputs: [],
    });
    component.job = pdfJob({ jobId: 'job-unavailable' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const block = fixture.nativeElement.querySelector(
      '.verify-block',
    ) as HTMLElement;
    expect(block.textContent).toContain(
      'Verification unavailable for this job.',
    );
    expect(block.textContent).not.toContain('Verified');
    expect(block.querySelector('.verify-icon')).toBeNull();
  });

  it('does not fetch verification for recipe jobs', async () => {
    component.job = pdfJob({
      jobId: 'recipe-job-1',
      toolId: 'recipe.email_pdf',
    });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.verifyJob).not.toHaveBeenCalled();
    expect(fixture.nativeElement.querySelector('.verify-block')).toBeNull();
  });

  it('skips recent conversions when recordRecent is false', () => {
    component.recordRecent = false;
    component.job = pdfJob({ jobId: 'job-no-recent' });
    fixture.detectChanges();

    expect(recentSpy.record).not.toHaveBeenCalled();
  });

  it('undoes a job, then restores it from the swapped action', async () => {
    component.job = pdfJob({ jobId: 'job-undo' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    let button = fixture.nativeElement.querySelector(
      '.result-undo',
    ) as HTMLButtonElement;
    expect(button.textContent).toContain('Undo');

    button.click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.undoJob).toHaveBeenCalledOnceWith('job-undo');
    button = fixture.nativeElement.querySelector(
      '.result-undo',
    ) as HTMLButtonElement;
    expect(button.textContent).toContain('Restore');
    expect(fixture.nativeElement.textContent).toContain(
      'Moved 1 file to the app trash',
    );

    button.click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.restoreJob).toHaveBeenCalledOnceWith('job-undo');
    button = fixture.nativeElement.querySelector(
      '.result-undo',
    ) as HTMLButtonElement;
    expect(button.textContent).toContain('Undo');
    expect(fixture.nativeElement.textContent).toContain('Restored 1 file');
  });

  it('keeps the Undo action and reports skipped files when undo fails', async () => {
    wailsSpy.undoJob.and.resolveTo({
      success: false,
      message: 'Could not undo the job.',
      moved: [],
      skipped: [
        { path: '/tmp/a.webp', reason: 'modified after conversion' },
      ],
    });
    component.job = job({
      jobId: 'job-undo-failed',
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
    await fixture.whenStable();
    fixture.detectChanges();

    const button = fixture.nativeElement.querySelector(
      '.result-undo',
    ) as HTMLButtonElement;
    button.click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(
      (fixture.nativeElement.querySelector('.result-undo') as HTMLButtonElement)
        .textContent,
    ).toContain('Undo');
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Could not undo the job.');
    expect(text).toContain('a.webp');
    expect(text).toContain('modified after conversion');
  });

  it('renders the success summary and details in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    component.toolLabel = 'PDF';
    component.job = job({
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/out/a.pdf',
          success: true,
          message: 'ok',
        },
      ],
    });
    fixture.detectChanges();

    const summary = fixture.nativeElement.querySelector(
      '.result-summary',
    ) as HTMLElement;
    expect(summary.textContent).toContain('Completado: 1 PDF');

    const details = fixture.nativeElement.querySelector(
      '.result-details',
    ) as HTMLElement;
    expect(details.textContent).toContain('Detalles');
    expect(details.textContent).toContain('ID del trabajo');
    expect(details.textContent).toContain('Estado');
    expect(details.textContent).toContain('Inicio');
    expect(details.textContent).toContain('Fin');
    expect(details.textContent).toContain('Completado');
  });

  it('localizes the failed summary and tool-error copy in Spanish', () => {
    TestBed.inject(I18nService).setLocale('es');
    component.job = job({
      status: 'failed',
      items: [
        {
          inputPath: '/tmp/b.png',
          outputPath: '',
          success: false,
          message: 'output exists',
          error: {
            code: 'IMAGE_OUTPUT_EXISTS',
            message: 'output already exists',
          },
        },
      ],
    });
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('No se pudo completar la conversión');
    expect(text).toContain(
      'FileForge no lo sobrescribirá. Elige otro nombre o carpeta.',
    );
    expect(text).not.toContain("FileForge won't overwrite it");
  });

  it('hides run again when no request was stored for the tool', () => {
    component.job = pdfJob({ jobId: 'job-no-request' });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.result-run-again')).toBeNull();
  });

  it('hides run again when the stored request has no inputs', () => {
    localStorage.setItem(
      `${LAST_REQUEST_STORAGE_PREFIX}tool.pdf.optimize`,
      JSON.stringify({
        schemaVersion: 1,
        toolId: 'tool.pdf.optimize',
        submittedAt: 1,
        request: lastRequest({ inputPaths: [] }),
      }),
    );
    component.job = pdfJob({ jobId: 'job-empty-inputs' });
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.result-run-again')).toBeNull();
  });

  it('shows the confirmation with the tool, input count and destinations', async () => {
    const request = savedRequest();
    wailsSpy.planJob.and.resolveTo(planFor(request));
    component.toolLabel = 'PDF';
    component.job = pdfJob({ jobId: 'job-confirm' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    const button = fixture.nativeElement.querySelector(
      '.result-run-again',
    ) as HTMLButtonElement;
    expect(button).not.toBeNull();
    button.click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.planJob).toHaveBeenCalledOnceWith(request);
    expect(wailsSpy.runJob).not.toHaveBeenCalled();

    const block = fixture.nativeElement.querySelector(
      '.run-again-block',
    ) as HTMLElement;
    expect(block.textContent).toContain('Run again with the same settings?');
    expect(block.textContent).toContain('PDF Optimize');
    expect(block.textContent).toContain('1 PDF');
    expect(block.textContent).toContain('/tmp/out/a_1_optimized.pdf');
  });

  it('shows a plan failure inline and never runs the job', async () => {
    const request = savedRequest();
    wailsSpy.planJob.and.resolveTo({
      success: false,
      message: 'options.compress is invalid',
      outputs: [],
      error: {
        code: 'VALIDATION_INVALID_INPUT',
        detail_code: 'PDF_OPTIMIZE_OPTIONS_INVALID',
        message: 'options.compress is invalid',
      },
    });
    component.job = pdfJob({ jobId: 'job-plan-failure' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    (fixture.nativeElement.querySelector(
      '.result-run-again',
    ) as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.runJob).not.toHaveBeenCalled();
    expect(fixture.nativeElement.querySelector('.run-again-block')).not.toBeNull();
    const error = fixture.nativeElement.querySelector(
      '.run-again-error',
    ) as HTMLElement;
    expect(error.textContent).toContain('options.compress is invalid');
    expect(fixture.nativeElement.querySelector('.run-again-block')).not.toBeNull();
  });

  it('runs once on confirm with the stored options and inputs', async () => {
    const request = savedRequest();
    wailsSpy.planJob.and.resolveTo(planFor(request));
    wailsSpy.runJob.and.resolveTo({
      success: true,
      message: 'queued',
      jobId: 'job-again-9',
      status: 'queued',
    });
    const startedSpy = jasmine.createSpy('runAgainStarted');
    component.runAgainStarted.subscribe(startedSpy);
    component.job = pdfJob({ jobId: 'job-confirm-run' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    (fixture.nativeElement.querySelector(
      '.result-run-again',
    ) as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();

    (fixture.nativeElement.querySelector(
      '.result-run-again-confirm',
    ) as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(wailsSpy.runJob).toHaveBeenCalledTimes(1);
    expect(wailsSpy.runJob).toHaveBeenCalledOnceWith(request);
    expect(startedSpy).toHaveBeenCalledOnceWith(
      jasmine.objectContaining({
        jobId: 'job-again-9',
        request,
      }),
    );
    expect(fixture.nativeElement.querySelector('.run-again-block')).toBeNull();
  });

  it('localizes the run again confirmation in Spanish', async () => {
    TestBed.inject(I18nService).setLocale('es');
    const request = savedRequest();
    wailsSpy.planJob.and.resolveTo(planFor(request));
    component.job = pdfJob({ jobId: 'job-again-es' });
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    (fixture.nativeElement.querySelector(
      '.result-run-again',
    ) as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();

    const block = fixture.nativeElement.querySelector(
      '.run-again-block',
    ) as HTMLElement;
    expect(block.textContent).toContain(
      '¿Ejecutar de nuevo con la misma configuración?',
    );
    expect(block.textContent).toContain('Destino planificado');
    expect(block.textContent).toContain('Ejecutar de nuevo');
  });

  describe('context menu', () => {
    function outputRow(index = 0): HTMLElement {
      return fixture.nativeElement.querySelectorAll(
        '.result-list:not(.result-failures) .result-row',
      )[index] as HTMLElement;
    }

    function failedRow(index = 0): HTMLElement {
      return fixture.nativeElement.querySelectorAll('.result-row-failed')[
        index
      ] as HTMLElement;
    }

    function item(id: string): HTMLButtonElement {
      return fixture.nativeElement.querySelector(
        `.context-menu-item[data-item-id="${id}"]`,
      ) as HTMLButtonElement;
    }

    function menu(): HTMLElement | null {
      return fixture.nativeElement.querySelector('.context-menu') as HTMLElement | null;
    }

    function labels(): string[] {
      return Array.from(
        fixture.nativeElement.querySelectorAll('.context-menu-label'),
      ).map((label) => (label as HTMLElement).textContent?.trim() ?? '');
    }

    function rightClick(target: HTMLElement, x = 42, y = 24): MouseEvent {
      const event = new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: x,
        clientY: y,
      });
      target.dispatchEvent(event);
      fixture.detectChanges();
      return event;
    }

    function rect(left: number, bottom: number): DOMRect {
      return {
        top: bottom - 40,
        bottom,
        height: 40,
        left,
        right: left + 200,
        width: 200,
        x: left,
        y: bottom - 40,
        toJSON: () => ({}),
      } as DOMRect;
    }

    function partialJob(): JobResult {
      return job({
        status: 'partial_success',
        toolId: 'tool.pdf.optimize',
        items: [
          {
            inputPath: '/tmp/source.pdf',
            outputPath: '/tmp/out/a.pdf',
            outputs: ['/tmp/out/a.pdf'],
            success: true,
            message: 'ok',
          },
          {
            inputPath: '/tmp/broken.pdf',
            outputPath: '',
            success: false,
            message: 'failed',
            error: {
              code: 'UNSUPPORTED_FORMAT',
              message: 'unsupported input',
            },
          },
        ],
      });
    }

    it('opens the output menu at the pointer with Open, Reveal and Copy path', () => {
      const openSpy = jasmine.createSpy('open');
      component.open.subscribe(openSpy);
      component.job = partialJob();
      fixture.detectChanges();

      const row = outputRow();
      const clickSpy = jasmine.createSpy('click');
      row.addEventListener('click', clickSpy);

      const event = rightClick(row, 64, 90);

      expect(event.defaultPrevented).toBeTrue();
      expect(clickSpy).not.toHaveBeenCalled();
      expect(openSpy).not.toHaveBeenCalled();

      const element = menu();
      expect(element?.getAttribute('role')).toBe('menu');
      expect(element?.style.left).toBe('64px');
      expect(element?.style.top).toBe('90px');
      expect(labels()).toEqual(['Open', 'Reveal in folder', 'Copy path']);
    });

    it('opens the failed-row menu with a Retry item', () => {
      component.job = partialJob();
      fixture.detectChanges();

      rightClick(failedRow());

      expect(labels()).toEqual([
        'Open',
        'Reveal in folder',
        'Copy path',
        'Retry',
      ]);
      expect(item('remove')).toBeNull();
    });

    it('reuses the existing open, reveal and retry paths', async () => {
      const openSpy = jasmine.createSpy('open');
      const revealSpy = jasmine.createSpy('reveal');
      const retrySpy = jasmine.createSpy('retry');
      component.open.subscribe(openSpy);
      component.reveal.subscribe(revealSpy);
      component.retryFailed.subscribe(retrySpy);
      component.job = partialJob();
      fixture.detectChanges();

      rightClick(outputRow());
      item('open').click();
      await fixture.whenStable();

      rightClick(outputRow());
      item('reveal').click();
      await fixture.whenStable();

      rightClick(failedRow());
      item('retry').click();

      expect(openSpy).toHaveBeenCalledOnceWith('/tmp/out/a.pdf');
      expect(revealSpy).toHaveBeenCalledOnceWith('/tmp/out/a.pdf');
      expect(retrySpy).toHaveBeenCalledTimes(1);
    });

    it('opens the input file from a failed row', () => {
      const openSpy = jasmine.createSpy('open');
      component.open.subscribe(openSpy);
      component.job = partialJob();
      fixture.detectChanges();

      rightClick(failedRow());
      item('open').click();

      expect(openSpy).toHaveBeenCalledOnceWith('/tmp/broken.pdf');
    });

    it('disables Open for a collapsed folder summary but keeps reveal and copy', () => {
      component.job = job({
        toolId: 'tool.pdf.split',
        items: [
          {
            inputPath: '/tmp/source.pdf',
            outputPath: '/tmp/out',
            outputCount: 8,
            outputs: [
              '/tmp/out/1.pdf',
              '/tmp/out/2.pdf',
              '/tmp/out/3.pdf',
              '/tmp/out/4.pdf',
              '/tmp/out/5.pdf',
              '/tmp/out/6.pdf',
              '/tmp/out/7.pdf',
              '/tmp/out/8.pdf',
            ],
            success: true,
            message: 'ok',
          },
        ],
      });
      fixture.detectChanges();

      const grouped = fixture.nativeElement.querySelector(
        '.result-row-grouped',
      ) as HTMLElement;
      expect(grouped).not.toBeNull();

      rightClick(grouped);

      expect(item('open').disabled).toBeTrue();
      expect(item('reveal').disabled).toBeFalse();
      expect(item('copyPath').disabled).toBeFalse();
    });

    it('does not act on an action that does not apply', () => {
      const openSpy = jasmine.createSpy('open');
      const revealSpy = jasmine.createSpy('reveal');
      component.open.subscribe(openSpy);
      component.reveal.subscribe(revealSpy);
      component.job = job({
        status: 'failed',
        toolId: 'tool.pdf.optimize',
        items: [
          {
            inputPath: '',
            outputPath: '',
            success: false,
            message: 'failed',
            error: { code: 'EXEC_IO_TRANSIENT', message: 'io' },
          },
        ],
      });
      fixture.detectChanges();

      rightClick(failedRow());

      expect(item('open').disabled).toBeTrue();
      expect(item('reveal').disabled).toBeTrue();
      expect(item('copyPath').disabled).toBeTrue();
      expect(item('retry').disabled).toBeFalse();

      item('open').click();
      item('copyPath').click();

      expect(openSpy).not.toHaveBeenCalled();
      expect(revealSpy).not.toHaveBeenCalled();
      expect(clipboardSpy.writeText).not.toHaveBeenCalled();
    });

    it('copies through ClipboardService and reports failure in a danger banner', async () => {
      clipboardSpy.writeText.and.resolveTo(false);
      component.job = partialJob();
      fixture.detectChanges();

      rightClick(outputRow());
      item('copyPath').click();
      await fixture.whenStable();
      fixture.detectChanges();

      expect(clipboardSpy.writeText).toHaveBeenCalledWith('/tmp/out/a.pdf');
      const banner = fixture.nativeElement.querySelector(
        '.result-copy-banner',
      ) as HTMLElement;
      expect(banner.classList).toContain('banner-danger');
      expect(banner.textContent).toContain('The path could not be copied');
    });

    it('opens from Shift+F10 on the focused output row', () => {
      component.job = partialJob();
      fixture.detectChanges();

      const row = outputRow();
      row.focus();
      spyOn(row, 'getBoundingClientRect').and.returnValue(rect(18, 72));

      row.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'F10',
          shiftKey: true,
          bubbles: true,
          cancelable: true,
        }),
      );
      fixture.detectChanges();

      expect(menu()?.style.left).toBe('18px');
      expect(menu()?.style.top).toBe('72px');
    });

    it('closes on Escape and restores focus to the row', () => {
      component.job = partialJob();
      fixture.detectChanges();

      const row = outputRow();
      rightClick(row);

      menu()?.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'Escape',
          bubbles: true,
          cancelable: true,
        }),
      );
      fixture.detectChanges();

      expect(menu()).toBeNull();
      expect(document.activeElement).toBe(row);
    });

    it('renders the Spanish menu copy', () => {
      TestBed.inject(I18nService).setLocale('es');
      component.job = partialJob();
      fixture.detectChanges();

      rightClick(failedRow());

      expect(labels()).toEqual([
        'Abrir',
        'Mostrar en la carpeta',
        'Copiar ruta',
        'Reintentar',
      ]);
    });
  });
});
