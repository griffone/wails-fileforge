import { BaseRouteReuseStrategy } from '@angular/router';

/**
 * The workbench routes are flat and carry no state worth preserving between
 * navigations. Recreating the page on every navigation also makes a reload of
 * the already-active tool run its `ngOnInit`, which is how the global drop
 * overlay hands files to the active tool.
 */
export class ReloadRouteReuseStrategy extends BaseRouteReuseStrategy {
  override shouldReuseRoute(): boolean {
    return false;
  }
}
