import { Wails } from './wails';

describe('Wails legacy IPC absence', () => {
  it('does not expose legacy conversion methods', () => {
    const proto = Wails.prototype as unknown as Record<string, unknown>;

    expect(proto['convertFile']).toBeUndefined();
    expect(proto['convertBatch']).toBeUndefined();
    expect(proto['getSupportedFormats']).toBeUndefined();
    expect(proto['getPdfBytes']).toBeUndefined();
    expect(proto['splitPdfBackend']).toBeUndefined();
    expect(typeof proto['registerMediaPath']).toBe('function');
  });
});
