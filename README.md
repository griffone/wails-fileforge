# FileForge Desktop

A local-first file workbench for Windows, macOS and Linux. FileForge wraps twelve conversion and editing tools in a single desktop app: image convert/crop/annotate, PDF merge/split/crop/optimize, Markdown and DOCX to PDF, and video convert/trim/merge.

Everything runs on your machine. There are no uploads, accounts, or cloud calls.

## Tools

| Domain | Tools |
| --- | --- |
| Image | Convert (PNG/JPG/WebP/GIF), Crop, Annotate (text, arrow, box, blur, redact) |
| PDF | Merge (explicit order), Split (pages, ranges, page count or bookmarks), Crop (presets or margins), Optimize (compression + metadata) |
| Documents | Markdown → PDF (optional header/footer), DOCX → PDF (LibreOffice + Pandoc fallback) |
| Video | Convert, Trim, Merge |

## Stack

- **Backend:** Go 1.25 + Wails v3 (alpha)
- **Frontend:** Angular 21, standalone components, TypeScript strict
- **Styling:** Tailwind CSS v4 + a token-based design system (`frontend/src/styles.css`)
- **Icons:** `lucide-angular`
- **Fonts:** Archivo Variable, Instrument Serif, IBM Plex Mono (bundled via `@fontsource`, no CDN)
- **Tests:** Karma + Jasmine

## Quick start

```bash
# Install frontend dependencies
cd frontend && npm install

# Run the desktop app in dev mode (frontend + Wails)
task dev

# Production build
task build
```

`task dev` delegates to the Wails binary configured in `Taskfile.yml`. If your binary lives elsewhere, update the task or run:

```bash
/home/ivan/go/bin/wails3 dev -config ./build/config.yml -port 9245
```

### Frontend only

```bash
cd frontend
npm run dev        # Angular dev server on :4200
npm run build:dev  # development build
npm run build      # production build
```

The Angular dev server is useful for UI work, but Wails IPC is unavailable there. Pages fall back to the static tool registry and show a `Preview only` runtime state.

## Tests

```bash
cd frontend
npx ng test --watch=false
```

Karma uses the Chromium bundled by Puppeteer. If that binary is missing, point `CHROME_BIN` at any Chromium-based browser:

```bash
CHROME_BIN=/usr/bin/brave npx ng test --watch=false
```

Run one spec:

```bash
npx ng test --watch=false --include src/app/components/home/home.spec.ts
```

Go tests:

```bash
go test ./...
```

## UI architecture

The app is a single workbench shell with a persistent navigation rail, a command palette, and tool pages built from one composition recipe.

- **Design system:** `frontend/src/styles.css` — semantic tokens for dark/light themes, typography, surfaces, buttons, fields, banners, badges and page composition. Existing Tailwind gray/blue/amber/red/slate utilities are remapped to these tokens, so every screen follows the active theme.
- **Shell:** `frontend/src/app/app.ts|html` — brand rail grouped by domain, top bar with breadcrumb, `Ctrl/Cmd+K` palette trigger, theme toggle and runtime status.
- **Command palette:** `frontend/src/app/components/command-palette/` — recent tools, grouped results, full keyboard navigation.
- **Tool registry:** `frontend/src/app/config/tools.ts` + `services/tool-registry.service.ts` — the single source of truth for routes, names, descriptions, icons, formats and keywords.
- **Theme:** `frontend/src/app/services/theme.service.ts` — persists `ff-theme`, follows the OS on first load.
- **Recent tools:** `frontend/src/app/services/recent-tools.service.ts` — last four destinations, persisted locally.
- **Shared components:** `components/file-drop`, `components/file-queue`, `components/output-plan-card`, `components/result-card`, `components/job-card`.
- **File-first tool flow:** drop/choose files → file cards → preset or advanced outcome → `OutputPlanCard` with the exact destination → one primary action → `ResultCard` with open/reveal/retry. Backend `PlanJobV1` is the source of truth; there is no separate Validate step and no free-text output path.
- **Page recipe:** `.page` → `.page-head` (eyebrow, title, subtitle) → `.panel` sections → `.field`/`.input` forms → `.banner` messages → `.btn` actions.

See [`docs/ui-ux.md`](docs/ui-ux.md) for the token contract, page conventions and a checklist for adding a tool.

## Project structure

```
main.go                          Wails bootstrap and asset embedding
internal/                        Go services, converters, jobs, models
frontend/src/app/
  app.ts|html|css                Workbench shell
  config/tools.ts                Tool registry data
  services/                      Wails IPC, theme, recent tools, registry
  components/                    Shell palette + tool pages + shared UI
  styles.css                     Design system and global styles
frontend/bindings/               Generated Wails bindings (do not edit)
docs/                            Product and UI documentation
odd/tasks/                       Organic development task tracking
```

## Generated bindings

`frontend/bindings/` is generated. Never edit it by hand. After changing a Go-exposed API:

```bash
/home/ivan/go/bin/wails3 generate bindings -clean=true
```

## License

See the repository for licensing information.
