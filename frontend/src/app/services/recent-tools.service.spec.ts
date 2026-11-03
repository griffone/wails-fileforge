import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { RecentToolsService } from './recent-tools.service';

describe('RecentToolsService', () => {
  const storageKey = 'ff-recent-tools';

  function storedRoutes(): string[] {
    const raw = localStorage.getItem(storageKey);
    return raw ? (JSON.parse(raw) as string[]) : [];
  }

  function createService(): RecentToolsService {
    TestBed.configureTestingModule({ providers: [provideRouter([])] });
    return TestBed.inject(RecentToolsService);
  }

  beforeEach(() => {
    localStorage.removeItem(storageKey);
  });

  afterEach(() => {
    localStorage.removeItem(storageKey);
  });

  it('prepends the most recently opened tool', () => {
    const service = createService();

    service.record('/pdf-merge');
    service.record('/image-crop');

    expect(service.recent().map((tool) => tool.route)).toEqual(['/image-crop', '/pdf-merge']);
  });

  it('deduplicates tools that were already recorded', () => {
    const service = createService();

    service.record('/pdf-merge');
    service.record('/image-converter');
    service.record('/pdf-merge');

    expect(service.recent().map((tool) => tool.route)).toEqual(['/pdf-merge', '/image-converter']);
  });

  it('caps the history at four tools', () => {
    const service = createService();

    service.record('/image-converter');
    service.record('/image-crop');
    service.record('/image-annotate');
    service.record('/pdf-merge');
    service.record('/pdf-split');

    expect(service.recent().length).toBe(4);
    expect(service.recent()[0].route).toBe('/pdf-split');
  });

  it('ignores Home, System Status and unknown routes', () => {
    const service = createService();

    service.record('/');
    service.record('/tool-catalog');
    service.record('/not-a-tool');

    expect(service.recent()).toEqual([]);
    expect(storedRoutes()).toEqual([]);
  });

  it('persists routes and reloads them into a fresh service', () => {
    const service = createService();

    service.record('/pdf-merge');
    service.record('/image-crop');

    expect(storedRoutes()).toEqual(['/image-crop', '/pdf-merge']);

    TestBed.resetTestingModule();
    const reloaded = createService();

    expect(reloaded.recent().map((tool) => tool.route)).toEqual(['/image-crop', '/pdf-merge']);
  });

  it('ignores corrupted storage values', () => {
    localStorage.setItem(storageKey, '{not json');
    const corrupted = createService();
    expect(corrupted.recent()).toEqual([]);

    TestBed.resetTestingModule();
    localStorage.setItem(storageKey, JSON.stringify(['/pdf-merge', 42, '/unknown-route']));
    const partlyValid = createService();

    expect(partlyValid.recent().map((tool) => tool.route)).toEqual(['/pdf-merge']);
  });
});
