import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { routes } from './app.routes';
import { Wails } from './services/wails';
import { clearToolStorage } from '../testing/tool-storage';

/**
 * The five routes this branch lazy-loads. `loadComponent` is exercised by the
 * router, so each expectation proves the chunk resolves and the page renders.
 */
const LAZY_ROUTES = [
  { path: '/pdf-rotate', selector: '#pdf-rotate-pages' },
  { path: '/pdf-remove-pages', selector: '#pdf-remove-pages' },
  { path: '/pdf-reorder', selector: '#pdf-reorder-order' },
  { path: '/pdf-extract-images', selector: '#pdf-extract-images-pages' },
  { path: '/duplicates', selector: '.page-readonly' },
] as const;

describe('lazy workbench routes', () => {
  beforeEach(() => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  afterEach(() => {
    localStorage.removeItem('ff-preferences');
    clearToolStorage();
  });

  it('renders each lazily loaded page on navigation', async () => {
    const wailsSpy = jasmine.createSpyObj<Wails>('Wails', [
      'isRuntimeAvailable',
      'listDuplicateScans',
    ]);
    wailsSpy.isRuntimeAvailable.and.returnValue(false);
    wailsSpy.listDuplicateScans.and.resolveTo({
      success: true,
      message: 'ok',
      scans: [],
    });

    TestBed.configureTestingModule({
      providers: [
        provideRouter(routes),
        { provide: Wails, useValue: wailsSpy },
      ],
    });

    const harness = await RouterTestingHarness.create(LAZY_ROUTES[0].path);

    for (const route of LAZY_ROUTES) {
      if (route.path !== LAZY_ROUTES[0].path) {
        await harness.navigateByUrl(route.path);
      }

      expect(harness.routeNativeElement?.querySelector(route.selector))
        .withContext(`${route.path} renders after lazy loading`)
        .not.toBeNull();
    }
  });
});
