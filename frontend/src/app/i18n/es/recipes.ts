export const recipesEs = {
  'recipes.email_pdf.inputLabel': '1+ PDF',
  'recipes.email_pdf.highlights': [
    'Unir si hace falta',
    'Tamaño objetivo',
    'Verificado + deshacer',
  ],
  'recipes.email_pdf.step.merge.name': 'Unir PDF',
  'recipes.email_pdf.step.merge.detail': 'Solo cuando hay dos o más PDF',
  'recipes.email_pdf.step.optimize.name': 'Optimizar al objetivo',
  'recipes.email_pdf.step.optimize.detail': 'Ghostscript cuando el objetivo lo requiere',
  'recipes.web_image_set.inputLabel': '1 imagen',
  'recipes.web_image_set.highlights': ['Sin ampliación', 'WebP + JPEG', 'ZIP + srcset'],
  'recipes.web_image_set.step.build.name': 'Generar variantes adaptables',
  'recipes.web_image_set.step.build.detail':
    'WebP y JPEG en los anchos que admita la fuente',
  'recipes.web_image_set.step.zip.name': 'Crear paquete ZIP',
  'recipes.web_image_set.step.zip.detail':
    'Todas las variantes y el fragmento en una descarga',
  'recipes.web_image_set.step.snippet.name': 'Escribir fragmento srcset',
  'recipes.web_image_set.step.snippet.detail': 'Marcado picture listo para pegar',
  'recipes.social_clip.inputLabel': '1 video',
  'recipes.social_clip.highlights': ['MP4 + WebM', 'Resolución original', 'Fotograma de portada'],
  'recipes.social_clip.step.mp4.name': 'Codificar MP4',
  'recipes.social_clip.step.mp4.detail': 'H.264 + AAC a la resolución original',
  'recipes.social_clip.step.webm.name': 'Codificar WebM',
  'recipes.social_clip.step.webm.detail': 'VP9 + Opus a la resolución original',
  'recipes.social_clip.step.poster.name': 'Extraer portada',
  'recipes.social_clip.step.poster.detail': 'Fotograma JPEG del primer segundo',
  'recipes.clean_scan.inputLabel': '1 PDF',
  'recipes.clean_scan.highlights': ['OCR local', 'PDF buscable', 'Texto adjunto'],
  'recipes.clean_scan.step.ocr.name': 'OCR en cada página',
  'recipes.clean_scan.step.ocr.detail':
    'Rasteriza a 300 DPI y reconoce texto localmente',
  'recipes.clean_scan.step.verify.name': 'Verificar capa de texto',
  'recipes.clean_scan.step.verify.detail': 'Número de páginas y texto extraído',
  'recipes.burned_subtitles.inputLabel': '1 video',
  'recipes.burned_subtitles.highlights': [
    'Whisper local',
    'Incrustado sincronizado',
    'SRT adjunto',
  ],
  'recipes.burned_subtitles.step.transcribe.name': 'Transcribir audio',
  'recipes.burned_subtitles.step.transcribe.detail':
    'Whisper escribe subtítulos sincronizados localmente',
  'recipes.burned_subtitles.step.burn.name': 'Incrustar subtítulos',
  'recipes.burned_subtitles.step.burn.detail':
    'ffmpeg renderiza las señales en la imagen; el audio se copia',
  'recipes.burned_subtitles.step.verify.name': 'Verificar sincronía',
  'recipes.burned_subtitles.step.verify.detail':
    'Se revisan duración, señales y fotogramas renderizados',
  'recipes.archive_document.inputLabel': '1 DOCX',
  'recipes.archive_document.highlights': [
    'PDF/A-2',
    'Suma SHA-256',
    'Archivo adjunto opcional',
  ],
  'recipes.archive_document.step.render.name': 'Generar PDF/A-2',
  'recipes.archive_document.step.render.detail':
    'El renderizador DOCX incluido y Ghostscript generan el PDF',
  'recipes.archive_document.step.verify.name': 'Calcular y verificar',
  'recipes.archive_document.step.verify.detail':
    'Firma PDF, marcador PDF/A, número de páginas y SHA-256',
};
