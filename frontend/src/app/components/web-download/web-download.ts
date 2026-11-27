import { Component } from '@angular/core';

/**
 * Downloader page shell. The backend tool and its registry entry land first;
 * the downloader UI slice fills in the link queue, per-link formats, quality
 * picker and live progress.
 */
@Component({
  selector: 'app-web-download',
  standalone: true,
  template: `
    <div class="page">
      <header class="page-head">
        <h1 class="page-title">Download <em>Media</em></h1>
        <p class="page-sub">
          Paste a link to download the exact video resolution or audio bitrate
          you choose. Everything runs on this machine.
        </p>
      </header>

      <section class="panel">
        <h2 class="panel-title">Link queue</h2>
        <p class="panel-sub">
          The downloader form is not available in this build yet.
        </p>
      </section>
    </div>
  `,
})
export class WebDownload {}
