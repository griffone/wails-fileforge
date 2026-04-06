import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { VideoTrim } from './video-trim';
import { JobRequestV1, Wails } from '../../services/wails';

describe('VideoTrim', () => {
  let component: VideoTrim;
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
      'subscribeJobProgressV1',
      'getPdfPreviewSource',
      'isRuntimeAvailable',
    ]);

    wailsSpy.subscribeJobProgressV1.and.returnValue(() => undefined);

    await TestBed.configureTestingModule({
      imports: [VideoTrim],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoTrim);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('shapes single payload for tool.video.trim', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.form.patchValue({
      jobMode: 'single',
      outputPath: '/tmp/out/video.webm',
      startTime: '1.25',
      endTime: '5.75',
      targetFormat: 'webm',
      qualityPreset: 'high',
      trimMode: 'copy',
    });
    component.selectedInputPaths = ['/tmp/input.mov'];

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.trim');
    expect(req.mode).toBe('single');
    expect(req.inputPaths).toEqual(['/tmp/input.mov']);
    expect(req.options['outputPath']).toBe('/tmp/out/video.webm');
    expect(req.options['startTime']).toBe(1.25);
    expect(req.options['endTime']).toBe(5.75);
    expect(req.options['targetFormat']).toBe('webm');
    expect(req.options['qualityPreset']).toBe('high');
    expect(req.options['trimMode']).toBe('copy');
  });

  it('shapes batch payload for tool.video.trim', async () => {
    wailsSpy.validateJobV1.and.returnValue(
      Promise.resolve({ success: true, message: 'ok', valid: true })
    );

    component.form.patchValue({
      jobMode: 'batch',
      outputDir: '/tmp/out',
      startTime: '1',
      endTime: '4',
      targetFormat: 'mp4',
      qualityPreset: 'medium',
      trimMode: 'auto',
    });
    component.selectedInputPaths = ['/tmp/a.mov', '/tmp/b.mkv'];

    await component.validate();

    const req = wailsSpy.validateJobV1.calls.mostRecent().args[0] as JobRequestV1;
    expect(req.toolId).toBe('tool.video.trim');
    expect(req.mode).toBe('batch');
    expect(req.inputPaths).toEqual(['/tmp/a.mov', '/tmp/b.mkv']);
    expect(req.outputDir).toBe('/tmp/out');
    expect(req.options['startTime']).toBe(1);
    expect(req.options['endTime']).toBe(4);
    expect(req.options['targetFormat']).toBe('mp4');
    expect(req.options['qualityPreset']).toBe('medium');
    expect(req.options['trimMode']).toBe('auto');
    expect(req.options['outputPath']).toBeUndefined();
  });

});
