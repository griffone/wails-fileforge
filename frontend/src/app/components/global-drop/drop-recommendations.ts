import type { ToolDefinition } from '../../config/tools';
import { fileExtension, fileName, formatCount } from '../../utils/format';

const IMAGE_EXTENSIONS = new Set([
  'jpg',
  'jpeg',
  'png',
  'gif',
  'webp',
  'bmp',
  'tiff',
  'tif',
]);

const VIDEO_EXTENSIONS = new Set(['mp4', 'mov', 'mkv', 'webm']);

const MARKDOWN_EXTENSIONS = new Set(['md', 'markdown']);

export interface DropFileSummary {
  total: number;
  /** Names shown in the overlay (at most four). */
  names: string[];
  /** Files hidden behind a "+ N more" row. */
  more: number;
  /** Uppercase, de-duplicated extensions in first-seen order. */
  extensions: string[];
  countLabel: string;
  typeLabel: string;
  /** Files no tool can handle. */
  unsupported: number;
}

export function isSupportedDropPath(path: string): boolean {
  const extension = extensionOf(path);
  return (
    IMAGE_EXTENSIONS.has(extension) ||
    VIDEO_EXTENSIONS.has(extension) ||
    MARKDOWN_EXTENSIONS.has(extension) ||
    extension === 'pdf' ||
    extension === 'docx'
  );
}

export function summarizeDropFiles(paths: readonly string[]): DropFileSummary {
  const names = paths.slice(0, 4).map((path) => fileName(path) || path);
  const extensions: string[] = [];
  let unsupported = 0;

  for (const path of paths) {
    const extension = extensionOf(path);
    const label = extension ? extension.toUpperCase() : 'FILE';
    if (!extensions.includes(label)) {
      extensions.push(label);
    }
    if (!isSupportedDropPath(path)) {
      unsupported += 1;
    }
  }

  return {
    total: paths.length,
    names,
    more: Math.max(0, paths.length - names.length),
    extensions,
    countLabel: formatCount(paths.length, 'file'),
    typeLabel: extensions.length > 0 ? extensions.join(' · ') : 'No files',
    unsupported,
  };
}

/**
 * Maps the dropped files to the tools that can open them, preserving the
 * registry order. Merge tools only appear when there is something to merge.
 */
export function recommendDropTools(
  paths: readonly string[],
  tools: readonly ToolDefinition[],
): ToolDefinition[] {
  let imageCount = 0;
  let pdfCount = 0;
  let videoCount = 0;
  let markdownCount = 0;
  let docxCount = 0;

  for (const path of paths) {
    const extension = extensionOf(path);
    if (IMAGE_EXTENSIONS.has(extension)) {
      imageCount += 1;
    } else if (VIDEO_EXTENSIONS.has(extension)) {
      videoCount += 1;
    } else if (MARKDOWN_EXTENSIONS.has(extension)) {
      markdownCount += 1;
    } else if (extension === 'pdf') {
      pdfCount += 1;
    } else if (extension === 'docx') {
      docxCount += 1;
    }
  }

  const routes = new Set<string>();

  if (imageCount > 0) {
    routes.add('/image-converter');
    routes.add('/image-crop');
    routes.add('/image-annotate');
    routes.add('/image-compress');
    routes.add('/image-watermark');
    routes.add('/image-ocr');
    routes.add('/image-background');
    routes.add('/pdf-from-images');
  }

  if (pdfCount > 0) {
    if (pdfCount >= 2) {
      routes.add('/pdf-merge');
    }
    routes.add('/pdf-split');
    routes.add('/pdf-crop');
    routes.add('/pdf-optimize');
    routes.add('/pdf-to-docx');
  }

  if (markdownCount > 0) {
    routes.add('/doc-md-to-pdf');
  }

  if (docxCount > 0) {
    routes.add('/doc-docx-to-pdf');
  }

  if (videoCount > 0) {
    routes.add('/video-convert');
    routes.add('/video-trim');
    if (videoCount >= 2) {
      routes.add('/video-merge');
    }
  }

  return tools.filter((tool) => routes.has(tool.route));
}

function extensionOf(path: string): string {
  return fileExtension(path).toLowerCase();
}
