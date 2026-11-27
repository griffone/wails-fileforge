import { bootstrapApplication } from '@angular/platform-browser';

import { App } from './app/app';
import { appConfig } from './app/app.config';
import { applyBootstrapRoute } from './app/utils/bootstrap-route';

// A secondary window is opened at `/?route=<tool>`: rewrite the address before
// the router starts so the first navigation is the tool, not Home.
applyBootstrapRoute(window.location, window.history);

bootstrapApplication(App, appConfig).catch((err) => console.error(err));
