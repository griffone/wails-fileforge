import { describeToolError, looksTechnicalMessage } from './tool-error';

describe('describeToolError', () => {
  it('maps output collision detail codes to changeName', () => {
    const copy = describeToolError({
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'IMAGE_OUTPUT_EXISTS',
      message: 'output exists',
    });

    expect(copy.title).toBe('A file with that name already exists.');
    expect(copy.description).toContain("won't overwrite");
    expect(copy.action).toBe('changeName');
    expect(copy.technical).toContain('IMAGE_OUTPUT_EXISTS');
  });

  it('maps output directory codes to chooseFolder', () => {
    const copy = describeToolError({
      code: 'EXEC_IO_TRANSIENT',
      detail_code: 'PDF_SPLIT_OUTPUT_DIR_NOT_WRITABLE',
      message: 'permission denied',
    });

    expect(copy.title).toBe("That folder can't be used.");
    expect(copy.action).toBe('chooseFolder');
  });

  it('maps missing inputs to a non-actionable message', () => {
    const copy = describeToolError({
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'INPUT_NOT_FOUND',
      message: 'missing file',
    });

    expect(copy.title).toBe("That file couldn't be found.");
    expect(copy.action).toBe('none');
  });

  it('maps unsupported format codes that do not end with UNSUPPORTED', () => {
    const copy = describeToolError({
      code: 'UNSUPPORTED_FORMAT',
      message: 'unknown image format',
    });

    expect(copy.title).toBe("That format isn't supported.");
    expect(copy.action).toBe('changeFormat');
  });

  it('maps runtime dependency codes', () => {
    const copy = describeToolError({
      code: 'RUNTIME_DEP_MISSING',
      detail_code: 'VIDEO_FFMPEG_MISSING',
      message: 'ffmpeg not found',
    });

    expect(copy.title).toBe('A required tool is missing.');
    expect(copy.action).toBe('installDependency');
  });

  it('maps a PDF without bookmarks to actionable split guidance', () => {
    const copy = describeToolError({
      code: 'VALIDATION_INVALID_INPUT',
      detail_code: 'PDF_SPLIT_NO_BOOKMARKS',
      message: 'PDF has no bookmarks to split by',
    });

    expect(copy.title).toBe('This PDF has no bookmarks.');
    expect(copy.description).toContain('Choose Every page');
    expect(copy.action).toBe('none');
  });

  it('maps cancellation codes', () => {
    const copy = describeToolError({
      code: 'CANCELLED_BY_USER',
      message: 'job cancelled',
    });

    expect(copy.title).toBe('Cancelled.');
    expect(copy.description).toBe('No files were changed.');
    expect(copy.action).toBe('none');
  });

  it('falls back to message patterns when no code matches', () => {
    const copy = describeToolError({
      code: 'EXEC_IO_TRANSIENT',
      detail_code: 'SOMETHING_NEW',
      message: 'The output file already exists',
    });

    expect(copy.action).toBe('changeName');
  });

  it('hides technical messages behind the generic retry copy', () => {
    const copy = describeToolError({
      code: 'EXEC_IO_TRANSIENT',
      detail_code: 'EXEC_FAILED',
      message:
        'exec /usr/bin/ffmpeg: exit status 1: /tmp/out.webp: invalid argument',
    });

    expect(copy.title).toBe('Something went wrong.');
    expect(copy.description).toBe(
      'Try again. If it keeps happening, open Details.',
    );
    expect(copy.action).toBe('retry');
    expect(copy.technical).toContain('exit status');
  });

  it('uses a plain message directly and honours the fallback copy', () => {
    const plain = describeToolError({
      code: 'EXEC_IO_TRANSIENT',
      detail_code: 'SOMETHING_NEW',
      message: 'The encoder stopped early.',
    });
    expect(plain.description).toBe('The encoder stopped early.');

    const empty = describeToolError(null, 'Drop a file first.');
    expect(empty.title).toBe('Something went wrong.');
    expect(empty.description).toBe('Drop a file first.');
    expect(empty.action).toBe('retry');
  });
});

describe('looksTechnicalMessage', () => {
  it('detects paths and process output', () => {
    expect(looksTechnicalMessage('/home/ivan/photo.png is locked')).toBeTrue();
    expect(looksTechnicalMessage('ffmpeg exited')).toBeTrue();
    expect(looksTechnicalMessage('C:\\Users\\ivan\\out.pdf is locked')).toBeTrue();
  });

  it('keeps short human copy non-technical', () => {
    expect(looksTechnicalMessage('That file is not a PDF.')).toBeFalse();
  });
});
