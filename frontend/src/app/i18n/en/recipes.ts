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
  'recipes.email_pdf.step.merge.name': 'Merge PDFs',
  'recipes.email_pdf.step.merge.detail': 'Only when there are two or more PDFs',
  'recipes.email_pdf.step.optimize.name': 'Optimize to target',
  'recipes.email_pdf.step.optimize.detail': 'Ghostscript when the target needs it',
  'recipes.web_image_set.inputLabel': '1 image',
  'recipes.web_image_set.highlights': ['No upscaling', 'WebP + JPEG', 'ZIP + srcset'],
  'recipes.web_image_set.step.build.name': 'Build responsive variants',
  'recipes.web_image_set.step.build.detail':
    'WebP and JPEG at the widths your source can support',
  'recipes.web_image_set.step.zip.name': 'Create ZIP bundle',
  'recipes.web_image_set.step.zip.detail':
    'All variants plus the snippet in one download',
  'recipes.web_image_set.step.snippet.name': 'Write srcset snippet',
  'recipes.web_image_set.step.snippet.detail': 'Ready-to-paste picture markup',
  'recipes.social_clip.inputLabel': '1 video',
  'recipes.social_clip.highlights': ['MP4 + WebM', 'Source resolution', 'Poster frame'],
  'recipes.social_clip.step.mp4.name': 'Encode MP4',
  'recipes.social_clip.step.mp4.detail': 'H.264 + AAC at the source resolution',
  'recipes.social_clip.step.webm.name': 'Encode WebM',
  'recipes.social_clip.step.webm.detail': 'VP9 + Opus at the source resolution',
  'recipes.social_clip.step.poster.name': 'Extract poster',
  'recipes.social_clip.step.poster.detail': 'JPEG frame from the first second',
  'recipes.clean_scan.inputLabel': '1 PDF',
  'recipes.clean_scan.highlights': ['Local OCR', 'Searchable PDF', 'Text sidecar'],
  'recipes.clean_scan.step.ocr.name': 'OCR every page',
  'recipes.clean_scan.step.ocr.detail':
    'Rasterize at 300 DPI and recognize text locally',
  'recipes.clean_scan.step.verify.name': 'Verify text layer',
  'recipes.clean_scan.step.verify.detail': 'Page count and extracted text',
  'recipes.burned_subtitles.inputLabel': '1 video',
  'recipes.burned_subtitles.highlights': [
    'Local Whisper',
    'Synchronized burn-in',
    'SRT sidecar',
  ],
  'recipes.burned_subtitles.step.transcribe.name': 'Transcribe audio',
  'recipes.burned_subtitles.step.transcribe.detail':
    'Whisper writes timed subtitles locally',
  'recipes.burned_subtitles.step.burn.name': 'Burn subtitles',
  'recipes.burned_subtitles.step.burn.detail':
    'ffmpeg renders the cues into the picture; audio is copied',
  'recipes.burned_subtitles.step.verify.name': 'Verify sync',
  'recipes.burned_subtitles.step.verify.detail':
    'Duration, cues and rendered frames are checked',
  'recipes.archive_document.inputLabel': '1 DOCX',
  'recipes.archive_document.highlights': [
    'PDF/A-2',
    'SHA-256 checksum',
    'Optional sidecar',
  ],
  'recipes.archive_document.step.render.name': 'Render PDF/A-2',
  'recipes.archive_document.step.render.detail':
    'Bundled DOCX renderer, then Ghostscript distils the PDF',
  'recipes.archive_document.step.verify.name': 'Hash and verify',
  'recipes.archive_document.step.verify.detail':
    'PDF signature, PDF/A marker, page count and SHA-256',
} as const;
