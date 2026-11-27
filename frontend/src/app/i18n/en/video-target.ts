export const videoTargetEn = {
  'videoTarget.field.target': 'Target size (MB)',
  'videoTarget.presets.label': 'Destination presets',
  'videoTarget.presets.help':
    'Pick a service and the target fills in. Any value you type also works.',
  'videoTarget.target.noTarget': 'No target',
  'videoTarget.target.summary': 'Target {size} per video',
  'videoTarget.target.unitHelp': 'Target sizes use 1 MB = 1,048,576 bytes.',
  'videoTarget.target.optionalHelp':
    'Optional. Leave it empty to encode with the preset settings.',
  'videoTarget.target.errorPositive':
    'Enter a target size greater than 0 MB.',
  'videoTarget.target.errorMax':
    'Enter a target size of {max} MB or less.',
  'videoTarget.target.errorTooLarge':
    'The target size is too large to use.',
  'videoTarget.note.title': 'Targets are best effort',
  'videoTarget.note.body':
    "When a video can't reach the target, the tool stops at the smallest result it can produce and reports the actual size.",
  'videoTarget.mode.forced':
    'A size target needs a two-pass re-encode, so Auto and Copy are switched off while a target is set.',
  'videoTarget.bitrate.overridden':
    'A target size chooses the bitrate, so this setting is ignored while a target is set.',
  'videoTarget.result.title': 'Target result',
  'videoTarget.result.sub': 'How each output compares with the target size.',
  'videoTarget.result.values': '{actual} of {target} target · {bitrate} kbps',
  'videoTarget.result.met': 'Target met',
  'videoTarget.result.miss': 'Not met (best effort)',
  'videoTarget.result.floor': 'Floor of {floor} kbps applied',
} as const;
