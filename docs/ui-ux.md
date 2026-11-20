# FileForge UI/UX Guide

The interface is a **precision workbench**: quiet, dense enough for repeated use, and built around one accent. The goal is that every tool feels like part of the same instrument instead of a separate screen.

## Design principles

1. **Local-first honesty.** The product never uploads files. The UI says so once per page, quietly, instead of repeating it everywhere.
2. **One accent.** Molten amber marks the primary action, the active destination and focus. Everything else is graphite, paper and muted metadata.
3. **Structure over decoration.** One page header, one panel grammar, one field grammar. No per-page chrome.
4. **Keyboard first, pointer friendly.** `Ctrl/Cmd+K` reaches every tool. Palettes, drop zones and icon-only controls have keyboard paths and labels.
5. **Editorial detail, used once.** Instrument Serif italic appears only on the last word of a page title and in the brand voice. It is not used for labels or body copy.
6. **Honest states.** Loading, empty, preview-only, partial success and failure are distinct states, not generic gray text.

## Tokens

All tokens live in `frontend/src/styles.css`. Use them through the CSS variables or the component classes; never hardcode a gray/blue/hex value in a page.

| Token | Dark | Light | Use |
| --- | --- | --- | --- |
| `--bg` | `#0b0d10` | `#f5f1e8` | App background |
| `--surface` | `#12151a` | `#fffdf8` | Panels, cards |
| `--surface-2` | `#171b21` | `#f2ece1` | Inputs, inset areas |
| `--border` | `#262c35` | `#ddd4c4` | Hairlines |
| `--text` | `#f4f1ea` | `#171512` | Primary text |
| `--text-soft` | `#c9c4b9` | `#3d3830` | Secondary text |
| `--muted` | `#8b949f` | `#6f675b` | Metadata, hints |
| `--accent` | `#ff8a3d` | `#c2410c` | Primary action, active nav |
| `--accent-ink` | `#1c1005` | `#fff8f0` | Text on accent |
| `--success` / `--warning` / `--danger` / `--info` | theme-specific | theme-specific | Status only |

Existing Tailwind color utilities are remapped in the `@theme` block: `gray`/`slate`/`zinc`/`neutral`/`stone` map to structure tokens, `blue` to the accent, `amber` to warning, `red` to danger, `green`/`emerald` to success. This keeps older markup theme-aware; new markup should use the semantic classes instead.

### Typography

- `--font-sans` / `--font-display`: **Archivo Variable**
- `--font-serif`: **Instrument Serif** (italic accent only)
- `--font-mono`: **IBM Plex Mono** (metadata, paths, shortcuts, counts)
- Fonts are bundled with `@fontsource`; the app makes no font network requests.

## Shell anatomy

`app.html` renders one frame:

```
.app-frame
  .rail                 brand, nav groups, runtime + theme footer
  .app-body
    .topbar             breadcrumb + command palette trigger
    .app-content        <router-outlet>
app-command-palette     overlay, opened by Ctrl/Cmd+K
```

- Navigation comes from `ToolRegistryService.navGroups`. Do not hardcode links in pages.
- The active item gets an accent dot/bar and `aria-current="page"`.
- At `<= 900px` the rail becomes a drawer with a backdrop; the top bar exposes the menu button.
- The top bar shows `CATEGORY / Short name` using the registry's `navName`, not the full name.

## File-first tool flow

Every tool follows the same user task, not the backend job contract:

> **Drop files → see the file cards → choose a simple outcome → see the exact output plan → one action → open/reveal the result.**

- Intake: `FileDrop` (native Wails paths) + `FileQueue`. One file is single mode; 2+ becomes batch automatically. No mode selector.
- Outcome: presets first (WebP/JPEG/PNG, MP4/WebM, crop size, page strategy); exact settings live under `Advanced`.
- Output: `OutputPlanCard` shows the resolved destination and collision-safe name before running. Default is next to the original; never overwrite.
- Run: one primary action (`Convert 3 images`, `Merge 2 PDFs`). There is no separate Validate button.
- Result: `ResultCard` with open, show in folder, retry failed and "do another"; technical job ids/codes live in a Details disclosure.
- Backend: `PlanJob` is the source of truth. The same resolver is used by planning and execution; explicit existing outputs are conflicts, auto-derived names get `-2`, `-3`.
- Native desktop: `EnableFileDrop` + `data-file-drop-target` + `common:WindowFilesDropped`; `OpenPath` / `RevealInFolder` for results.

Shared components:

- `app-file-drop` — click, Enter/Space, native OS paths, drag state.
- `app-file-queue` — file cards, optional reorder, remove.
- `app-output-plan-card` — destination, resolved names, counts, adjusted warnings.
- `app-result-card` — success/partial/failure, open/reveal, retry, details.
- `app-job-card` — live progress.

## Page recipe

Every tool page uses the same composition:

```html
<div class="page">
  <header class="page-head">
    <div class="page-head-row">
      <div>
        <p class="eyebrow">IMAGE</p>
        <h1 class="page-title">Image <em>Crop</em></h1>
        <p class="page-sub">One sentence ending with the local-first note.</p>
      </div>
      <span class="badge badge-neutral page-local"><!-- shield --> Local only</span>
    </div>
  </header>

  <div class="page-stack">
    <section class="panel">
      <div class="panel-head">
        <div>
          <h2 class="panel-title">Input images</h2>
          <p class="panel-sub">Short helper.</p>
        </div>
        <span class="badge badge-neutral">0 files</span>
      </div>
      <!-- body -->
    </section>
  </div>
</div>
```

Rules:

- No `min-h-screen`, `bg-gray-50`, page-level back links or Home/Catalog buttons.
- Use `.panel`/`.panel-head`/`.panel-title`/`.panel-sub` for sections.
- Use `.field`/`.label`/`.input`/`.select`/`.textarea`; every control needs a `for`/`id` pair.
- Use `.banner banner-info|success|warning|danger` for messages. Errors use `role="alert"`; progress/status uses `role="status"` + `aria-live="polite"`.
- Use `.btn btn-primary|secondary|ghost|danger`, `.btn-sm`, `.icon-btn`. Icon-only buttons need `aria-label`.
- Use `.tool-icon[data-tone="image|pdf|docs|video|system"]` for category icon tiles. The tone is the only place category color appears.
- Use `.empty-state` for empty lists, not a gray paragraph.
- Job progress belongs to `app-job-card`; do not duplicate stage/percent/ETA blocks.

## Shared components

### `app-file-drop`

```html
<app-file-drop
  accept=".pdf,application/pdf"
  [multiple]="true"
  ariaLabel="PDF file drop zone"
  hint="PDF only"
  (filesSelected)="onFilesSelected($event)"
/>
```

- Emits `File[]`.
- Supports click, Enter and Space, drag state and a described-by hint.
- In a real Wails webview, prefer the native picker for backend paths; use the emitted `File` path when available.

### `app-job-card`

- Input: `job: JobResult | null`.
- Single progress bar, stage badge, ETA, spinner while finalizing, live-region announcements.
- Do not wrap it in another `aria-live` region.

## Adding a tool

1. Add the route in `frontend/src/app/app.routes.ts`.
2. Add a `ToolDefinition` in `frontend/src/app/config/tools.ts` (name, `navName`, description, route, category, icon, formats, keywords).
3. The rail, command palette, Home grid and recents pick it up automatically.
4. Build the page with the file-first flow: `FileDrop`/`FileQueue` → presets/advanced → `OutputPlanCard` → one action → `ResultCard`. Use `.page`, `.panel`, fields, banners and buttons.
5. Implement `Plan(ctx, req)` on the Go tool and reuse the same resolver in execution; never overwrite, and return planned outputs/counts/warnings. Expose it through the existing `PlanJob` path.
6. Add `Local only` once in the header; no back links, no mode selector, no output-path typing, no Validate button.
7. Add or update both Go and frontend specs, then run:

   ```bash
   task verify:all
   ```
8. If the tool needs a new backend API, update Go, regenerate bindings, and never edit `frontend/bindings/` by hand.

## Accessibility baseline

- Visible `:focus-visible` ring on every interactive element.
- Labelled inputs; `aria-invalid` + `aria-describedby` for field errors.
- `aria-live` for validation/status and job progress.
- `aria-pressed` on tool chips/toggles, `aria-current` on nav, `aria-expanded` on menus.
- Modals close on Escape and backdrop click; focus is moved into the modal and restored when it closes.
- Progress bars expose `aria-label` and `aria-valuetext`.
- Canvases have `role="img"`/`aria-label` and fallback text.

## Known constraints

- The Angular dev server has no Wails IPC. Pages show `Preview only` and the static registry; this is expected.
- The production bundle is over the default 500 kB warning budget because the app ships PDF.js, fonts and tool pages. The initial error budget is 1.25 MB.
- `home.css`, `image-annotate.css` and `pdf-merge.css` are slightly over the 4 kB per-component warning threshold. They are under the 8 kB error limit; move shared rules into `styles.css` before adding much more.
- Generated files in `frontend/bindings/` are read-only.
