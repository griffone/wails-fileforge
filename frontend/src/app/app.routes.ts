import { Routes } from '@angular/router';
import { Home } from './components/home/home';
import { ImageConverter } from './components/image-converter/image-converter';
import { ToolCatalog } from './components/tool-catalog/tool-catalog';
import { ImageCrop } from './components/image-crop/image-crop';
import { ImageAnnotate } from './components/image-annotate/image-annotate';
import { PdfMerge } from './components/pdf-merge/pdf-merge';
import { PdfSplit } from './components/pdf-split/pdf-split';
import { PdfCrop } from './components/pdf-crop/pdf-crop';
import { PdfOptimize } from './components/pdf-optimize/pdf-optimize';
import { PdfOcr } from './components/pdf-ocr/pdf-ocr';
import { DocMdToPdf } from './components/doc-md-to-pdf/doc-md-to-pdf';
import { DocDocxToPdf } from './components/doc-docx-to-pdf/doc-docx-to-pdf';
import { VideoConvert } from './components/video-convert/video-convert';
import { VideoTrim } from './components/video-trim/video-trim';
import { VideoMerge } from './components/video-merge/video-merge';
import { VideoTranscribe } from './components/video-transcribe/video-transcribe';
import { Settings } from './components/settings/settings';
import { RecipeRun } from './components/recipe-run/recipe-run';
import { Automations } from './components/automations/automations';

export const routes: Routes = [
  { path: '', component: Home },
  { path: 'image-converter', component: ImageConverter },
  { path: 'tool-catalog', component: ToolCatalog },
  { path: 'settings', component: Settings },
  { path: 'automations', component: Automations },
  { path: 'image-crop', component: ImageCrop },
  { path: 'image-annotate', component: ImageAnnotate },
  { path: 'pdf-merge', component: PdfMerge },
  { path: 'pdf-split', component: PdfSplit },
  { path: 'pdf-crop', component: PdfCrop },
  { path: 'pdf-optimize', component: PdfOptimize },
  { path: 'pdf-ocr', component: PdfOcr },
  { path: 'doc-md-to-pdf', component: DocMdToPdf },
  { path: 'doc-docx-to-pdf', component: DocDocxToPdf },
  { path: 'video-convert', component: VideoConvert },
  { path: 'video-trim', component: VideoTrim },
  { path: 'video-merge', component: VideoMerge },
  { path: 'video-transcribe', component: VideoTranscribe },
  { path: 'recipe/email-pdf', component: RecipeRun, data: { recipeId: 'recipe.email_pdf' } },
  {
    path: 'recipe/web-image-set',
    component: RecipeRun,
    data: { recipeId: 'recipe.web_image_set' },
  },
  {
    path: 'recipe/social-clip',
    component: RecipeRun,
    data: { recipeId: 'recipe.social_clip' },
  },
  {
    path: 'recipe/clean-scan',
    component: RecipeRun,
    data: { recipeId: 'recipe.clean_scan' },
  },
  { path: '**', redirectTo: '' },
];
