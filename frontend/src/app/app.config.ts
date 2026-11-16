import {
  ApplicationConfig,
  provideBrowserGlobalErrorListeners,
  provideZoneChangeDetection,
} from '@angular/core';
import {
  RouteReuseStrategy,
  provideRouter,
  withRouterConfig,
} from '@angular/router';
import { provideHttpClient } from '@angular/common/http';

import { routes } from './app.routes';
import { ReloadRouteReuseStrategy } from './app-route-reuse.strategy';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideZoneChangeDetection({ eventCoalescing: true }),
    provideRouter(routes, withRouterConfig({ onSameUrlNavigation: 'reload' })),
    { provide: RouteReuseStrategy, useClass: ReloadRouteReuseStrategy },
    provideHttpClient(),
  ],
};
