import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';

import { routes } from './app.routes';
import { Wails } from './services/wails';
import { clearToolStorage } from '../testing/tool-storage';

interface LazyRouteSample {
  readonly path: string;
  readonly tag: string;
  readonly selector: string;
  /** Defaults to true; set false when the anchor is a form control. */
  readonly expectText?: boolean;
}

/**
 * One representative route per tool group plus the system pages. The router
 * only activates a `loadComponent` route after the dynamic import settles, so
 * the component tag being present proves the chunk resolved and mounted, and
 * the anchor proves its template rendered.
 *
 * `system.status` routes to `/tool-catalog`, so one navigation covers both the
 * ToolCatalog and Status entries. Home stays eager and is not exercised here;
 * `app.spec.ts` covers it.
 */
const LAZY_ROUTES: readonly LazyRouteSample[] = [
  { path: '/image-crop', tag: 'app-image-crop', selector: 'h1.page-title' },
  { path: '/pdf-merge', tag: 'app-pdf-merge', selector: 'h1.page-title' },
  { path: '/video-trim', tag: 'app-video-trim', selector: 'h1.page-title' },
  {
    path: '/doc-md-to-pdf',
    tag: 'app-doc-md-to-pdf',
    selector: 'h1.page-title',
  },
  {
    path: '/audio-convert',
    tag: 'app-audio-convert',
    selector: 'h1.page-title',
  },
  {
    path: '/pdf-rotate',
    tag: 'app-pdf-rotate',
    selector: '#pdf-rotate-pages',
    // The anchor is a bare input, so presence is the meaningful assertion.
    expectText: false,
  },
  { path: '/duplicates', tag: 'app-duplicates', selector: '.page-readonly' },
  { path: '/settings', tag: 'app-settings', selector: '#settings-output-title' },
  {
    path: '/automations',
    tag: 'app-automations',
    selector: '#automation-summary-title',
  },
  {
    path: '/tool-catalog',
    tag: 'app-tool-catalog',
    selector: '#catalog-runtime-title',
  },
];

/**
 * Minimal Wails double. Known init calls get explicit empty successes so the
 * pages settle into their empty state; every other method resolves to a
 * failure response the components already handle, and event streams (`$`
 * properties) read as absent, exactly like the browser preview.
 */
function createWailsStub(): Wails {
  const known: Record<string, unknown> = {
    isRuntimeAvailable: () => false,
    listDuplicateScans: () =>
      Promise.resolve({ success: true, message: 'ok', scans: [] }),
    listTools: () => Promise.resolve({ success: true, message: 'ok', tools: [] }),
    listRecipes: () =>
      Promise.resolve({ success: true, message: 'ok', recipes: [] }),
    listAutomationRules: () =>
      Promise.resolve({ success: true, message: 'ok', rules: [] }),
    getAutomationStatus: () =>
      Promise.resolve({
        success: true,
        message: 'ok',
        totalRules: 0,
        activeRules: 0,
      }),
    listAutomationRuns: () =>
      Promise.resolve({ success: true, message: 'ok', runs: [] }),
  };

  const unavailable = () =>
    Promise.resolve({ success: false, message: 'unavailable' });

  return new Proxy(known, {
    get(target, property) {
      if (Reflect.has(target, property)) {
        return Reflect.get(target, property);
      }
      if (typeof property === 'string' && property.endsWith('$')) {
        return undefined;
      }
      return unavailable;
    },
  }) as unknown as Wails;
}

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
    TestBed.configureTestingModule({
      providers: [provideRouter(routes), { provide: Wails, useValue: createWailsStub() }],
    });

    const harness = await RouterTestingHarness.create(LAZY_ROUTES[0].path);

    for (const route of LAZY_ROUTES) {
      if (route.path !== LAZY_ROUTES[0].path) {
        await harness.navigateByUrl(route.path);
      }

      const element = harness.routeNativeElement;
      expect(element)
        .withContext(`${route.path} activates its routed component`)
        .not.toBeNull();
      expect(element?.tagName.toLowerCase())
        .withContext(`${route.path} lazy chunk mounted ${route.tag}`)
        .toBe(route.tag);

      const anchor = element?.querySelector(route.selector);
      expect(anchor)
        .withContext(`${route.path} renders ${route.selector}`)
        .not.toBeNull();

      if (route.expectText !== false) {
        expect((anchor?.textContent ?? '').trim().length)
          .withContext(`${route.path} renders page content`)
          .toBeGreaterThan(0);
      }
    }
  });
});
