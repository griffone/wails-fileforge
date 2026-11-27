import {
  Archive,
  Captions,
  Clapperboard,
  Images,
  Mail,
  ScanText,
  type LucideIconData,
} from 'lucide-angular';

import { RecipeOptionKind } from '../../../bindings/fileforge-desktop/internal/models/models';
import { Recipe } from '../services/wails';
import type { NavGroup, ToolCategory, ToolDefinition } from './tools';

/**
 * A curated outcome that chains several tools behind one action.
 *
 * Recipes stay out of the tool registry on purpose: they are outcomes, not
 * tools, and the Home tool count keeps describing the underlying bench.
 */
export interface RecipeDefinition {
  /** Stable identifier; mirrors the backend recipe id. */
  id: string;
  /** Absolute route path, always starting with a slash. */
  route: string;
  /** Accepted inputs, e.g. `1+ PDFs`. */
  inputLabel: string;
  /** Final output formats. */
  formats: string[];
  icon: LucideIconData;
  /** Full recipe definition used in preview mode before (or without) IPC. */
  fallback: Recipe;
}

const MEGABYTE = 1024 * 1024;

export const RECIPES: readonly RecipeDefinition[] = [
  {
    id: 'recipe.email_pdf',
    route: '/recipe/email-pdf',
    inputLabel: '1+ PDFs',
    formats: ['PDF'],
    icon: Mail,
    fallback: {
      id: 'recipe.email_pdf',
      name: 'Email PDF',
      description:
        'Merge one or more PDFs and compress the result to a size that is easy to send by email.',
      category: 'documents',
      inputKind: 'pdf',
      acceptExtensions: ['pdf'],
      maxInputs: 0,
      highlights: ['Merge when needed', 'Target size', 'Verified + undo'],
      steps: [
        {
          id: 'merge',
          name: 'Merge PDFs',
          toolId: 'tool.pdf.merge',
          detail: 'Only when there are two or more PDFs',
        },
        {
          id: 'optimize',
          name: 'Optimize to target',
          toolId: 'tool.pdf.optimize',
          detail: 'Ghostscript when the target needs it',
        },
      ],
      defaultTargetSizeBytes: 10 * MEGABYTE,
      duplicable: false,
      optionSchema: [
        {
          key: 'targetSizeBytes',
          label: 'Target size',
          kind: RecipeOptionKind.RecipeOptionInteger,
          unit: 'MB',
          min: 1 * MEGABYTE,
          max: 100 * MEGABYTE,
          scale: MEGABYTE,
          default: 10 * MEGABYTE,
          help: 'Best effort: the output may stay above the target.',
        },
      ],
    },
  },
  {
    id: 'recipe.web_image_set',
    route: '/recipe/web-image-set',
    inputLabel: '1 image',
    formats: ['WEBP', 'JPG'],
    icon: Images,
    fallback: {
      id: 'recipe.web_image_set',
      name: 'Web image set',
      description:
        'Export responsive WebP and JPEG variants with a ZIP bundle and a ready-to-paste srcset snippet.',
      category: 'image',
      inputKind: 'image',
      acceptExtensions: ['png', 'jpg', 'jpeg', 'webp'],
      maxInputs: 1,
      highlights: ['No upscaling', 'WebP + JPEG', 'ZIP + srcset'],
      steps: [
        {
          id: 'build',
          name: 'Build responsive variants',
          toolId: 'tool.image.convert',
          detail: 'WebP and JPEG at the widths your source can support',
        },
        {
          id: 'zip',
          name: 'Create ZIP bundle',
          toolId: '',
          detail: 'All variants plus the snippet in one download',
        },
        {
          id: 'snippet',
          name: 'Write srcset snippet',
          toolId: '',
          detail: 'Ready-to-paste picture markup',
        },
      ],
      defaultTargetSizeBytes: 0,
      duplicable: false,
    },
  },
  {
    id: 'recipe.social_clip',
    route: '/recipe/social-clip',
    inputLabel: '1 video',
    formats: ['MP4', 'WEBM'],
    icon: Clapperboard,
    fallback: {
      id: 'recipe.social_clip',
      name: 'Social clip',
      description:
        'Encode one video as MP4 and WebM and export a poster frame, ready for social uploads.',
      category: 'video',
      inputKind: 'video',
      acceptExtensions: ['mp4', 'mov', 'mkv', 'webm', 'avi'],
      maxInputs: 1,
      highlights: ['MP4 + WebM', 'Source resolution', 'Poster frame'],
      steps: [
        {
          id: 'mp4',
          name: 'Encode MP4',
          toolId: 'tool.video.convert',
          detail: 'H.264 + AAC at the source resolution',
        },
        {
          id: 'webm',
          name: 'Encode WebM',
          toolId: 'tool.video.convert',
          detail: 'VP9 + Opus at the source resolution',
        },
        {
          id: 'poster',
          name: 'Extract poster',
          toolId: '',
          detail: 'JPEG frame from the first second',
        },
      ],
      defaultTargetSizeBytes: 0,
      duplicable: false,
    },
  },
  {
    id: 'recipe.clean_scan',
    route: '/recipe/clean-scan',
    inputLabel: '1 PDF',
    formats: ['PDF', 'TXT'],
    icon: ScanText,
    fallback: {
      id: 'recipe.clean_scan',
      name: 'Clean scan',
      description:
        'Turn a scanned PDF into a searchable PDF with a local text sidecar.',
      category: 'documents',
      inputKind: 'pdf',
      acceptExtensions: ['pdf'],
      maxInputs: 1,
      highlights: ['Local OCR', 'Searchable PDF', 'Text sidecar'],
      steps: [
        {
          id: 'ocr',
          name: 'OCR every page',
          toolId: 'tool.pdf.ocr',
          detail: 'Rasterize at 300 DPI and recognize text locally',
        },
        {
          id: 'verify',
          name: 'Verify text layer',
          toolId: '',
          detail: 'Page count and extracted text',
        },
      ],
      defaultTargetSizeBytes: 0,
      duplicable: false,
    },
  },
  {
    id: 'recipe.burned_subtitles',
    route: '/recipe/burned-subtitles',
    inputLabel: '1 video',
    formats: ['MP4', 'SRT'],
    icon: Captions,
    fallback: {
      id: 'recipe.burned_subtitles',
      name: 'Burned-in subtitles',
      description:
        'Transcribe a video locally and burn synchronized subtitles into a new MP4.',
      category: 'video',
      inputKind: 'video',
      acceptExtensions: ['mp4', 'mov', 'mkv', 'webm', 'avi'],
      maxInputs: 1,
      highlights: ['Local Whisper', 'Synchronized burn-in', 'SRT sidecar'],
      steps: [
        {
          id: 'transcribe',
          name: 'Transcribe audio',
          toolId: 'tool.video.transcribe',
          detail: 'Whisper writes timed subtitles locally',
        },
        {
          id: 'burn',
          name: 'Burn subtitles',
          toolId: '',
          detail: 'ffmpeg renders the cues into the picture; audio is copied',
        },
        {
          id: 'verify',
          name: 'Verify sync',
          toolId: '',
          detail: 'Duration, cues and rendered frames are checked',
        },
      ],
      defaultTargetSizeBytes: 0,
      duplicable: false,
    },
  },
  {
    id: 'recipe.archive_document',
    route: '/recipe/archive-document',
    inputLabel: '1 DOCX',
    formats: ['PDF/A-2', 'SHA-256'],
    icon: Archive,
    fallback: {
      id: 'recipe.archive_document',
      name: 'Archive document',
      description:
        'Turn a DOCX into a PDF/A-2 file and report its SHA-256 for long-term storage.',
      category: 'documents',
      inputKind: 'document',
      acceptExtensions: ['docx'],
      maxInputs: 1,
      highlights: ['PDF/A-2', 'SHA-256 checksum', 'Optional sidecar'],
      steps: [
        {
          id: 'render',
          name: 'Render PDF/A-2',
          toolId: 'tool.doc.docx_to_pdf',
          detail: 'Bundled DOCX renderer, then Ghostscript distils the PDF',
        },
        {
          id: 'verify',
          name: 'Hash and verify',
          toolId: '',
          detail: 'PDF signature, PDF/A marker, page count and SHA-256',
        },
      ],
      defaultTargetSizeBytes: 0,
      duplicable: true,
      optionSchema: [
        {
          key: 'writeSidecar',
          label: 'SHA-256 sidecar',
          kind: RecipeOptionKind.RecipeOptionBoolean,
          default: false,
          help: 'Write <pdf>.sha256 next to the PDF with a `hash  filename` line.',
        },
      ],
    },
  },
];

/**
 * Derives the sidebar category for a recipe without registering it as a tool.
 * Image and video recipes keep their category; PDF inputs land in `pdf` and
 * everything else falls back to `docs`.
 */
function recipeNavCategory(recipe: RecipeDefinition): ToolCategory {
  const { category, inputKind } = recipe.fallback;

  if (category === 'image') {
    return 'image';
  }

  if (category === 'video') {
    return 'video';
  }

  if (inputKind === 'pdf') {
    return 'pdf';
  }

  return 'docs';
}

/** Recipe entries shaped as navigation destinations, kept out of `NAVIGATION`. */
export const RECIPE_NAV: readonly ToolDefinition[] = RECIPES.map((recipe) => {
  const category = recipeNavCategory(recipe);
  const nameWords = recipe.fallback.name.toLowerCase().split(/\s+/);

  return {
    id: recipe.id,
    name: recipe.fallback.name,
    navName: recipe.fallback.name,
    description: recipe.fallback.description,
    route: recipe.route,
    category,
    icon: recipe.icon,
    formats: recipe.formats,
    keywords: ['recipe', ...nameWords, 'outcome', category],
  };
});

export const RECIPE_NAV_GROUP: NavGroup = {
  id: 'recipes',
  label: 'Recipes',
  items: RECIPE_NAV,
};

/** Static definition for a recipe, used before the catalog answers or offline. */
export function findRecipeFallback(recipeId: string): Recipe | null {
  const found = RECIPES.find((recipe) => recipe.id === recipeId);
  return found ? found.fallback : null;
}
