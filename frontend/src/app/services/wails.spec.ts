import { TestBed } from '@angular/core/testing';

import { Wails } from './wails';

describe('Wails', () => {
  let service: Wails;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(Wails);
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });

  it('should return safe fallback when listToolsV1 fails', async () => {
    spyOn<any>(service, 'callByID').and.rejectWith(new Error('ipc unavailable'));

    const result = await service.listToolsV1();

    expect(result.success).toBeFalse();
    expect(result.tools).toEqual([]);
    expect(result.message).toContain('ipc unavailable');
  });

  it('should return canonical coded fallback when runJobV1 fails', async () => {
    spyOn<any>(service, 'callByID').and.rejectWith(new Error('timeout'));

    const result = await service.runJobV1({
      toolId: 'tool.pdf.merge',
      mode: 'single',
      inputPaths: ['/tmp/input-a.pdf', '/tmp/input-b.pdf'],
      outputDir: '/tmp',
      options: { outputPath: '/tmp/out.pdf' },
    });

    expect(result.success).toBeFalse();
    expect(result.status).toBe('failed');
    expect(result.message).toContain('timeout');
    expect(result.error?.code).toBe('EXEC_IO_TRANSIENT');
    expect(result.error?.detail_code).toBe('IPC_RUN_FAILED');
  });

  it('should return canonical coded fallback when getPdfPreviewSource fails', async () => {
    spyOn<any>(service, 'callByID').and.rejectWith(new Error('ipc down'));

    const result = await service.getPdfPreviewSource('/tmp/in.pdf');

    expect(result.success).toBeFalse();
    expect(result.error?.code).toBe('EXEC_IO_TRANSIENT');
    expect(result.error?.detail_code).toBe('PDF_PREVIEW_READ_FAILED');
    expect(result.error?.message).toContain('ipc down');
  });
});
