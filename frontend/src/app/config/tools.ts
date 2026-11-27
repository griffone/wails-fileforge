import {
  Activity,
  Anvil,
  ArrowUpDown,
  Captions,
  Combine,
  Crop,
  Eraser,
  FileMinus,
  FileText,
  FileType,
  Frame,
  Gauge,
  House,
  Image as ImageIcon,
  ImageUpscale,
  Images,
  Layers,
  LayoutGrid,
  Minimize2,
  PenTool,
  RotateCw,
  ScanText,
  Scissors,
  ScanSearch,
  Stamp,
  Settings2,
  Timer,
  Video,
  Workflow,
  type LucideIconData,
} from 'lucide-angular';

export type ToolCategory =
  | 'workspace'
  | 'image'
  | 'pdf'
  | 'docs'
  | 'video'
  | 'audio'
  | 'system';

/** Sidebar groups include every tool category plus the curated recipes. */
export type NavGroupId = ToolCategory | 'recipes';

/** A single destination in the workbench: a tool, Home or System Status. */
export interface ToolDefinition {
  /** Stable identifier; tool ids mirror the backend toolId values. */
  id: string;
  /** Full product name. */
  name: string;
  /** Short label used by the navigation rail. */
  navName: string;
  /** One sentence describing only capabilities that actually exist. */
  description: string;
  /** Absolute route path, always starting with a slash. */
  route: string;
  category: ToolCategory;
  icon: LucideIconData;
  /** File types or output formats the tool works with. */
  formats: string[];
  /** Extra search terms for the command palette and home search. */
  keywords: string[];
}

export interface ToolCategoryDefinition {
  id: ToolCategory;
  label: string;
}

export interface NavGroup {
  id: NavGroupId;
  label: string;
  items: readonly ToolDefinition[];
}

export const TOOL_CATEGORIES: readonly ToolCategoryDefinition[] = [
  { id: 'workspace', label: 'Workspace' },
  { id: 'image', label: 'Image' },
  { id: 'pdf', label: 'PDF' },
  { id: 'docs', label: 'Documents' },
  { id: 'video', label: 'Video' },
  { id: 'audio', label: 'Audio' },
  { id: 'system', label: 'System' },
];

export const HOME_TOOL: ToolDefinition = {
  id: 'workspace.home',
  name: 'Home',
  navName: 'Home',
  description: 'Search the workbench, open a tool and pick up where you left off.',
  route: '/',
  category: 'workspace',
  icon: House,
  formats: [],
  keywords: ['home', 'start', 'overview', 'search', 'recent', 'workbench'],
};

export const SYSTEM_TOOL: ToolDefinition = {
  id: 'system.status',
  name: 'System Status',
  navName: 'Status',
  description: 'Inspect registered tools, runtime dependencies and backend health.',
  route: '/tool-catalog',
  category: 'system',
  icon: Activity,
  formats: [],
  keywords: ['status', 'health', 'catalog', 'tools', 'runtime', 'dependencies', 'diagnostics'],
};

export const SETTINGS_TOOL: ToolDefinition = {
  id: 'system.settings',
  name: 'Settings',
  navName: 'Settings',
  description: 'Set the default output folder, theme and history preferences.',
  route: '/settings',
  category: 'system',
  icon: Settings2,
  formats: [],
  keywords: ['settings', 'preferences', 'output folder', 'theme', 'history'],
};

export const AUTOMATION_TOOL: ToolDefinition = {
  id: 'system.automations',
  name: 'Automations',
  navName: 'Automations',
  description: 'Watch a folder and run a recipe when a file arrives.',
  route: '/automations',
  category: 'system',
  icon: Workflow,
  formats: [],
  keywords: ['automation', 'watch', 'folder', 'rule', 'recipe'],
};

/**
 * Duplicate scan page. It is a folder-level report, not a tool: no file drop,
 * no job queue and no plan card. Read-only by design.
 */
export const DUPLICATES_TOOL: ToolDefinition = {
  id: 'system.duplicates',
  name: 'Duplicates',
  navName: 'Duplicates',
  description:
    'Scan folders for identical files and see how much space they waste.',
  route: '/duplicates',
  category: 'system',
  icon: ScanSearch,
  formats: [],
  keywords: [
    'duplicates',
    'duplicate',
    'copy',
    'identical',
    'hash',
    'space',
    'wasted',
    'scan',
  ],
};

/** The twenty-eight conversion tools. Ids mirror the backend toolId contract. */
export const TOOLS: readonly ToolDefinition[] = [
  {
    id: 'tool.image.convert',
    name: 'Image Convert',
    navName: 'Convert',
    description: 'Convert PNG, JPG, WebP and GIF images in single or batch mode.',
    route: '/image-converter',
    category: 'image',
    icon: ImageIcon,
    formats: ['PNG', 'JPG', 'WebP', 'GIF'],
    keywords: ['image', 'convert', 'format', 'png', 'jpg', 'jpeg', 'webp', 'gif', 'quality'],
  },
  {
    id: 'tool.image.crop',
    name: 'Image Crop',
    navName: 'Crop',
    description: 'Crop an image to a ratio preset or a custom region with a live preview.',
    route: '/image-crop',
    category: 'image',
    icon: Crop,
    formats: ['PNG', 'JPG', 'WebP'],
    keywords: ['image', 'crop', 'ratio', 'region', 'preview', 'trim'],
  },
  {
    id: 'tool.image.annotate',
    name: 'Image Annotate',
    navName: 'Annotate',
    description: 'Add text, arrows and boxes, then blur or redact regions before export.',
    route: '/image-annotate',
    category: 'image',
    icon: PenTool,
    formats: ['PNG', 'JPG', 'WebP'],
    keywords: ['image', 'annotate', 'text', 'arrow', 'box', 'blur', 'redact', 'markup'],
  },
  {
    id: 'tool.image.compress',
    name: 'Image Compress',
    navName: 'Compress',
    description: 'Compress images to a target file size with optional downscaling.',
    route: '/image-compress',
    category: 'image',
    icon: Minimize2,
    formats: ['PNG', 'JPG', 'WebP'],
    keywords: [
      'image',
      'compress',
      'target size',
      'size limit',
      'shrink',
      'smaller',
      'downscale',
      'optimize',
    ],
  },
  {
    id: 'tool.image.watermark',
    name: 'Image Watermark',
    navName: 'Watermark',
    description: 'Watermark images with text or a logo at nine anchors, with opacity and tiling.',
    route: '/image-watermark',
    category: 'image',
    icon: Stamp,
    formats: ['PNG', 'JPG', 'WebP'],
    keywords: [
      'image',
      'watermark',
      'text watermark',
      'logo',
      'anchor',
      'opacity',
      'tile',
      'tiled',
      'brand',
    ],
  },
  {
    id: 'tool.image.ocr',
    name: 'Image OCR',
    navName: 'Image OCR',
    description:
      'Extract text from images with local Tesseract and optionally write a searchable PDF.',
    route: '/image-ocr',
    category: 'image',
    icon: ScanText,
    formats: ['TXT', 'PDF'],
    keywords: [
      'image',
      'text',
      'extract text',
      'screenshot',
      'scan',
      'tesseract',
      'searchable',
    ],
  },
  {
    id: 'tool.image.background',
    name: 'Remove Background',
    navName: 'Remove BG',
    description:
      'Remove image backgrounds locally and save a transparent PNG or a chosen color.',
    route: '/image-background',
    category: 'image',
    icon: Eraser,
    formats: ['PNG'],
    keywords: [
      'image',
      'background',
      'remove background',
      'transparent',
      'cutout',
      'subject',
      'product photo',
      'rembg',
    ],
  },
  {
    id: 'tool.image.upscale',
    name: 'Upscale Image',
    navName: 'Upscale',
    description:
      'Enlarge images 2x or 4x locally with an optional ONNX model pack.',
    route: '/image-upscale',
    category: 'image',
    icon: ImageUpscale,
    formats: ['PNG', 'JPG', 'WebP'],
    keywords: [
      'image',
      'upscale',
      'enlarge',
      '2x',
      '4x',
      'resolution',
      'real-esrgan',
      'onnx',
    ],
  },
  {
    id: 'tool.pdf.merge',
    name: 'PDF Merge',
    navName: 'Merge',
    description: 'Combine several PDF files into one document in the order you choose.',
    route: '/pdf-merge',
    category: 'pdf',
    icon: Combine,
    formats: ['PDF'],
    keywords: ['pdf', 'merge', 'combine', 'join', 'append', 'order'],
  },
  {
    id: 'tool.pdf.split',
    name: 'PDF Split',
    navName: 'Split',
    description: 'Split a PDF into separate pages, ranges, page chunks or bookmarks.',
    route: '/pdf-split',
    category: 'pdf',
    icon: Scissors,
    formats: ['PDF'],
    keywords: [
      'pdf',
      'split',
      'pages',
      'ranges',
      'extract',
      'chunks',
      'bookmarks',
    ],
  },
  {
    id: 'tool.pdf.crop',
    name: 'PDF Crop',
    navName: 'Crop',
    description: 'Crop PDF pages with margin presets or custom values, single or batch.',
    route: '/pdf-crop',
    category: 'pdf',
    icon: Frame,
    formats: ['PDF'],
    keywords: ['pdf', 'crop', 'margins', 'presets', 'batch'],
  },
  {
    id: 'tool.pdf.optimize',
    name: 'PDF Optimize',
    navName: 'Optimize',
    description: 'Compress PDFs and edit title, author and document metadata.',
    route: '/pdf-optimize',
    category: 'pdf',
    icon: Gauge,
    formats: ['PDF'],
    keywords: [
      'pdf',
      'optimize',
      'compress',
      'metadata',
      'title',
      'author',
      'subject',
      'keywords',
      'creator',
    ],
  },
  {
    id: 'tool.pdf.ocr',
    name: 'PDF OCR',
    navName: 'OCR',
    description: 'Make scanned PDFs searchable with local OCR and a text sidecar.',
    route: '/pdf-ocr',
    category: 'pdf',
    icon: ScanText,
    formats: ['PDF', 'TXT'],
    keywords: ['pdf', 'ocr', 'scan', 'searchable', 'text', 'tesseract', 'recognize'],
  },
  {
    id: 'tool.pdf.rotate',
    name: 'PDF Rotate',
    navName: 'Rotate',
    description: 'Rotate all pages or a page selection by 90, 180 or 270 degrees',
    route: '/pdf-rotate',
    category: 'pdf',
    icon: RotateCw,
    formats: ['PDF'],
    keywords: [
      'pdf',
      'rotate',
      'rotation',
      'turn',
      'pages',
      'angle',
      '90',
      '180',
      '270',
    ],
  },
  {
    id: 'tool.pdf.remove_pages',
    name: 'PDF Remove Pages',
    navName: 'Remove pages',
    description: 'Delete selected pages and keep the rest of the document',
    route: '/pdf-remove-pages',
    category: 'pdf',
    icon: FileMinus,
    formats: ['PDF'],
    keywords: [
      'pdf',
      'pages',
      'delete',
      'remove',
      'remove pages',
      'delete pages',
      'drop pages',
    ],
  },
  {
    id: 'tool.pdf.reorder',
    name: 'PDF Reorder Pages',
    navName: 'Reorder',
    description: 'Rewrite the page order from an explicit spec, or reverse the document',
    route: '/pdf-reorder',
    category: 'pdf',
    icon: ArrowUpDown,
    formats: ['PDF'],
    keywords: [
      'pdf',
      'reorder',
      'order',
      'pages',
      'reverse',
      'move pages',
      'sort pages',
    ],
  },
  {
    id: 'tool.pdf.extract_images',
    name: 'PDF Extract Images',
    navName: 'Extract images',
    description: 'Extract embedded images into a folder with collision-safe names',
    route: '/pdf-extract-images',
    category: 'pdf',
    icon: Images,
    formats: ['PNG', 'JPG', 'JPEG', 'TIF', 'TIFF', 'JP2'],
    keywords: [
      'pdf',
      'images',
      'extract',
      'extract images',
      'pictures',
      'photos',
      'folder',
    ],
  },
  {
    id: 'tool.doc.md_to_pdf',
    name: 'Markdown → PDF',
    navName: 'Markdown',
    description: 'Render Markdown to PDF with an optional header and footer.',
    route: '/doc-md-to-pdf',
    category: 'docs',
    icon: FileText,
    formats: ['MD', 'PDF'],
    keywords: ['markdown', 'md', 'pdf', 'document', 'header', 'footer', 'render'],
  },
  {
    id: 'tool.doc.docx_to_pdf',
    name: 'DOCX → PDF',
    navName: 'DOCX',
    description: 'Convert DOCX documents to PDF in single or batch mode.',
    route: '/doc-docx-to-pdf',
    category: 'docs',
    icon: FileType,
    formats: ['DOCX', 'PDF'],
    keywords: ['docx', 'word', 'pdf', 'document', 'convert', 'batch'],
  },
  {
    id: 'tool.video.convert',
    name: 'Video Convert',
    navName: 'Convert',
    description: 'Convert MP4, MOV, MKV and WebM videos with live progress and ETA.',
    route: '/video-convert',
    category: 'video',
    icon: Video,
    formats: ['MP4', 'MOV', 'MKV', 'WebM'],
    keywords: ['video', 'convert', 'mp4', 'mov', 'mkv', 'webm', 'codec'],
  },
  {
    id: 'tool.video.trim',
    name: 'Video Trim',
    navName: 'Trim',
    description: 'Trim single or batch videos to the start and end time you set.',
    route: '/video-trim',
    category: 'video',
    icon: Timer,
    formats: ['MP4', 'MOV', 'MKV', 'WebM'],
    keywords: ['video', 'trim', 'cut', 'start', 'end', 'clip'],
  },
  {
    id: 'tool.video.merge',
    name: 'Video Merge',
    navName: 'Merge',
    description: 'Join video clips into one file in the order you choose.',
    route: '/video-merge',
    category: 'video',
    icon: Layers,
    formats: ['MP4', 'MOV', 'MKV', 'WebM'],
    keywords: ['video', 'merge', 'join', 'concat', 'clips', 'order'],
  },
  {
    id: 'tool.video.transcribe',
    name: 'Video Transcribe',
    navName: 'Transcribe',
    description: 'Transcribe video and audio locally with Whisper. No uploads.',
    route: '/video-transcribe',
    category: 'video',
    icon: Captions,
    formats: ['SRT', 'VTT', 'TXT'],
    keywords: ['video', 'audio', 'transcribe', 'whisper', 'subtitles', 'srt', 'vtt'],
  },
  {
    id: 'tool.audio.extract',
    name: 'Extract Audio',
    navName: 'Extract',
    description:
      'Extract the audio track from a video or audio file as mp3, m4a, opus, wav or flac',
    route: '/audio-extract',
    category: 'audio',
    icon: Captions,
    formats: ['MP3', 'M4A', 'OPUS', 'WAV', 'FLAC'],
    keywords: [
      'audio',
      'extract',
      'extract audio',
      'audio track',
      'soundtrack',
      'rip audio',
      'video',
      'mp3',
      'm4a',
      'opus',
      'wav',
      'flac',
    ],
  },
  {
    id: 'tool.audio.convert',
    name: 'Audio Convert',
    navName: 'Convert',
    description: 'Convert audio files between mp3, m4a, opus, wav and flac',
    route: '/audio-convert',
    category: 'audio',
    icon: FileType,
    formats: ['MP3', 'M4A', 'OPUS', 'WAV', 'FLAC'],
    keywords: [
      'audio',
      'convert',
      'format',
      'codec',
      'mp3',
      'm4a',
      'opus',
      'wav',
      'flac',
      'bitrate',
    ],
  },
  {
    id: 'tool.audio.normalize',
    name: 'Normalize Audio',
    navName: 'Normalize',
    description:
      'Two-pass EBU R128 loudness normalization to streaming, broadcast or podcast targets',
    route: '/audio-normalize',
    category: 'audio',
    icon: Gauge,
    formats: ['MP3', 'M4A', 'OPUS', 'WAV', 'FLAC'],
    keywords: [
      'audio',
      'normalize',
      'normalise',
      'loudness',
      'lufs',
      'ebu r128',
      'volume',
      'true peak',
      'streaming',
      'broadcast',
      'podcast',
    ],
  },
  {
    id: 'tool.pdf.images',
    name: 'Images to PDF',
    navName: 'Images to PDF',
    description: 'Place several images on a page grid and export one PDF.',
    route: '/pdf-from-images',
    category: 'pdf',
    icon: LayoutGrid,
    formats: ['PDF'],
    keywords: ['images', 'photos', 'pdf', 'grid', 'layout', 'album'],
  },
  {
    id: 'tool.pdf.to_docx',
    name: 'PDF to DOCX',
    navName: 'To DOCX',
    description: 'Convert a PDF into an editable DOCX while preserving fonts, weight, colors, images, tables and page layout.',
    route: '/pdf-to-docx',
    category: 'pdf',
    icon: FileType,
    formats: ['DOCX'],
    keywords: ['pdf', 'docx', 'word', 'layout', 'fidelity', 'editable'],
  },
];

/** Home, every tool, then System Status, Automations, Duplicates and Settings: the full 33-entry navigation model. */
export const NAVIGATION: readonly ToolDefinition[] = [
  HOME_TOOL,
  ...TOOLS,
  SYSTEM_TOOL,
  AUTOMATION_TOOL,
  DUPLICATES_TOOL,
  SETTINGS_TOOL,
];

export const NAV_GROUPS: readonly NavGroup[] = TOOL_CATEGORIES.map((category) => ({
  id: category.id,
  label: category.label,
  items: NAVIGATION.filter((item) => item.category === category.id),
}));

/**
 * Normalises a router URL into the canonical route form used by the registry:
 * strips query/hash, guarantees a leading slash and removes a trailing slash.
 */
export function normalizeToolRoute(route: string | null | undefined): string {
  if (!route) {
    return '/';
  }

  const [path] = route.split(/[?#]/);
  const trimmed = path?.trim() ?? '';

  if (trimmed === '' || trimmed === '/') {
    return '/';
  }

  const withLeadingSlash = trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
  return withLeadingSlash.replace(/\/+$/, '') || '/';
}

/** Brand mark shared by the shell and the empty states. */
export const BRAND_ICON: LucideIconData = Anvil;
