import {
  Activity,
  Anvil,
  Combine,
  Crop,
  FileText,
  FileType,
  Frame,
  Gauge,
  House,
  Image as ImageIcon,
  Layers,
  PenTool,
  Scissors,
  Settings2,
  Timer,
  Video,
  type LucideIconData,
} from 'lucide-angular';

export type ToolCategory = 'workspace' | 'image' | 'pdf' | 'docs' | 'video' | 'system';

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
  id: ToolCategory;
  label: string;
  items: readonly ToolDefinition[];
}

export const TOOL_CATEGORIES: readonly ToolCategoryDefinition[] = [
  { id: 'workspace', label: 'Workspace' },
  { id: 'image', label: 'Image' },
  { id: 'pdf', label: 'PDF' },
  { id: 'docs', label: 'Documents' },
  { id: 'video', label: 'Video' },
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

/** The twelve conversion tools. Ids mirror the backend toolId contract. */
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
];

/** Home, every tool, then System Status and Settings: the full 15-entry navigation model. */
export const NAVIGATION: readonly ToolDefinition[] = [
  HOME_TOOL,
  ...TOOLS,
  SYSTEM_TOOL,
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
