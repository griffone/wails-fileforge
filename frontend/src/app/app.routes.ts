import { Routes } from '@angular/router';
import { Home } from './components/home/home';
import { ImageConverter } from './components/image-converter/image-converter';
import { ToolCatalog } from './components/tool-catalog/tool-catalog';
import { PdfMerge } from './components/pdf-merge/pdf-merge';
import { PdfSplit } from './components/pdf-split/pdf-split';
import { PdfCrop } from './components/pdf-crop/pdf-crop';
import { VideoConvert } from './components/video-convert/video-convert';
import { VideoTrim } from './components/video-trim/video-trim';

export const routes: Routes = [
  { path: '', component: Home },
  { path: 'image-converter', component: ImageConverter },
  { path: 'tool-catalog', component: ToolCatalog },
  { path: 'pdf-merge', component: PdfMerge },
  { path: 'pdf-split', component: PdfSplit },
  { path: 'pdf-crop', component: PdfCrop },
  { path: 'video-convert', component: VideoConvert },
  { path: 'video-trim', component: VideoTrim },
  { path: '**', redirectTo: '' },
];
