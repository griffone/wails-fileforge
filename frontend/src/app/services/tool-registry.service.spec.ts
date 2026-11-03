import { TestBed } from '@angular/core/testing';

import { routes } from '../app.routes';
import { ToolRegistryService } from './tool-registry.service';

describe('ToolRegistryService', () => {
  let service: ToolRegistryService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(ToolRegistryService);
  });

  it('exposes the eleven conversion tools', () => {
    expect(service.tools.length).toBe(11);
  });

  it('exposes thirteen navigation entries', () => {
    expect(service.navigation.length).toBe(13);
    expect(service.navigation[0].route).toBe('/');
    expect(service.navigation[service.navigation.length - 1].route).toBe('/tool-catalog');
  });

  it('keeps routes and ids unique', () => {
    const routesSeen = new Set(service.navigation.map((item) => item.route));
    const idsSeen = new Set(service.navigation.map((item) => item.id));

    expect(routesSeen.size).toBe(service.navigation.length);
    expect(idsSeen.size).toBe(service.navigation.length);
  });

  it('resolves entries by route, including Home and System Status', () => {
    expect(service.findByRoute('/')?.name).toBe('Home');
    expect(service.findByRoute('/image-converter')?.name).toBe('Image Convert');
    expect(service.findByRoute('/tool-catalog')?.name).toBe('System Status');
    expect(service.findByRoute('/unknown-route')).toBeNull();
    expect(service.findByRoute(undefined)).toBeNull();
  });

  it('normalises URLs with queries, hashes and trailing slashes', () => {
    expect(service.findByRoute('/pdf-merge?mode=batch#output')?.name).toBe('PDF Merge');
    expect(service.findByRoute('/pdf-merge/')?.name).toBe('PDF Merge');
    expect(service.findByRoute('pdf-merge')?.name).toBe('PDF Merge');
  });

  it('only treats conversion entries as tools', () => {
    const home = service.findByRoute('/');
    const system = service.findByRoute('/tool-catalog');
    const tool = service.findByRoute('/video-trim');

    expect(home && service.isTool(home)).toBeFalse();
    expect(system && service.isTool(system)).toBeFalse();
    expect(tool && service.isTool(tool)).toBeTrue();
  });

  it('groups every navigation entry exactly once', () => {
    const grouped = service.navGroups.reduce((total, group) => total + group.items.length, 0);

    expect(service.navGroups.length).toBe(6);
    expect(grouped).toBe(service.navigation.length);
    expect(service.navGroups.map((group) => group.label)).toEqual([
      'Workspace',
      'Image',
      'PDF',
      'Documents',
      'Video',
      'System',
    ]);
  });

  it('labels categories', () => {
    expect(service.categoryLabel('docs')).toBe('Documents');
    expect(service.categoryLabel('pdf')).toBe('PDF');
  });

  it('matches every tool route declared in app.routes.ts', () => {
    const declaredRoutes = new Set(
      routes
        .filter((route) => route.path !== '**')
        .map((route) => (route.path === '' ? '/' : `/${route.path}`)),
    );

    for (const tool of service.tools) {
      expect(declaredRoutes.has(tool.route))
        .withContext(`route ${tool.route} is declared in app.routes.ts`)
        .toBeTrue();
    }
  });
});
