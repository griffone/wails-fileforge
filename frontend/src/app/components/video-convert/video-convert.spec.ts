import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { VideoConvert } from './video-convert';
import { JobRequestV1, JobResultV1, Wails } from '../../services/wails';

describe('VideoConvert', () => {
  let component: VideoConvert;
  let fixture: any;
  let wailsSpy: jasmine.SpyObj<Wails>;

  beforeEach(async () => {
    wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'validateJobV1',
      'runJobV1',
      'getJobStatusV1',
      'cancelJobV1',
      'listToolsV1',
      'openFileDialog',
      'openMultipleFilesDialog',
      'openDirectoryDialog',
      'getPdfPreviewSource',
      'isRuntimeAvailable',
    ]);

    await TestBed.configureTestingModule({
      imports: [VideoConvert],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoConvert);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('shapes single payload for tool.video.convert', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.form.patchValue({
      jobMode: 'single',
      outputPath: '/tmp/out/video.webm',
      targetFormat: 'webm',
      qualityPreset: 'high',
    });
    component.selectedInputPaths = ['/tmp/input.mov'];

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.convert');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/input.mov']);
    expect(req.options['outputPath']).toBe('/tmp/out/video.webm');
    expect(req.options['targetFormat']).toBe('webm');
    expect(req.options['qualityPreset']).toBe('high');
  });

  it('shapes batch payload for tool.video.convert', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.form.patchValue({
      jobMode: 'batch',
      outputDir: '/tmp/out',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
    });
    component.selectedInputPaths = ['/tmp/a.mov', '/tmp/b.mkv'];

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.convert');
    expect(req.mode).toBe('batch');
    expect(req.inputPaths).toEqual(['/tmp/a.mov', '/tmp/b.mkv']);
    expect(req.outputDir).toBe('/tmp/out');
    expect(req.options['targetFormat']).toBe('mp4');
    expect(req.options['qualityPreset']).toBe('medium');
    expect(req.options['outputPath']).toBeUndefined();
  });

  it('triggers merge chain when batch has >=2 outputs', async () => {
    component.form.patchValue({
      jobMode: 'batch',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
      mergeOutputs: 'yes',
      mergeOutputPath: '/tmp/out/merged.mp4',
      mergeMode: 'auto',
    });

    const convertResult: JobResultV1 = {
      jobId: 'convert-job',
      success: true,
      message: 'done',
      toolId: 'tool.video.convert',
      status: 'success',
      progress: { current: 2, total: 2, stage: 'success', message: 'done' },
      items: [
        {
          inputPath: '/tmp/a.mov',
          outputPath: '/tmp/out/a_converted.mp4',
          outputs: ['/tmp/out/a_converted.mp4'],
          outputCount: 1,
          success: true,
          message: 'ok',
        },
        {
          inputPath: '/tmp/b.mov',
          outputPath: '/tmp/out/b_converted.mp4',
          outputs: ['/tmp/out/b_converted.mp4'],
          outputCount: 1,
          success: true,
          message: 'ok',
        },
      ],
      startedAt: Date.now(),
      endedAt: Date.now(),
    };

    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );
    wailsSpy.runJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'submitted', jobId: 'merge-job', status: 'queued' })
    );

    await (component as any).handleConvertTerminal(convertResult);

    const mergeValidateReq = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(mergeValidateReq.toolId).toBe('tool.video.merge');
    expect(mergeValidateReq.mode).toBe('single');
    expect(mergeValidateReq.inputPaths.length).toBe(2);
    expect(component['activeJobKind']).toBe('merge');
    expect(component.mergeChainMessage).toContain('Merge chain started');
  });

  it('does not trigger merge chain when batch has <2 outputs', async () => {
    component.form.patchValue({ jobMode: 'batch', mergeOutputs: 'yes' });

    const convertResult: JobResultV1 = {
      jobId: 'convert-job',
      success: true,
      message: 'done',
      toolId: 'tool.video.convert',
      status: 'success',
      progress: { current: 1, total: 1, stage: 'success', message: 'done' },
      items: [
        {
          inputPath: '/tmp/a.mov',
          outputPath: '/tmp/out/a_converted.mp4',
          outputs: ['/tmp/out/a_converted.mp4'],
          outputCount: 1,
          success: true,
          message: 'ok',
        },
      ],
      startedAt: Date.now(),
      endedAt: Date.now(),
    };

    await (component as any).handleConvertTerminal(convertResult);

    expect(wailsSpy.validateJobV1).not.toHaveBeenCalled();
    expect(wailsSpy.runJobV1).not.toHaveBeenCalled();
    expect(component.mergeChainMessage).toContain('skipped');
  });
});
