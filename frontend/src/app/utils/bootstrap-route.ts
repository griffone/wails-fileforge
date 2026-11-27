/**
 * Bootstrap route handoff.
 *
 * The window service opens a secondary window at `/?route=/image-crop`. Reading
 * that parameter and rewriting the URL before Angular starts means the router's
 * initial navigation lands on the tool directly: Home is never rendered first,
 * so there is no flash to correct afterwards.
 */

/** Query parameter used by the backend when it opens a tool window. */
export const BOOTSTRAP_ROUTE_PARAM = 'route';

/**
 * Reads the route from a URL search string and returns it in canonical form, or
 * null when the value is missing or not a safe in-app path.
 */
export function bootstrapRouteFromSearch(search: string): string | null {
  const params = new URLSearchParams(search.startsWith('?') ? search.slice(1) : search);
  const raw = params.get(BOOTSTRAP_ROUTE_PARAM);
  if (!raw) {
    return null;
  }

  const candidate = raw.trim();
  if (!candidate.startsWith('/') || candidate.startsWith('//')) {
    return null;
  }
  if (candidate.includes('://') || candidate.includes('?') || candidate.includes('#')) {
    return null;
  }

  const route = candidate.replace(/\/+$/, '') || '/';

  return route === '/' ? null : route;
}

/**
 * Applies the bootstrap route to the current location before the Angular app
 * starts. Returns the applied route, or null when nothing was rewritten.
 */
export function applyBootstrapRoute(
  location: { search: string },
  history: { replaceState(state: unknown, title: string, url: string): void },
): string | null {
  const route = bootstrapRouteFromSearch(location.search);
  if (!route) {
    return null;
  }

  try {
    history.replaceState(null, '', route);
  } catch {
    // A refused rewrite (an unusual origin) is not worth failing startup for:
    // the app still boots at its default route.
    return null;
  }

  return route;
}
