export const imageCompressEn = {
  'imageCompress.page.eyebrow': 'IMAGE',
  'imageCompress.page.titleLead': 'Image',
  'imageCompress.page.titleTail': 'Compress',
  'imageCompress.page.sub':
    'Compress images to a target file size. The tool searches quality, uses a palette for PNG and can downscale when the target needs it.',
  'imageCompress.page.localBadge': 'Local only',
  'imageCompress.intake.title': 'Images',
  'imageCompress.intake.sub':
    'Drop images or choose them from your computer.',
  'imageCompress.intake.dropTitle': 'Drop images here',
  'imageCompress.intake.dropAria': 'Drop or choose images to compress',
  'imageCompress.intake.actionsAria': 'Image selection actions',
  'imageCompress.intake.choose': 'Choose files',
  'imageCompress.intake.clear': 'Clear',
  'imageCompress.intake.batchNote':
    'The same target and settings apply to every image.',
  'imageCompress.notice.filesOne': "1 file wasn't added.",
  'imageCompress.notice.filesMany': "{count} files weren't added.",
  'imageCompress.notice.extensionOne': "{extension} isn't supported.",
  'imageCompress.notice.extensionsMany':
    "{head} and {tail} aren't supported.",
  'imageCompress.output.title': 'Output',
  'imageCompress.output.sub':
    'Set the target, then check where the files will go.',
  'imageCompress.field.target': 'Target size (MB)',
  'imageCompress.field.format': 'Format',
  'imageCompress.format.auto.label': 'Auto',
  'imageCompress.format.jpeg.label': 'JPEG',
  'imageCompress.format.webp.label': 'WebP',
  'imageCompress.format.png.label': 'PNG',
  'imageCompress.formatHelp.auto':
    'Keeps JPEG, WebP and PNG. Other sources become JPEG, or PNG when they have transparency.',
  'imageCompress.formatHelp.png':
    'PNG is lossless; the tool quantises the palette to reach the target.',
  'imageCompress.formatHelp.other':
    'Every image is encoded as {format}.',
  'imageCompress.field.downscaling': 'Downscaling',
  'imageCompress.field.allowDownscale':
    'Allow downscaling to reach the target',
  'imageCompress.field.allowDownscaleHelp':
    "Reduces the longest edge in steps when quality alone can't hit the target. Never enlarges.",
  'imageCompress.advanced.title': 'Advanced',
  'imageCompress.advanced.hint':
    'Dimension limit, quality ceiling and metadata',
  'imageCompress.field.maxDimension': 'Max dimension (px)',
  'imageCompress.field.maxDimensionPlaceholder': 'Original size',
  'imageCompress.field.maxDimensionHelp':
    'Caps the longest edge before compression. Leave empty to keep the original.',
  'imageCompress.field.qualityCeiling': 'Quality ceiling (40–95)',
  'imageCompress.field.qualityPlaceholder': 'Encoder default',
  'imageCompress.field.qualityHelp':
    '40–95. Caps the encoder search; PNG ignores it and quantises instead.',
  'imageCompress.field.metadata': 'Metadata',
  'imageCompress.field.stripMetadata': 'Strip metadata',
  'imageCompress.field.stripMetadataHelp':
    'Removes EXIF, GPS and color profiles unless keeping them is the only way to reach the target.',
  'imageCompress.advanced.reset': 'Reset advanced settings',
  'imageCompress.note.title': 'Targets are best effort',
  'imageCompress.note.body':
    "When an image can't reach the target, the tool stops at the smallest result it can produce and reports the actual size.",
  'imageCompress.helper.addImage':
    'Add at least one image to compress.',
  'imageCompress.target.errorPositive':
    'Enter a target size greater than 0 MB.',
  'imageCompress.target.errorMax':
    'Enter a target size of {max} MB or less.',
  'imageCompress.target.errorTooLarge':
    'The target size is too large to use.',
  'imageCompress.target.noTarget': 'No target',
  'imageCompress.target.summary': 'Target {size} per image',
  'imageCompress.action.compress': 'Compress',
  'imageCompress.action.compressOne': 'Compress 1 image',
  'imageCompress.action.compressMany': 'Compress {count} images',
  'imageCompress.action.compressing': 'Compressing…',
  'imageCompress.action.cancel': 'Cancel',
  'imageCompress.outcome.single':
    'The output is at or under the {target} target.',
  'imageCompress.outcome.all':
    'All {count} outputs are at or under the {target} target.',
  'imageCompress.outcome.none':
    'No output reached the {target} target; the files are the smallest the encoder could produce.',
  'imageCompress.outcome.partial':
    '{atTarget} of {total} outputs are at or under the {target} target; the rest are the smallest available.',
  'imageCompress.savings.saved': 'Saved {size} ({percent}%)',
  'imageCompress.savings.larger': 'Output is {size} larger',
  'imageCompress.destination.original': 'Next to each original',
  'imageCompress.progress.aria': 'Compression progress',
  'imageCompress.status.waiting': 'Waiting to start',
  'imageCompress.error.start': 'The compression could not start.',
  'imageCompress.error.plan': 'The output could not be planned.',
  'imageCompress.error.cancel': 'The job could not be cancelled.',
  'imageCompress.error.openFile': 'The file could not be opened.',
  'imageCompress.error.openFolder': 'The folder could not be opened.',
  'imageCompress.error.status': 'The job status could not be read.',
  'imageCompress.error.inspect': 'File sizes could not be read.',
} as const;
