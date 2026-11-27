import { Routes } from '@angular/router';
import { Home } from './components/home/home';

/**
 * Every tool page is a lazy route so the initial bundle only carries the shell.
 * Home stays eager: it is the landing route and must paint without waiting for
 * a chunk.
 */
export const routes: Routes = [
  { path: '', component: Home },
  {
    path: 'image-converter',
    loadComponent: () =>
      import('./components/image-converter/image-converter').then(
        (m) => m.ImageConverter,
      ),
  },
  {
    path: 'tool-catalog',
    loadComponent: () =>
      import('./components/tool-catalog/tool-catalog').then(
        (m) => m.ToolCatalog,
      ),
  },
  {
    path: 'settings',
    loadComponent: () =>
      import('./components/settings/settings').then((m) => m.Settings),
  },
  {
    path: 'automations',
    loadComponent: () =>
      import('./components/automations/automations').then((m) => m.Automations),
  },
  {
    path: 'duplicates',
    loadComponent: () =>
      import('./components/duplicates/duplicates').then((m) => m.Duplicates),
  },
  {
    path: 'image-crop',
    loadComponent: () =>
      import('./components/image-crop/image-crop').then((m) => m.ImageCrop),
  },
  {
    path: 'image-annotate',
    loadComponent: () =>
      import('./components/image-annotate/image-annotate').then(
        (m) => m.ImageAnnotate,
      ),
  },
  {
    path: 'image-compress',
    loadComponent: () =>
      import('./components/image-compress/image-compress').then(
        (m) => m.ImageCompress,
      ),
  },
  {
    path: 'image-watermark',
    loadComponent: () =>
      import('./components/image-watermark/image-watermark').then(
        (m) => m.ImageWatermark,
      ),
  },
  {
    path: 'image-ocr',
    loadComponent: () =>
      import('./components/image-ocr/image-ocr').then((m) => m.ImageOcr),
  },
  {
    path: 'image-background',
    loadComponent: () =>
      import('./components/image-background/image-background').then(
        (m) => m.ImageBackground,
      ),
  },
  {
    path: 'image-upscale',
    loadComponent: () =>
      import('./components/image-upscale/image-upscale').then(
        (m) => m.ImageUpscale,
      ),
  },
  {
    path: 'pdf-merge',
    loadComponent: () =>
      import('./components/pdf-merge/pdf-merge').then((m) => m.PdfMerge),
  },
  {
    path: 'pdf-split',
    loadComponent: () =>
      import('./components/pdf-split/pdf-split').then((m) => m.PdfSplit),
  },
  {
    path: 'pdf-crop',
    loadComponent: () =>
      import('./components/pdf-crop/pdf-crop').then((m) => m.PdfCrop),
  },
  {
    path: 'pdf-optimize',
    loadComponent: () =>
      import('./components/pdf-optimize/pdf-optimize').then(
        (m) => m.PdfOptimize,
      ),
  },
  {
    path: 'pdf-ocr',
    loadComponent: () =>
      import('./components/pdf-ocr/pdf-ocr').then((m) => m.PdfOcr),
  },
  {
    path: 'pdf-rotate',
    loadComponent: () =>
      import('./components/pdf-page-ops/pdf-rotate').then((m) => m.PdfRotate),
  },
  {
    path: 'pdf-remove-pages',
    loadComponent: () =>
      import('./components/pdf-page-ops/pdf-remove-pages').then(
        (m) => m.PdfRemovePages,
      ),
  },
  {
    path: 'pdf-reorder',
    loadComponent: () =>
      import('./components/pdf-page-ops/pdf-reorder').then(
        (m) => m.PdfReorder,
      ),
  },
  {
    path: 'pdf-extract-images',
    loadComponent: () =>
      import('./components/pdf-page-ops/pdf-extract-images').then(
        (m) => m.PdfExtractImages,
      ),
  },
  {
    path: 'pdf-to-docx',
    loadComponent: () =>
      import('./components/pdf-to-docx/pdf-to-docx').then(
        (m) => m.PdfToDocx,
      ),
  },
  {
    path: 'pdf-from-images',
    loadComponent: () =>
      import('./components/pdf-from-images/pdf-from-images').then(
        (m) => m.PdfFromImages,
      ),
  },
  {
    path: 'doc-md-to-pdf',
    loadComponent: () =>
      import('./components/doc-md-to-pdf/doc-md-to-pdf').then(
        (m) => m.DocMdToPdf,
      ),
  },
  {
    path: 'doc-docx-to-pdf',
    loadComponent: () =>
      import('./components/doc-docx-to-pdf/doc-docx-to-pdf').then(
        (m) => m.DocDocxToPdf,
      ),
  },
  {
    path: 'video-convert',
    loadComponent: () =>
      import('./components/video-convert/video-convert').then(
        (m) => m.VideoConvert,
      ),
  },
  {
    path: 'video-trim',
    loadComponent: () =>
      import('./components/video-trim/video-trim').then((m) => m.VideoTrim),
  },
  {
    path: 'video-merge',
    loadComponent: () =>
      import('./components/video-merge/video-merge').then((m) => m.VideoMerge),
  },
  {
    path: 'video-transcribe',
    loadComponent: () =>
      import('./components/video-transcribe/video-transcribe').then(
        (m) => m.VideoTranscribe,
      ),
  },
  {
    path: 'web-download',
    loadComponent: () =>
      import('./components/web-download/web-download').then(
        (m) => m.WebDownload,
      ),
  },
  {
    path: 'audio-extract',
    loadComponent: () =>
      import('./components/audio/audio-extract').then((m) => m.AudioExtract),
  },
  {
    path: 'audio-convert',
    loadComponent: () =>
      import('./components/audio/audio-convert').then((m) => m.AudioConvert),
  },
  {
    path: 'audio-normalize',
    loadComponent: () =>
      import('./components/audio/audio-normalize').then(
        (m) => m.AudioNormalize,
      ),
  },
  {
    path: 'recipe/email-pdf',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
    data: { recipeId: 'recipe.email_pdf' },
  },
  {
    path: 'recipe/web-image-set',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
    data: { recipeId: 'recipe.web_image_set' },
  },
  {
    path: 'recipe/social-clip',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
    data: { recipeId: 'recipe.social_clip' },
  },
  {
    path: 'recipe/clean-scan',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
    data: { recipeId: 'recipe.clean_scan' },
  },
  {
    path: 'recipe/burned-subtitles',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
    data: { recipeId: 'recipe.burned_subtitles' },
  },
  {
    path: 'recipe/archive-document',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
    data: { recipeId: 'recipe.archive_document' },
  },
  {
    path: 'recipe/user/:recipeId',
    loadComponent: () =>
      import('./components/recipe-run/recipe-run').then((m) => m.RecipeRun),
  },
  { path: '**', redirectTo: '' },
];
