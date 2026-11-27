/**
 * Navigation overlays for every workbench destination.
 *
 * English values are the canonical strings from `config/tools.ts` and
 * `config/recipes.ts`; ids stay identical to the registry ids.
 */
export const toolsEn = {
  'tools.workspace.home.name': 'Home',
  'tools.workspace.home.navName': 'Home',
  'tools.workspace.home.description':
    'Search the workbench, open a tool and pick up where you left off.',

  'tools.tool.image.convert.name': 'Image Convert',
  'tools.tool.image.convert.navName': 'Convert',
  'tools.tool.image.convert.description':
    'Convert PNG, JPG, WebP and GIF images in single or batch mode.',

  'tools.tool.image.crop.name': 'Image Crop',
  'tools.tool.image.crop.navName': 'Crop',
  'tools.tool.image.crop.description':
    'Crop an image to a ratio preset or a custom region with a live preview.',

  'tools.tool.image.annotate.name': 'Image Annotate',
  'tools.tool.image.annotate.navName': 'Annotate',
  'tools.tool.image.annotate.description':
    'Add text, arrows and boxes, then blur or redact regions before export.',

  'tools.tool.image.compress.name': 'Image Compress',
  'tools.tool.image.compress.navName': 'Compress',
  'tools.tool.image.compress.description':
    'Compress images to a target file size with optional downscaling.',

  'tools.tool.image.watermark.name': 'Image Watermark',
  'tools.tool.image.watermark.navName': 'Watermark',
  'tools.tool.image.watermark.description':
    'Watermark images with text or a logo at nine anchors, with opacity and tiling.',

  'tools.tool.image.ocr.name': 'Image OCR',
  'tools.tool.image.ocr.navName': 'Image OCR',
  'tools.tool.image.ocr.description':
    'Extract text from images with local Tesseract and optionally write a searchable PDF.',

  'tools.tool.image.background.name': 'Remove Background',
  'tools.tool.image.background.navName': 'Remove BG',
  'tools.tool.image.background.description':
    'Remove image backgrounds locally and save a transparent PNG or a chosen color.',

  'tools.tool.image.upscale.name': 'Upscale Image',
  'tools.tool.image.upscale.navName': 'Upscale',
  'tools.tool.image.upscale.description':
    'Enlarge images 2x or 4x locally with an optional ONNX model pack.',

  'tools.tool.pdf.merge.name': 'PDF Merge',
  'tools.tool.pdf.merge.navName': 'Merge',
  'tools.tool.pdf.merge.description':
    'Combine several PDF files into one document in the order you choose.',

  'tools.tool.pdf.split.name': 'PDF Split',
  'tools.tool.pdf.split.navName': 'Split',
  'tools.tool.pdf.split.description':
    'Split a PDF into separate pages, ranges, page chunks or bookmarks.',

  'tools.tool.pdf.crop.name': 'PDF Crop',
  'tools.tool.pdf.crop.navName': 'Crop',
  'tools.tool.pdf.crop.description':
    'Crop PDF pages with margin presets or custom values, single or batch.',

  'tools.tool.pdf.optimize.name': 'PDF Optimize',
  'tools.tool.pdf.optimize.navName': 'Optimize',
  'tools.tool.pdf.optimize.description':
    'Compress PDFs and edit title, author and document metadata.',

  'tools.tool.pdf.ocr.name': 'PDF OCR',
  'tools.tool.pdf.ocr.navName': 'OCR',
  'tools.tool.pdf.ocr.description':
    'Make scanned PDFs searchable with local OCR and a text sidecar.',

  'tools.tool.pdf.rotate.name': 'PDF Rotate',
  'tools.tool.pdf.rotate.navName': 'Rotate',
  'tools.tool.pdf.rotate.description':
    'Rotate all pages or a page selection by 90, 180 or 270 degrees',

  'tools.tool.pdf.remove_pages.name': 'PDF Remove Pages',
  'tools.tool.pdf.remove_pages.navName': 'Remove pages',
  'tools.tool.pdf.remove_pages.description':
    'Delete selected pages and keep the rest of the document',

  'tools.tool.pdf.reorder.name': 'PDF Reorder Pages',
  'tools.tool.pdf.reorder.navName': 'Reorder',
  'tools.tool.pdf.reorder.description':
    'Rewrite the page order from an explicit spec, or reverse the document',

  'tools.tool.pdf.extract_images.name': 'PDF Extract Images',
  'tools.tool.pdf.extract_images.navName': 'Extract images',
  'tools.tool.pdf.extract_images.description':
    'Extract embedded images into a folder with collision-safe names',

  'tools.tool.doc.md_to_pdf.name': 'Markdown → PDF',
  'tools.tool.doc.md_to_pdf.navName': 'Markdown',
  'tools.tool.doc.md_to_pdf.description':
    'Render Markdown to PDF with an optional header and footer.',

  'tools.tool.doc.docx_to_pdf.name': 'DOCX → PDF',
  'tools.tool.doc.docx_to_pdf.navName': 'DOCX',
  'tools.tool.doc.docx_to_pdf.description':
    'Convert DOCX documents to PDF in single or batch mode.',

  'tools.tool.video.convert.name': 'Video Convert',
  'tools.tool.video.convert.navName': 'Convert',
  'tools.tool.video.convert.description':
    'Convert MP4, MOV, MKV and WebM videos with live progress and ETA.',

  'tools.tool.video.trim.name': 'Video Trim',
  'tools.tool.video.trim.navName': 'Trim',
  'tools.tool.video.trim.description':
    'Trim single or batch videos to the start and end time you set.',

  'tools.tool.video.merge.name': 'Video Merge',
  'tools.tool.video.merge.navName': 'Merge',
  'tools.tool.video.merge.description':
    'Join video clips into one file in the order you choose.',

  'tools.tool.video.transcribe.name': 'Video Transcribe',
  'tools.tool.video.transcribe.navName': 'Transcribe',
  'tools.tool.video.transcribe.description':
    'Transcribe video and audio locally with Whisper. No uploads.',

  'tools.tool.audio.extract.name': 'Extract Audio',
  'tools.tool.audio.extract.navName': 'Extract',
  'tools.tool.audio.extract.description':
    'Extract the audio track from a video or audio file as mp3, m4a, opus, wav or flac',

  'tools.tool.audio.convert.name': 'Audio Convert',
  'tools.tool.audio.convert.navName': 'Convert',
  'tools.tool.audio.convert.description':
    'Convert audio files between mp3, m4a, opus, wav and flac',

  'tools.tool.audio.normalize.name': 'Normalize Audio',
  'tools.tool.audio.normalize.navName': 'Normalize',
  'tools.tool.audio.normalize.description':
    'Two-pass EBU R128 loudness normalization to streaming, broadcast or podcast targets',

  'tools.tool.pdf.images.name': 'Images to PDF',
  'tools.tool.pdf.images.navName': 'Images to PDF',
  'tools.tool.pdf.images.description':
    'Place several images on a page grid and export one PDF.',

  'tools.tool.pdf.to_docx.name': 'PDF to DOCX',
  'tools.tool.pdf.to_docx.navName': 'To DOCX',
  'tools.tool.pdf.to_docx.description':
    'Convert a PDF into an editable DOCX while preserving fonts, weight, colors, images, tables and page layout.',

  'tools.system.status.name': 'System Status',
  'tools.system.status.navName': 'Status',
  'tools.system.status.description':
    'Inspect registered tools, runtime dependencies and backend health.',

  'tools.system.automations.name': 'Automations',
  'tools.system.automations.navName': 'Automations',
  'tools.system.automations.description':
    'Watch a folder and run a recipe when a file arrives.',

  'tools.system.duplicates.name': 'Duplicates',
  'tools.system.duplicates.navName': 'Duplicates',
  'tools.system.duplicates.description':
    'Scan folders for identical files and see how much space they waste.',

  'tools.system.settings.name': 'Settings',
  'tools.system.settings.navName': 'Settings',
  'tools.system.settings.description':
    'Set the default output folder, theme and history preferences.',

  'tools.recipe.email_pdf.name': 'Email PDF',
  'tools.recipe.email_pdf.navName': 'Email PDF',
  'tools.recipe.email_pdf.description':
    'Merge one or more PDFs and compress the result to a size that is easy to send by email.',

  'tools.recipe.web_image_set.name': 'Web image set',
  'tools.recipe.web_image_set.navName': 'Web image set',
  'tools.recipe.web_image_set.description':
    'Export responsive WebP and JPEG variants with a ZIP bundle and a ready-to-paste srcset snippet.',

  'tools.recipe.social_clip.name': 'Social clip',
  'tools.recipe.social_clip.navName': 'Social clip',
  'tools.recipe.social_clip.description':
    'Encode one video as MP4 and WebM and export a poster frame, ready for social uploads.',

  'tools.recipe.clean_scan.name': 'Clean scan',
  'tools.recipe.clean_scan.navName': 'Clean scan',
  'tools.recipe.clean_scan.description':
    'Turn a scanned PDF into a searchable PDF with a local text sidecar.',

  'tools.recipe.burned_subtitles.name': 'Burned-in subtitles',
  'tools.recipe.burned_subtitles.navName': 'Burned-in subtitles',
  'tools.recipe.burned_subtitles.description':
    'Transcribe a video locally and burn synchronized subtitles into a new MP4.',

  'tools.recipe.archive_document.name': 'Archive document',
  'tools.recipe.archive_document.navName': 'Archive document',
  'tools.recipe.archive_document.description':
    'Turn a DOCX into a PDF/A-2 file and report its SHA-256 for long-term storage.',
} as const;
