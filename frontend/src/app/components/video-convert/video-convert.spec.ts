import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { VideoConvert } from './video-convert';
import { JobRequestV1, Wails } from '../../services/wails';

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
      'subscribeJobProgressV1',
      'getPdfPreviewSource',
      'isRuntimeAvailable',
    ]);

    // return an unsubscribe spy so we can assert it's called on destroy
    const unsubscribeSpy = jasmine.createSpy('unsubscribe');
    wailsSpy.subscribeJobProgressV1.and.returnValue(unsubscribeSpy);

    await TestBed.configureTestingModule({
      imports: [VideoConvert],
      providers: [provideRouter([]), { provide: Wails, useValue: wailsSpy }],
    }).compileComponents();

    fixture = TestBed.createComponent(VideoConvert);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('unsubscribes from job progress on destroy', () => {
    (component as any).unsubscribeProgressEvent = wailsSpy.subscribeJobProgressV1(() => {});
    fixture.destroy();

    expect((wailsSpy.subscribeJobProgressV1 as jasmine.Spy).calls.count()).toBeGreaterThan(0);
    const returned = (wailsSpy.subscribeJobProgressV1 as jasmine.Spy).calls.mostRecent().returnValue as jasmine.Spy;
    expect(returned).toHaveBeenCalled();
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

});
