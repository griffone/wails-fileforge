import { ComponentFixture, TestBed } from '@angular/core/testing';
import { FormBuilder } from '@angular/forms';

import {
  ExecutionPanelField,
  ToolExecutionPanel,
} from './tool-execution-panel';

describe('ToolExecutionPanel', () => {
  let fixture: ComponentFixture<ToolExecutionPanel>;
  let component: ToolExecutionPanel;
  const fb = new FormBuilder();

  const fields: ExecutionPanelField[] = [
    {
      controlName: 'mode',
      label: 'Mode',
      type: 'select',
      options: [
        { value: 'single', label: 'Single' },
        { value: 'batch', label: 'Batch' },
      ],
    },
    {
      controlName: 'inputPath',
      label: 'Input',
      type: 'text',
      visibleModes: ['single'],
    },
    {
      controlName: 'batchInputPaths',
      label: 'Batch',
      type: 'textarea',
      visibleModes: ['batch'],
    },
    {
      controlName: 'outputDir',
      label: 'Output',
      type: 'text',
    },
  ];

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ToolExecutionPanel],
    }).compileComponents();

    fixture = TestBed.createComponent(ToolExecutionPanel);
    component = fixture.componentInstance;
    component.form = fb.nonNullable.group({
      mode: ['single'],
      inputPath: ['/tmp/input.txt'],
      batchInputPaths: ['/tmp/a.txt\n/tmp/b.txt'],
      outputDir: ['/tmp'],
    });
    component.fields = fields;
    component.validationMessage = 'Validation OK.';
    component.submitMessage = 'Submitted';
    component.statusMessage = 'running';
    component.canCancel = true;
    component.isPolling = true;
    component.jobResult = {
      jobId: 'job-1',
      success: false,
      message: 'in progress',
      toolId: 'tool.pdf.merge',
      status: 'running',
      progress: {
        current: 1,
        total: 3,
        stage: 'processing',
        message: 'step',
      },
      items: [],
      startedAt: Date.now(),
    };
    fixture.detectChanges();
  });

  it('emits validate, run and cancel actions', () => {
    const validateSpy = jasmine.createSpy('validate');
    const runSpy = jasmine.createSpy('run');
    const cancelSpy = jasmine.createSpy('cancel');

    component.validate.subscribe(validateSpy);
    component.run.subscribe(runSpy);
    component.cancel.subscribe(cancelSpy);

    const buttons = Array.from(
      fixture.nativeElement.querySelectorAll('button')
    ) as HTMLButtonElement[];
    buttons[0].click();
    buttons[1].click();
    buttons[2].click();

    expect(validateSpy).toHaveBeenCalledTimes(1);
    expect(runSpy).toHaveBeenCalledTimes(1);
    expect(cancelSpy).toHaveBeenCalledTimes(1);
  });

  it('renders mode-specific fields using current mode', () => {
    component.currentMode = 'single';
    fixture.detectChanges();

    let input = fixture.nativeElement.querySelector('#inputPath');
    let batch = fixture.nativeElement.querySelector('#batchInputPaths');
    expect(input).toBeTruthy();
    expect(batch).toBeFalsy();

    component.currentMode = 'batch';
    fixture.detectChanges();

    input = fixture.nativeElement.querySelector('#inputPath');
    batch = fixture.nativeElement.querySelector('#batchInputPaths');
    expect(input).toBeFalsy();
    expect(batch).toBeTruthy();
  });

  it('shows visible state blocks and computes rounded progress percent', () => {
    const text = fixture.nativeElement.textContent as string;

    expect(text).toContain('Validation OK.');
    expect(text).toContain('Submitted');
    expect(text).toContain('running');
    expect(text).toContain('Job Status');
    expect(component.progressPercent()).toBe(33);
  });

  it('returns 0 percent when job result is missing or total is zero', () => {
    component.jobResult = null;
    expect(component.progressPercent()).toBe(0);

    component.jobResult = {
      jobId: 'job-2',
      success: true,
      message: 'done',
      toolId: 'tool.pdf.merge',
      status: 'success',
      progress: {
        current: 0,
        total: 0,
        stage: 'done',
        message: 'done',
      },
      items: [],
      startedAt: Date.now(),
      endedAt: Date.now(),
    };
    expect(component.progressPercent()).toBe(0);
  });

  it('returns explicit outputs when present', () => {
    const outputs = component.itemOutputs({
      inputPath: '/tmp/in.pdf',
      outputPath: '/tmp/out-unused.pdf',
      outputs: ['/tmp/out-1.pdf', '/tmp/out-2.pdf'],
      outputCount: 2,
      success: true,
      message: 'ok',
    });

    expect(outputs).toEqual(['/tmp/out-1.pdf', '/tmp/out-2.pdf']);
  });

  it('returns empty array when outputs are absent', () => {
    const outputs = component.itemOutputs({
      inputPath: '/tmp/in.pdf',
      outputPath: '/tmp/out-legacy.pdf',
      success: true,
      message: 'ok',
    });

    expect(outputs).toEqual([]);
  });

  it('renders failed item details and aggregated file errors', () => {
    component.jobResult = {
      jobId: 'job-3',
      success: false,
      message: 'job partial success',
      toolId: 'tool.pdf.crop',
      status: 'partial_success',
      progress: {
        current: 2,
        total: 2,
        stage: 'partial_success',
        message: 'job partial success',
      },
      items: [
        {
          inputPath: '/tmp/a.pdf',
          outputPath: '/tmp/out/a_cropped.pdf',
          success: false,
          message: 'range exceeds page count',
          error: {
            code: 'VALIDATION_INVALID_INPUT',
            detail_code: 'PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS',
            message: 'range exceeds page count',
          },
        },
        {
          inputPath: '/tmp/b.pdf',
          outputPath: '/tmp/out/b_cropped.pdf',
          outputs: ['/tmp/out/b_cropped.pdf'],
          outputCount: 1,
          success: true,
          message: 'ok',
        },
      ],
      error: {
        code: 'EXEC_IO_TRANSIENT',
        detail_code: 'PDF_CROP_FAILED',
        message: 'one or more files failed in batch crop',
        details: {
          fileErrors: [
            {
              path: '/tmp/a.pdf',
              code: 'PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS',
              message: 'range exceeds page count',
            },
          ],
        },
      },
      startedAt: Date.now(),
      endedAt: Date.now(),
    };

    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Item status: failed');
    expect(text).toContain('VALIDATION_INVALID_INPUT [PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS]');
    expect(text).toContain('Batch file errors (aggregated)');
    expect(text).toContain('/tmp/a.pdf');
  });
});
