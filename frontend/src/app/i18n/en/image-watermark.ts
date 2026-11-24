export const imageWatermarkEn = {
  'imageWatermark.page.eyebrow': 'IMAGE',
  'imageWatermark.page.titleLead': 'Image',
  'imageWatermark.page.titleTail': 'Watermark',
  'imageWatermark.page.sub':
    'Add a text or logo watermark to a batch of images. Pick one of nine anchors and check the placement before running.',
  'imageWatermark.page.localBadge': 'Local only',
  'imageWatermark.intake.title': 'Images',
  'imageWatermark.intake.sub':
    'Drop images or choose them from your computer.',
  'imageWatermark.intake.dropTitle': 'Drop images here',
  'imageWatermark.intake.dropAria': 'Drop or choose images to watermark',
  'imageWatermark.intake.actionsAria': 'Image selection actions',
  'imageWatermark.intake.choose': 'Choose files',
  'imageWatermark.intake.clear': 'Clear',
  'imageWatermark.intake.batchNote':
    'The same watermark applies to every image.',
  'imageWatermark.notice.rejectedOne':
    "{name} wasn't added because it isn't an image.",
  'imageWatermark.notice.rejectedMany':
    "{count} files weren't added because they aren't images.",
  'imageWatermark.notice.duplicateOne': '{name} is already in the queue.',
  'imageWatermark.notice.duplicateMany':
    '{count} images are already in the queue.',
  'imageWatermark.preview.title': 'Preview',
  'imageWatermark.preview.sub':
    'The first selected image with the current placement.',
  'imageWatermark.preview.approximate': 'Approximate',
  'imageWatermark.preview.alt': 'First image with the watermark preview',
  'imageWatermark.preview.description.logo':
    'Logo watermark at {position}, {scale}% of the image width, {opacity}% opacity.',
  'imageWatermark.preview.description.tiled':
    'Tiled text watermark, {scale}% font size, {opacity}% opacity. The tiled preview is approximate.',
  'imageWatermark.preview.description.text':
    'Text watermark at {position}, {scale}% font size, {opacity}% opacity.',
  'imageWatermark.preview.note.tiled':
    'Tiled preview is approximate. The output repeats the text on a rotated grid using the bundled font.',
  'imageWatermark.preview.note.logo':
    'Logo preview uses the selected logo. The output never upscales a logo beyond its natural width.',
  'imageWatermark.preview.note.text':
    'Preview is approximate; the output auto-fits the text and uses the bundled font.',
  'imageWatermark.preview.loadingTitle': 'Loading preview…',
  'imageWatermark.preview.unavailableTitle': 'Preview unavailable',
  'imageWatermark.preview.emptyTitle': 'No image to watermark yet',
  'imageWatermark.preview.loadingBody': 'Reading the first image from disk.',
  'imageWatermark.preview.emptyBody':
    'Add an image above to see the watermark placement.',
  'imageWatermark.settings.title': 'Watermark',
  'imageWatermark.settings.sub':
    'Choose the content, placement and visibility.',
  'imageWatermark.kind.text': 'Text',
  'imageWatermark.kind.logo': 'Logo',
  'imageWatermark.kind.logoDisabledTitle':
    'Untick tiling to use a logo',
  'imageWatermark.kind.logoTitle': 'Logo watermark',
  'imageWatermark.field.type': 'Type',
  'imageWatermark.field.typeAria': 'Watermark type',
  'imageWatermark.field.text': 'Text',
  'imageWatermark.field.textHelp':
    'Line breaks are preserved. {limit}, up to 10 lines.',
  'imageWatermark.field.color': 'Color',
  'imageWatermark.field.colorAria': 'Watermark color',
  'imageWatermark.field.weight': 'Weight',
  'imageWatermark.field.bold': 'Bold text',
  'imageWatermark.field.logo': 'Logo',
  'imageWatermark.logo.none': 'No logo selected',
  'imageWatermark.logo.loading': 'Loading thumbnail…',
  'imageWatermark.logo.hint': 'PNG, JPEG or WebP.',
  'imageWatermark.logo.choose': 'Choose logo',
  'imageWatermark.logo.removeAria': 'Remove logo',
  'imageWatermark.logo.removeTitle': 'Remove logo',
  'imageWatermark.field.anchor': 'Anchor',
  'imageWatermark.anchor.aria': 'Anchor: {label}',
  'imageWatermark.anchor.top-left': 'Top left',
  'imageWatermark.anchor.top-center': 'Top center',
  'imageWatermark.anchor.top-right': 'Top right',
  'imageWatermark.anchor.center-left': 'Center left',
  'imageWatermark.anchor.center': 'Center',
  'imageWatermark.anchor.center-right': 'Center right',
  'imageWatermark.anchor.bottom-left': 'Bottom left',
  'imageWatermark.anchor.bottom-center': 'Bottom center',
  'imageWatermark.anchor.bottom-right': 'Bottom right',
  'imageWatermark.field.margin': 'Margin (% of short edge)',
  'imageWatermark.field.marginAria': 'Margin percent',
  'imageWatermark.field.scaleLogo': 'Scale (% of image width)',
  'imageWatermark.field.scaleText': 'Scale (% of short edge)',
  'imageWatermark.field.scaleAria': 'Watermark scale percent',
  'imageWatermark.field.scaleLogoHelp':
    'Logo width relative to the image.',
  'imageWatermark.field.scaleTextHelp':
    'Font size relative to the shorter image edge.',
  'imageWatermark.field.opacity': 'Opacity (0.05–1)',
  'imageWatermark.field.opacityAria': 'Opacity',
  'imageWatermark.field.tiling': 'Tiling',
  'imageWatermark.field.tile': 'Repeat the text across the image',
  'imageWatermark.field.tileHelp':
    'Tiling applies to text watermarks only; it replaces the single anchored placement.',
  'imageWatermark.field.rotation': 'Rotation (-90° to 90°)',
  'imageWatermark.field.rotationAria': 'Tile rotation degrees',
  'imageWatermark.output.title': 'Output',
  'imageWatermark.output.sub':
    'Check where the watermarked images will go.',
  'imageWatermark.advanced.title': 'Advanced',
  'imageWatermark.advanced.hint': 'Format and quality',
  'imageWatermark.field.format': 'Format',
  'imageWatermark.field.formatHelp':
    'Auto keeps JPEG, PNG and WebP. Other sources become JPEG, or PNG when they have transparency.',
  'imageWatermark.field.quality': 'Quality',
  'imageWatermark.field.qualityHelp':
    '40–100. Applies to JPEG and WebP; PNG is lossless.',
  'imageWatermark.helper.addImage':
    'Add at least one image to watermark.',
  'imageWatermark.helper.chooseLogo': 'Choose a logo image.',
  'imageWatermark.helper.logoFormat': 'Use a PNG, JPEG or WebP logo.',
  'imageWatermark.helper.textRequired': 'Enter the watermark text.',
  'imageWatermark.helper.textTooLong':
    'Text must be at most {max} characters.',
  'imageWatermark.helper.tooManyLines':
    'Text must be at most {max} lines.',
  'imageWatermark.textLimit': '{count}/{max} characters',
  'imageWatermark.error.logoFormat': 'Use a PNG, JPEG or WebP logo.',
  'imageWatermark.error.previewImage':
    'The preview image could not be displayed.',
  'imageWatermark.error.logoPreview':
    'The logo preview could not be displayed.',
  'imageWatermark.error.logoPreviewLoad':
    'The logo preview could not be loaded.',
  'imageWatermark.error.preview':
    'The image preview could not be loaded.',
  'imageWatermark.action.watermark': 'Watermark',
  'imageWatermark.action.watermarkOne': 'Watermark 1 image',
  'imageWatermark.action.watermarkMany': 'Watermark {count} images',
  'imageWatermark.action.watermarking': 'Watermarking…',
  'imageWatermark.action.cancel': 'Cancel',
  'imageWatermark.destination.original': 'Next to each original',
  'imageWatermark.progress.aria': 'Watermark progress',
  'imageWatermark.status.waiting': 'Waiting to start',
  'imageWatermark.error.start': 'The watermark could not start.',
  'imageWatermark.error.plan': 'The output could not be planned.',
  'imageWatermark.error.cancel': 'The job could not be cancelled.',
  'imageWatermark.error.openFile': 'The file could not be opened.',
  'imageWatermark.error.openFolder': 'The folder could not be opened.',
  'imageWatermark.error.status': 'The job status could not be read.',
  'imageWatermark.error.inspect': 'File sizes could not be read.',
  'imageWatermark.format.auto.label': 'Auto',
  'imageWatermark.format.jpeg.label': 'JPEG',
  'imageWatermark.format.png.label': 'PNG',
  'imageWatermark.format.webp.label': 'WebP',
} as const;
