import { Routes } from '@angular/router';
import { Home } from './components/home/home';
import { ImageConverter } from './components/image-converter/image-converter';

export const routes: Routes = [
  { path: '', component: Home },
  { path: 'image-converter', component: ImageConverter },
  { path: '**', redirectTo: '' },
];
