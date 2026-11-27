/**
 * Spanish overlays for every workbench destination. Keys mirror `en/tools.ts`
 * and stay id-based so `ToolRegistryService` can resolve them at runtime.
 */
export const toolsEs = {
  'tools.workspace.home.name': 'Inicio',
  'tools.workspace.home.navName': 'Inicio',
  'tools.workspace.home.description':
    'Busca en el área de trabajo, abre una herramienta y continúa donde lo dejaste.',

  'tools.tool.image.convert.name': 'Convertir imagen',
  'tools.tool.image.convert.navName': 'Convertir',
  'tools.tool.image.convert.description':
    'Convierte imágenes PNG, JPG, WebP y GIF en modo individual o por lotes.',

  'tools.tool.image.crop.name': 'Recortar imagen',
  'tools.tool.image.crop.navName': 'Recortar',
  'tools.tool.image.crop.description':
    'Recorta una imagen a una proporción predefinida o a una región personalizada con vista previa en vivo.',

  'tools.tool.image.annotate.name': 'Anotar imagen',
  'tools.tool.image.annotate.navName': 'Anotar',
  'tools.tool.image.annotate.description':
    'Añade texto, flechas y cajas, y luego difumina o censura regiones antes de exportar.',

  'tools.tool.image.compress.name': 'Comprimir imagen',
  'tools.tool.image.compress.navName': 'Comprimir',
  'tools.tool.image.compress.description':
    'Comprime imágenes a un tamaño de archivo objetivo con reducción de escala opcional.',

  'tools.tool.image.watermark.name': 'Marca de agua',
  'tools.tool.image.watermark.navName': 'Marca de agua',
  'tools.tool.image.watermark.description':
    'Aplica marcas de agua con texto o un logo en nueve posiciones, con opacidad y mosaico.',

  'tools.tool.image.ocr.name': 'OCR de imagen',
  'tools.tool.image.ocr.navName': 'OCR',
  'tools.tool.image.ocr.description':
    'Extrae texto de imágenes con Tesseract local y, si lo deseas, genera un PDF con texto buscable.',

  'tools.tool.image.background.name': 'Quitar fondo',
  'tools.tool.image.background.navName': 'Quitar fondo',
  'tools.tool.image.background.description':
    'Elimina fondos de imágenes localmente y guarda un PNG transparente o un color elegido.',

  'tools.tool.image.upscale.name': 'Ampliar imagen',
  'tools.tool.image.upscale.navName': 'Ampliar',
  'tools.tool.image.upscale.description':
    'Amplía imágenes 2x o 4x localmente con un paquete opcional de modelos ONNX.',

  'tools.tool.pdf.merge.name': 'Unir PDF',
  'tools.tool.pdf.merge.navName': 'Unir',
  'tools.tool.pdf.merge.description':
    'Combina varios archivos PDF en un solo documento en el orden que elijas.',

  'tools.tool.pdf.split.name': 'Dividir PDF',
  'tools.tool.pdf.split.navName': 'Dividir',
  'tools.tool.pdf.split.description':
    'Divide un PDF en páginas, rangos, bloques de páginas o marcadores.',

  'tools.tool.pdf.crop.name': 'Recortar PDF',
  'tools.tool.pdf.crop.navName': 'Recortar',
  'tools.tool.pdf.crop.description':
    'Recorta páginas PDF con márgenes predefinidos o valores personalizados, individual o por lotes.',

  'tools.tool.pdf.optimize.name': 'Optimizar PDF',
  'tools.tool.pdf.optimize.navName': 'Optimizar',
  'tools.tool.pdf.optimize.description':
    'Comprime PDF y edita el título, el autor y los metadatos del documento.',

  'tools.tool.pdf.ocr.name': 'OCR de PDF',
  'tools.tool.pdf.ocr.navName': 'OCR',
  'tools.tool.pdf.ocr.description':
    'Haz que los PDF escaneados sean buscables con OCR local y un archivo de texto adjunto.',

  'tools.tool.pdf.rotate.name': 'Rotar PDF',
  'tools.tool.pdf.rotate.navName': 'Rotar',
  'tools.tool.pdf.rotate.description':
    'Rota todas las páginas o una selección 90, 180 o 270 grados',

  'tools.tool.pdf.remove_pages.name': 'Eliminar páginas de PDF',
  'tools.tool.pdf.remove_pages.navName': 'Eliminar páginas',
  'tools.tool.pdf.remove_pages.description':
    'Elimina las páginas seleccionadas y conserva el resto del documento',

  'tools.tool.pdf.reorder.name': 'Reordenar páginas de PDF',
  'tools.tool.pdf.reorder.navName': 'Reordenar',
  'tools.tool.pdf.reorder.description':
    'Reescribe el orden de páginas con una especificación explícita, o invierte el documento',

  'tools.tool.pdf.extract_images.name': 'Extraer imágenes de PDF',
  'tools.tool.pdf.extract_images.navName': 'Extraer imágenes',
  'tools.tool.pdf.extract_images.description':
    'Extrae las imágenes incrustadas a una carpeta con nombres sin colisiones',

  'tools.tool.doc.md_to_pdf.name': 'Markdown → PDF',
  'tools.tool.doc.md_to_pdf.navName': 'Markdown',
  'tools.tool.doc.md_to_pdf.description':
    'Convierte Markdown a PDF con encabezado y pie de página opcionales.',

  'tools.tool.doc.docx_to_pdf.name': 'DOCX → PDF',
  'tools.tool.doc.docx_to_pdf.navName': 'DOCX',
  'tools.tool.doc.docx_to_pdf.description':
    'Convierte documentos DOCX a PDF en modo individual o por lotes.',

  'tools.tool.video.convert.name': 'Convertir video',
  'tools.tool.video.convert.navName': 'Convertir',
  'tools.tool.video.convert.description':
    'Convierte videos MP4, MOV, MKV y WebM con progreso y tiempo restante en vivo.',

  'tools.tool.video.trim.name': 'Recortar video',
  'tools.tool.video.trim.navName': 'Recortar',
  'tools.tool.video.trim.description':
    'Recorta videos individuales o por lotes según la hora de inicio y fin que definas.',

  'tools.tool.video.merge.name': 'Unir video',
  'tools.tool.video.merge.navName': 'Unir',
  'tools.tool.video.merge.description':
    'Combina clips de video en un solo archivo en el orden que elijas.',

  'tools.tool.video.transcribe.name': 'Transcribir video',
  'tools.tool.video.transcribe.navName': 'Transcribir',
  'tools.tool.video.transcribe.description':
    'Transcribe video y audio localmente con Whisper. Sin subidas.',

  'tools.tool.audio.extract.name': 'Extraer audio',
  'tools.tool.audio.extract.navName': 'Extraer',
  'tools.tool.audio.extract.description':
    'Extrae la pista de audio de un vídeo o archivo de audio como mp3, m4a, opus, wav o flac',

  'tools.tool.audio.convert.name': 'Convertir audio',
  'tools.tool.audio.convert.navName': 'Convertir',
  'tools.tool.audio.convert.description':
    'Convierte archivos de audio entre mp3, m4a, opus, wav y flac',

  'tools.tool.audio.normalize.name': 'Normalizar audio',
  'tools.tool.audio.normalize.navName': 'Normalizar',
  'tools.tool.audio.normalize.description':
    'Normalización de sonoridad EBU R128 en dos pasadas para objetivos de streaming, difusión o pódcast',

  'tools.tool.pdf.images.name': 'Imágenes a PDF',
  'tools.tool.pdf.images.navName': 'Imágenes a PDF',
  'tools.tool.pdf.images.description':
    'Coloca varias imágenes en una cuadrícula de página y exporta un solo PDF.',

  'tools.tool.pdf.to_docx.name': 'PDF a DOCX',
  'tools.tool.pdf.to_docx.navName': 'A DOCX',
  'tools.tool.pdf.to_docx.description':
    'Convierte un PDF en un DOCX editable conservando fuentes, grosor, colores, imágenes, tablas y diseño de página.',

  'tools.system.status.name': 'Estado del sistema',
  'tools.system.status.navName': 'Estado',
  'tools.system.status.description':
    'Inspecciona las herramientas registradas, las dependencias del entorno y el estado del backend.',

  'tools.system.automations.name': 'Automatizaciones',
  'tools.system.automations.navName': 'Automatizaciones',
  'tools.system.automations.description':
    'Vigila una carpeta y ejecuta una receta cuando llega un archivo.',

  'tools.system.duplicates.name': 'Duplicados',
  'tools.system.duplicates.navName': 'Duplicados',
  'tools.system.duplicates.description':
    'Busca archivos idénticos en tus carpetas y muestra cuánto espacio ocupan de más.',

  'tools.system.settings.name': 'Configuración',
  'tools.system.settings.navName': 'Ajustes',
  'tools.system.settings.description':
    'Define la carpeta de salida predeterminada, el tema y las preferencias del historial.',

  'tools.recipe.email_pdf.name': 'PDF por email',
  'tools.recipe.email_pdf.navName': 'PDF por email',
  'tools.recipe.email_pdf.description':
    'Une uno o varios PDF y comprime el resultado a un tamaño fácil de enviar por correo electrónico.',

  'tools.recipe.web_image_set.name': 'Set de imágenes web',
  'tools.recipe.web_image_set.navName': 'Set web',
  'tools.recipe.web_image_set.description':
    'Exporta variantes WebP y JPEG adaptables con un paquete ZIP y un fragmento srcset listo para pegar.',

  'tools.recipe.social_clip.name': 'Clip social',
  'tools.recipe.social_clip.navName': 'Clip social',
  'tools.recipe.social_clip.description':
    'Codifica un video como MP4 y WebM y exporta un fotograma de portada, listo para redes sociales.',

  'tools.recipe.clean_scan.name': 'Escaneo limpio',
  'tools.recipe.clean_scan.navName': 'Escaneo limpio',
  'tools.recipe.clean_scan.description':
    'Convierte un PDF escaneado en un PDF con texto buscable y un archivo de texto local.',

  'tools.recipe.burned_subtitles.name': 'Subtítulos incrustados',
  'tools.recipe.burned_subtitles.navName': 'Subtítulos',
  'tools.recipe.burned_subtitles.description':
    'Transcribe un video localmente e incrusta subtítulos sincronizados en un nuevo MP4.',

  'tools.recipe.archive_document.name': 'Documento de archivo',
  'tools.recipe.archive_document.navName': 'Archivo',
  'tools.recipe.archive_document.description':
    'Convierte un DOCX en un archivo PDF/A-2 e informa su SHA-256 para guardarlo a largo plazo.',
};
