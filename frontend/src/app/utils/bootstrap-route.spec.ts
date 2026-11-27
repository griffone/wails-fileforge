import {
  applyBootstrapRoute,
  bootstrapRouteFromSearch,
} from './bootstrap-route';

describe('bootstrap route handoff', () => {
  describe('bootstrapRouteFromSearch', () => {
    it('reads the tool route from the window parameter', () => {
      expect(bootstrapRouteFromSearch('?route=%2Fimage-crop')).toBe('/image-crop');
      expect(bootstrapRouteFromSearch('route=/pdf-merge')).toBe('/pdf-merge');
    });

    it('normalises a trailing slash', () => {
      expect(bootstrapRouteFromSearch('?route=%2Fimage-crop%2F')).toBe('/image-crop');
    });

    it('ignores a missing, empty or home route', () => {
      expect(bootstrapRouteFromSearch('')).toBeNull();
      expect(bootstrapRouteFromSearch('?route=')).toBeNull();
      expect(bootstrapRouteFromSearch('?route=%2F')).toBeNull();
      expect(bootstrapRouteFromSearch('?other=%2Fimage-crop')).toBeNull();
    });

    it('rejects absolute URLs, protocol-relative URLs and queries', () => {
      expect(bootstrapRouteFromSearch('?route=https%3A%2F%2Fevil.test%2Fimage-crop')).toBeNull();
      expect(bootstrapRouteFromSearch('?route=%2F%2Fevil.test%2Fimage-crop')).toBeNull();
      expect(bootstrapRouteFromSearch('?route=image-crop')).toBeNull();
      expect(bootstrapRouteFromSearch('?route=%2Fimage-crop%3Fx%3D1')).toBeNull();
      expect(bootstrapRouteFromSearch('?route=%2Fimage-crop%23top')).toBeNull();
    });
  });

  describe('applyBootstrapRoute', () => {
    it('rewrites the address before the router starts', () => {
      const replaceState = jasmine.createSpy('replaceState');

      const applied = applyBootstrapRoute(
        { search: '?route=%2Fimage-crop' },
        { replaceState },
      );

      expect(applied).toBe('/image-crop');
      expect(replaceState).toHaveBeenCalledWith(null, '', '/image-crop');
    });

    it('leaves the address alone without a route parameter', () => {
      const replaceState = jasmine.createSpy('replaceState');

      const applied = applyBootstrapRoute({ search: '' }, { replaceState });

      expect(applied).toBeNull();
      expect(replaceState).not.toHaveBeenCalled();
    });

    it('survives a refused rewrite', () => {
      const replaceState = jasmine.createSpy('replaceState').and.throwError('SecurityError');

      expect(
        applyBootstrapRoute({ search: '?route=%2Fimage-crop' }, { replaceState }),
      ).toBeNull();
    });
  });
});
