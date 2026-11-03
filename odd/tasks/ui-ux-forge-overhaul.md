# FileForge UI/UX Overhaul — "Forge Workbench"

- Status: in progress
- Created: 2026-11-03
- Feature branch: `feat/ui-ux-forge-overhaul`
- Delivery strategy: `ask-on-risk` (no push/PR without explicit user request)
- Tests: no strict TDD configuration found; functional checks = Angular build + Karma specs

## Objective

Turn FileForge Desktop from a set of unstyled utility screens into one coherent, premium, local-first file workbench: a persistent navigation shell, a command palette, a searchable tool launcher, a real design system with light/dark themes, and consistent page composition across all 11 tools.

## Problem

- Every page is its own island: two page archetypes, four max-widths, mixed headers, and a bare unstyled app header above each page.
- A 405-instance hardcoded color surface (gray/blue/pastel/raw hex) with no theme or semantic tokens.
- The unused `lucide-angular` dependency means all iconography is text glyphs.
- 11 tools are only discoverable by scrolling a pastel card grid; no global search, no recent tools, no persistent nav.
- Inconsistent input selection (only 2 of 13 pages use `FileDrop`), duplicated progress UI, mixed Spanish/English copy, missing focus states and labels.
- App identity is missing: default Angular title, "Wails Frontend" heading, no brand.

## Design direction

**Precision workbench / forged metal.** Industrial, quiet, and confident — not a generic SaaS dashboard. Obsidian and warm graphite surfaces in dark mode, warm paper in light mode, a molten amber accent used sparingly, fine grid and ember glow for atmosphere, Archivo for UI and display, Instrument Serif italic for one editorial accent, IBM Plex Mono for metadata and shortcuts.

- Signature: an anvil brand mark, a fixed left rail grouped by domain, and a `⌘K` command palette.
- Motion: restrained; one orchestrated hero reveal, hover lift on tool cards, 150–220 ms transitions.
- Accessibility: visible focus rings, AA contrast, semantic labels, keyboard paths for every shell action.

## Scope

In scope:
- Global design tokens, fonts, base styles, light/dark themes with persistence.
- App shell: sidebar navigation, top bar, responsive rail, brand.
- Shared services: tool registry, theme, recent tools.
- Command palette with keyboard navigation.
- Home redesign as a searchable command center.
- Shared components restyle: FileDrop, JobCard, ToolExecutionPanel, ToolCatalog.
- Flagship pages: Image Converter, PDF Merge.
- Consistent header/panel/color sweep across the remaining tool pages.
- Targeted UX/accessibility fixes: duplicate progress removal, missing labels, live regions, focus states.
- Specs for new services and palette behavior; keep existing specs green.

Out of scope:
- Backend changes, IPC contracts, generated bindings.
- New conversion capabilities.
- Rewriting crop/annotate/canvas interaction logic.

## Acceptance criteria

1. All 12 routes (11 tools + system) are reachable from the sidebar and the command palette.
2. `Ctrl/Cmd+K` opens the palette; arrows/Enter/Escape work; results filter by name, category, and keywords.
3. Theme toggle persists; first load follows the OS; both themes meet AA on text and controls.
4. Home supports text search, category filters, recent tools, and empty states.
5. No pastel rainbow, no unstyled default header, no "Wails Frontend" / "Frontend" titles.
6. No page-level back links or duplicated navigation; page headers share one composition.
7. Video convert/trim no longer duplicate JobCard progress; doc pages expose their messages consistently.
8. `npm run build:dev` passes; new and existing Karma specs pass.
9. Generated bindings untouched.

## Checks

- `cd frontend && npm run build:dev`
- `cd frontend && npx ng test --watch=false`
- Manual route sweep: every sidebar item renders its page.
- Keyboard sweep: Tab order in shell, palette arrows/Enter/Escape, FileDrop Space/Enter.

## Tasks

- [x] T01 Foundation — fonts, tokens, themes, base styles, semantic remap of Tailwind gray/blue/amber/red/slate scales.
- [x] T02 Services — tool registry, theme service, recent-tools service.
- [ ] T03 App shell — sidebar, top bar, responsive rail, brand, route context.
- [ ] T04 Command palette — `⌘K`, filtering, grouped results, keyboard navigation.
- [ ] T05 Home — searchable command center, category filters, tool cards, recent tools, local-first strip.
- [ ] T06 Shared components — FileDrop, JobCard, ToolExecutionPanel restyle and a11y.
- [ ] T07 System page — Tool Catalog restyle and status presentation.
- [ ] T08 Flagship pages — Image Converter and PDF Merge composition.
- [ ] T09 Remaining pages — crop, annotate, split, PDF crop, docs, video pages.
- [ ] T10 A11y + QA — labels, live regions, focus, keyboard; build and tests.
- [ ] T11 Docs + close — update repo docs, verify final state, report.

## Routing and delegation log

- Exploration: 1 narrow `explore` mapping subagent over all components (trigger: 4+ files).
- Writer tasks: one bounded writer at a time; no parallel writers in one worktree.
- Core design decisions and token contract are orchestrator-owned; repetitive page sweeps are delegated with this document.

## Progress log

- 2026-11-03: Baseline `npm run build:dev` passes (983 kB initial, existing budget warning). Repo clean on `main`.
- 2026-11-03: UI map completed; 13 page components, 4 shared components, no theme, 405 hardcoded color utilities, `lucide-angular` unused.
- 2026-11-03: Design direction locked; tracking started.
- 2026-11-03: T01–T02 complete. Bundled Archivo Variable + Instrument Serif + IBM Plex Mono; 1,275-line token/component system; dark/light themes with pre-boot script; semantic Tailwind remap verified in the compiled bundle; tool registry (11 tools + Home + System), theme service and recent-tools service. Targeted specs: 19/19. Build passes; angular initial budget raised to 1.25 MB to fit bundled fonts (baseline 983 kB, now 1.00 MB).
