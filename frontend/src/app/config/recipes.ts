import { Clapperboard, Images, Mail, type LucideIconData } from 'lucide-angular';

import { RecipeV1 } from '../services/wails';

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
  fallback: RecipeV1;
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
    },
  },
];

/** Static definition for a recipe, used before the catalog answers or offline. */
export function findRecipeFallback(recipeId: string): RecipeV1 | null {
  const found = RECIPES.find((recipe) => recipe.id === recipeId);
  return found ? found.fallback : null;
}
