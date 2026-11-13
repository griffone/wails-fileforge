# Tool Completeness — "No tool left poor"

- Status: Image and PDF domains complete; documents/video/shell pending
- Branch: `feat/tool-usability` (continues after the file-first overhaul)
- Started: 2026-11-13
- Related: `odd/tasks/tool-usability-overhaul.md`

## Feedback driving this phase

1. **PDF Split has no thumbnails of the resulting sections** — users split blind.
2. **Video Trim has no video preview/scrubbing** — users cut blind; it must feel like a real editor.
3. **Every tool must be complete**, comparable to dedicated alternatives, for technical and non-technical users.
4. **Several changes are incomplete** — audit and fix, not just add new chrome.

## Audit findings

- PDF Split preview was removed in the file-first rewrite; the old `pdf-split-preview`/`split-modal` components are orphaned and contain TODOs/dead client-side logic.
- Video Trim only has two time fields; no playback, playhead, in/out handles, frame strip or selection preview.
- `wails.ts` still has dead `getPdfBytes`/`splitPdfBackend` wrappers calling `Call.ByID(0)`.
- Image Crop batch plan validates crop bounds only against the first image; a smaller image can fail at execution.
- Video Trim batch applies one range to all videos without checking each duration; end beyond a shorter clip is silently truncated.
- PDF Crop preview is a static first-page thumbnail; no crop overlay.
- Image Annotate has no undo/redo.
- Image Convert lacks preserve-original, metadata control and before/after size.
- PDF Merge doesn't support per-file page ranges.
- Markdown → PDF is fixed to A4 portrait; no page size/orientation/margins.
- Video Convert has no resolution/bitrate controls; Video Merge has no clip previews.
- `ToolExecutionPanel` is now unused by pages (dead shared component).
- Global drop overlay, recent conversions and preferences from the previous plan are still pending.

## Feature matrix (table stakes from alternatives)

| Tool | We have | Missing / next |
| --- | --- | --- |
| Image Convert | formats, quality, max width, batch, presets, thumbnails | preserve original, resize by longest edge, strip/keep metadata, before/after size, format coverage |
| Image Crop | visual crop, ratios, batch, output format, preview | batch bounds validation, rotate/flip, output size |
| Image Annotate | text/arrow/rect/blur/redact, operations list, preview, batch | undo/redo, duplicate/delete keyboard, redact confirm copy |
| PDF Merge | ordered queue, auto output, batch file cards | per-file page ranges, metadata, compression |
| PDF Split | every page/ranges, batch, folders, plan count | page/section thumbnails, select pages, divider lines, preview, split by size/bookmarks |
| PDF Crop | presets, custom margins, page selection, thumbnail | interactive crop overlay, auto-detect margins |
| Markdown → PDF | header/footer, preview, file-first | page size/orientation, margins, theme, TOC |
| DOCX → PDF | single/batch, auto output, fidelity note | page ranges, PDF/A, better runtime guidance |
| Video Convert | MP4/WebM, quality presets, batch | resolution, bitrate, audio/subtitles, preview/estimate |
| Video Trim | time fields, duration, batch note | video preview, playhead, in/out handles, filmstrip, play selection, keyboard nudges |
| Video Merge | reorderable queue, presets, auto output | clip previews/thumbnails, transitions/audio policy |

## Phase 1 — requested fixes (this iteration)

1. **PDF Split thumbnails**
   - Render page thumbnails for the selected PDF (first file; file selector for batch).
   - `Every page`: each page card with its output name.
   - `Ranges`: one group card per range with page thumbnails and the output name/count.
   - Update live when strategy/ranges change; invalid ranges show a clear message.
   - Limits/lazy rendering for large PDFs; fallback message if the PDF is too large to preview.
   - Reuse `PdfRendererService` + `getPdfPreviewSource`; no new backend endpoint needed.

2. **Video Trim real editor**
   - Backend: secure local media streaming (`/media/<token>`) with Range support, registered per selected path.
   - Frontend: `<video>` preview, play/pause, seek bar, current/total time, selection bar, draggable in/out handles, "Set start"/"Set end", "Play selection", numeric inputs synced.
   - Batch: preview the first video, note that the range applies to all; warn if the end exceeds a shorter clip.
   - Keyboard: Space play/pause, I/O set range, arrows nudge 0.1s, Shift+arrows 1s.
   - Fallback to time-only mode if media registration/preview fails.

3. **Incomplete fixes**
   - Remove dead `getPdfBytes`/`splitPdfBackend` wrappers and orphan split-preview components.
   - Image Crop: validate batch bounds for every input in `Plan`; return a clear file-specific error.
   - Video Trim: batch duration warning/clamp in the UI.
   - Result/footer verbs already support `verbPast`; ensure "Trim more"/"Split more" are not mislabeled if cheap.

## Phase 2 — completeness features (next)

- Image: preserve-original + metadata strip + resize mode; crop rotate/flip; annotate undo/redo.
- PDF: split page selection/dividers + preview; crop interactive overlay; merge page ranges.
- Docs: page size/orientation/margins.
- Video: resolution/bitrate; merge clip thumbnails; convert preview/estimate.
- Shell: global drop overlay, recent conversions, preferences/onboarding.

## Acceptance criteria (Phase 1)

1. PDF Split shows result thumbnails for every page/range and updates with strategy/ranges.
2. Video Trim plays the selected video, shows a playhead and selected range, supports dragging in/out, play selection and syncs numeric inputs.
3. Media URLs are token-scoped, Range-capable and work in the Wails desktop app.
4. Image Crop batch plans fail early for any input smaller than the crop, naming the file.
5. Video Trim warns when a batch clip is shorter than the selected end.
6. Dead wrappers/orphan preview code removed.
7. Go tests, frontend specs and build pass; no regressions in the file-first flows.

## Checks

- `go test ./...`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false`
- Manual (desktop): split a 5+ page PDF, switch every page/ranges, play a video, drag handles, play selection, trim, inspect output.

## Progress

- 2026-11-13: Phase 1 complete.
  - **PDF Split thumbnails**: new `app-pdf-split-preview` renders every page (every_page) or grouped range cards with all page thumbnails, output names and counts; file selector for batch; 60-page initial limit + Show all; progressive rendering; fallback for oversized/unreadable PDFs. Old orphan `pdf-split-preview`/`split-modal` removed.
  - **Video Trim editor**: secure `/media/<token>` streaming with Range support; video stage, play/pause, playhead, selected range, draggable in/out handles, Set start/end, Play selection, track seek, keyboard (Space/I/O/arrows, Shift nudge), synced time inputs, batch shorter-clip warning. Fallback to time-only mode if preview is unavailable.
  - **Incomplete fixes**: dead `wails.getPdfBytes`/`splitPdfBackend` wrappers removed; unused `tool-execution-panel` removed; Image Crop batch plans now validate bounds against every readable input and name the offending file (corrupt files still fail per item); Video Trim warns when a batch clip is shorter than the selected end.
  - **Verification**: Go build/tests pass (including media registry and crop batch tests); frontend build passes; 235/235 Karma specs. Visual checks via `npx agent-browser` (PDF Split page/range thumbnails, Video Trim editor populated/fallback, dark/light, 1440/1000). Real video playback/Range streaming must be verified in `task dev`; the Angular dev server has no `/media` endpoint and intentionally shows the fallback.
- 2026-11-13: Image domain complete.
  - **Image Convert**: preserve-original (`format:auto`), AVIF/TIFF output, longest-edge resize, strip metadata, rotate/flip, before/after size summary, advanced format select, Original preset.
  - **Image Crop**: output width/height, rotate/flip after crop, batch bound validation, advanced transforms and reset; preview hint for transforms.
  - **Image Annotate**: bounded undo/redo history, duplicate/delete actions, Ctrl/Cmd+Z, Ctrl/Cmd+Shift+Z / Ctrl+Y, Ctrl/Cmd+D, Delete/Backspace; form edits commit on apply/blur.
  - **Verification**: Go tests pass (image package 63 tests), frontend 251/251 specs, build passes; advanced states visually checked with `npx agent-browser`.
  - **Next**: PDF domain — selectable split pages/dividers, live crop overlay, per-file merge page ranges.
- 2026-11-13: PDF domain complete.
  - **PDF Split**: third `Selected pages` strategy with clickable page thumbnails, Select all/Clear, consecutive-page grouping into ranges, live part cards, plan uses ranges under the hood.
  - **PDF Crop**: first-page dimensions from inspect, aspect-correct preview frame, live kept-region overlay with dimmed outside, preset/custom margins caption.
  - **PDF Merge**: per-file `Page ranges` in Advanced, one input per file in queue order, backend merge-ranges support (collect selected pages then merge), plan errors block the action.
  - **Verification**: Go tests pass (merge ranges/inspect dims), frontend 270/270 specs, build passes; visual checks via `npx agent-browser` for selected pages, crop overlay and merge ranges. Advanced split-by-size/bookmarks and PDF compression remain optional later.
