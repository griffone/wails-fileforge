export const videoTargetEs = {
  'videoTarget.field.target': 'Tamaño objetivo (MB)',
  'videoTarget.presets.label': 'Preajustes de destino',
  'videoTarget.presets.help':
    'Elige un servicio y el objetivo se rellena. Cualquier valor que escribas también funciona.',
  'videoTarget.target.noTarget': 'Sin objetivo',
  'videoTarget.target.summary': 'Objetivo de {size} por vídeo',
  'videoTarget.target.unitHelp':
    'Los tamaños objetivo usan 1 MB = 1.048.576 bytes.',
  'videoTarget.target.optionalHelp':
    'Opcional. Déjalo vacío para codificar con los ajustes del preset.',
  'videoTarget.target.errorPositive':
    'Introduce un tamaño objetivo mayor que 0 MB.',
  'videoTarget.target.errorMax':
    'Introduce un tamaño objetivo de {max} MB o menos.',
  'videoTarget.target.errorTooLarge':
    'El tamaño objetivo es demasiado grande para usarlo.',
  'videoTarget.note.title': 'Los objetivos son un máximo esfuerzo',
  'videoTarget.note.body':
    'Cuando un vídeo no puede alcanzar el objetivo, la herramienta se detiene en el resultado más pequeño que puede producir e informa el tamaño real.',
  'videoTarget.mode.forced':
    'Un objetivo de tamaño requiere una recodificación en dos pasadas, así que Automático y Copia se desactivan mientras hay un objetivo.',
  'videoTarget.bitrate.overridden':
    'El tamaño objetivo elige el bitrate, así que este ajuste se ignora mientras hay un objetivo.',
  'videoTarget.result.title': 'Resultado del objetivo',
  'videoTarget.result.sub':
    'Cómo se compara cada salida con el tamaño objetivo.',
  'videoTarget.result.values':
    '{actual} de {target} objetivo · {bitrate} kbps',
  'videoTarget.result.met': 'Objetivo alcanzado',
  'videoTarget.result.miss': 'No alcanzado (máximo esfuerzo)',
  'videoTarget.result.floor': 'Se aplicó el mínimo de {floor} kbps',
} as const;
