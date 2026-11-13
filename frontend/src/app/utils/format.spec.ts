import {
  fileDir,
  fileExtension,
  fileName,
  fileStem,
  formatBytes,
  formatCount,
  formatDuration,
  parseTimeToSeconds,
} from './format';

describe('formatBytes', () => {
  it('formats zero and tiny values as bytes', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(-4)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
  });

  it('uses binary units with one decimal below 100', () => {
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(1024 * 1024 * 1.2)).toBe('1.2 MB');
    expect(formatBytes(1024 * 1024)).toBe('1 MB');
  });

  it('rounds large values inside a unit', () => {
    expect(formatBytes(840 * 1024)).toBe('840 KB');
  });
});

describe('path helpers', () => {
  it('handles POSIX paths', () => {
    expect(fileName('/home/ivan/photo.png')).toBe('photo.png');
    expect(fileDir('/home/ivan/photo.png')).toBe('/home/ivan');
    expect(fileStem('/home/ivan/photo.png')).toBe('photo');
    expect(fileExtension('/home/ivan/photo.png')).toBe('png');
  });

  it('handles Windows paths', () => {
    expect(fileName('C:\\Users\\ivan\\photo.png')).toBe('photo.png');
    expect(fileDir('C:\\Users\\ivan\\photo.png')).toBe('C:\\Users\\ivan');
    expect(fileStem('C:\\Users\\ivan\\photo.png')).toBe('photo');
    expect(fileExtension('C:\\Users\\ivan\\photo.png')).toBe('png');
  });

  it('keeps dotfiles intact and returns empty values for bare names', () => {
    expect(fileStem('/tmp/.gitignore')).toBe('.gitignore');
    expect(fileExtension('/tmp/.gitignore')).toBe('');
    expect(fileDir('photo.png')).toBe('');
    expect(fileName('/tmp/folder/')).toBe('folder');
    expect(fileDir('C:\\photo.png')).toBe('C:\\');
  });
});

describe('formatCount', () => {
  it('uses singular and plural forms', () => {
    expect(formatCount(1, 'image')).toBe('1 image');
    expect(formatCount(0, 'image')).toBe('0 images');
    expect(formatCount(3, 'image')).toBe('3 images');
  });

  it('supports an explicit plural', () => {
    expect(formatCount(2, 'PDF', 'PDFs')).toBe('2 PDFs');
  });
});

describe('formatDuration', () => {
  it('formats minutes and seconds', () => {
    expect(formatDuration(7)).toBe('0:07');
    expect(formatDuration(83)).toBe('1:23');
    expect(formatDuration(600)).toBe('10:00');
  });

  it('adds hours only when needed', () => {
    expect(formatDuration(3723)).toBe('1:02:03');
    expect(formatDuration(3600)).toBe('1:00:00');
  });

  it('returns an em dash for unknown values', () => {
    expect(formatDuration(0)).toBe('—');
    expect(formatDuration(-5)).toBe('—');
    expect(formatDuration(Number.NaN)).toBe('—');
    expect(formatDuration(Number.POSITIVE_INFINITY)).toBe('—');
  });
});

describe('parseTimeToSeconds', () => {
  it('accepts seconds and decimal seconds', () => {
    expect(parseTimeToSeconds('30')).toBe(30);
    expect(parseTimeToSeconds('30.5')).toBe(30.5);
    expect(parseTimeToSeconds('  0  ')).toBe(0);
  });

  it('accepts MM:SS and HH:MM:SS', () => {
    expect(parseTimeToSeconds('00:30')).toBe(30);
    expect(parseTimeToSeconds('1:23')).toBe(83);
    expect(parseTimeToSeconds('1:02:03')).toBe(3723);
    expect(parseTimeToSeconds('01:02:03.5')).toBe(3723.5);
  });

  it('rejects empty, negative and malformed values', () => {
    expect(parseTimeToSeconds('')).toBeNull();
    expect(parseTimeToSeconds('   ')).toBeNull();
    expect(parseTimeToSeconds('-5')).toBeNull();
    expect(parseTimeToSeconds('-1:00')).toBeNull();
    expect(parseTimeToSeconds('abc')).toBeNull();
    expect(parseTimeToSeconds('1:')).toBeNull();
    expect(parseTimeToSeconds(':30')).toBeNull();
    expect(parseTimeToSeconds('1:2:3:4')).toBeNull();
    expect(parseTimeToSeconds('1:75')).toBeNull();
    expect(parseTimeToSeconds('1:75:00')).toBeNull();
  });
});
