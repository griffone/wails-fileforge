import { Routes } from '@angular/router';
import { Home } from './components/home/home';
import { ImageConverter } from './components/image-converter/image-converter';
import { ToolCatalog } from './components/tool-catalog/tool-catalog';
import { ImageCrop } from './components/image-crop/image-crop';
import { PdfMerge } from './components/pdf-merge/pdf-merge';
import { PdfSplit } from './components/pdf-split/pdf-split';
import { PdfCrop } from './components/pdf-crop/pdf-crop';
import { VideoConvert } from './components/video-convert/video-convert';
import { VideoTrim } from './components/video-trim/video-trim';
import { VideoMerge } from './components/video-merge/video-merge';

export const routes: Routes = [
  { path: '', component: Home },
  { path: 'image-converter', component: ImageConverter },
  { path: 'tool-catalog', component: ToolCatalog },
  { path: 'image-crop', component: ImageCrop },
  { path: 'pdf-merge', component: PdfMerge },
  { path: 'pdf-split', component: PdfSplit },
  { path: 'pdf-crop', component: PdfCrop },
  { path: 'video-convert', component: VideoConvert },
  { path: 'video-trim', component: VideoTrim },
  { path: 'video-merge', component: VideoMerge },
  { path: '**', redirectTo: '' },
];
