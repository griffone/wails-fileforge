import {
  fileDir,
  fileExtension,
  fileName,
  fileStem,
  formatBytes,
  formatCount,
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
