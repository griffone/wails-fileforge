/**
 * Recipe-only copy that is not part of the canonical `tools.<id>` overlays.
 *
 * Slugs drop the `recipe.` prefix from the recipe id.
 */
export const recipesEn = {
  'recipes.email_pdf.inputLabel': '1+ PDFs',
  'recipes.email_pdf.highlights': [
    'Merge when needed',
    'Target size',
    'Verified + undo',
  ],
  'recipes.web_image_set.inputLabel': '1 image',
  'recipes.web_image_set.highlights': ['No upscaling', 'WebP + JPEG', 'ZIP + srcset'],
  'recipes.social_clip.inputLabel': '1 video',
  'recipes.social_clip.highlights': ['MP4 + WebM', 'Source resolution', 'Poster frame'],
  'recipes.clean_scan.inputLabel': '1 PDF',
  'recipes.clean_scan.highlights': ['Local OCR', 'Searchable PDF', 'Text sidecar'],
  'recipes.burned_subtitles.inputLabel': '1 video',
  'recipes.burned_subtitles.highlights': [
    'Local Whisper',
    'Synchronized burn-in',
    'SRT sidecar',
  ],
} as const;
