# Tool Usability Overhaul — File-first Workbench

- Status: all 11 tools migrated to the file-first flow (global intake/history/preferences pending)
- Branch: `feat/tool-usability`
- Started: 2026-11-13
- Related: `odd/tasks/ui-ux-forge-overhaul.md` (visual system, already merged)

## Objective

Replace the backend-contract-shaped tool forms with a guided, file-first workflow that works for non-technical users without hiding power from technical users:

> **Drop files → see what will happen → choose a simple outcome → run → open the result.**

This is not a visual pass. It requires backend primitives for output planning, file inspection, native result actions, and plain-language errors.

## Evidence from the current implementation

- 11 tools expose 11 different form shapes: mode selectors, free-text output paths, raw option keys, “Validate” then “Run”.
- Only 2 tools use `FileDrop`; the rest use hidden inputs or path fields.
- Output semantics are inconsistent:
  - image convert can silently overwrite an explicit output path;
  - PDF merge/split/crop and video fail when the output exists;
  - some tools auto-suffix (`-2`), others refuse;
  - `req.OutputDir` vs `options.outputDir` is tool-specific.
- PDF merge requires a full output path; video convert/trim/merge require full paths; PDF split/crop require `options.outputDir`.
- Image convert batch requires `req.outputDir` even though the natural default is “next to each input”.
- Results expose no open/reveal action, no file size, and no “do another”.
- Errors expose canonical codes and raw OS/ffmpeg text; recovery actions are missing.
- Job recovery is dead because `ToolingService.SetContext` is never called (`ServiceStartup` is not implemented).
- Manifests only list input/output extensions; defaults, presets and option constraints live in duplicated Go/Angular code.
- No plan endpoint: the UI cannot show the exact destination before running.
- No file inspection/thumbnail endpoint for a friendly file card (image/PDF previews exist; video does not).

## Research principles

Best-in-class local converters (Permute, ImageOptim, Squoosh, HandBrake presets) and UX guidance converge on:

1. **File-first intake.** A large drop target; files are the first object, not a mode dropdown.
2. **One primary action.** “Convert”, “Merge”, “Split” — not “Validate” then “Run”.
3. **Smart defaults + visible destination.** Save next to the original, never overwrite, show the exact planned path.
4. **Compatible choices only.** Show outputs that work for the selected files; no invalid combinations.
5. **Presets first, advanced later.** Simple outcome presets; exact settings behind “Advanced”.
6. **Actionable results.** Open file, show in folder, convert another; per-file status for batches.
7. **Plain-language errors with a recovery action.** File name first, what happened, what to do; technical details in a disclosure.
8. **Progressive disclosure and inline validation.** No modals for routine errors; no validation summaries as the only signal.
9. **Batch is a consequence of dropping multiple files**, not a mode the user must choose.

Sources:
- NN/g — error messages in forms, progressive disclosure, smart defaults.
- Permute — drop a queue, presets, per-file start, trim/crop editors.
- ImageOptim — drop files and wait; one job, one result.
- Squoosh — file first, simple/advanced split.
- HandBrake — presets for normal users, detailed controls for experts.
- FileFy/FormatFusion — compatible outputs, auto save location, one direct result, visible stages.

## UX model

### 1. Intake

- Drop zone is the first and largest element.
- Dropping 1 file = single; 2+ = batch automatically. Remove the mode selector from the main flow.
- File cards show thumbnail (image/PDF/video frame), name, type, size, dimensions/duration, remove.
- Native “Choose files” is secondary; drag/drop is primary.
- Unsupported files are rejected inline before running, with a clear reason.

### 2. Outcome

- Presets, not raw option keys:
  - Image: `WebP · smaller` (default), `JPEG · photos`, `PNG · lossless`, `GIF`.
  - Video: `MP4 · compatible` (default), `WebM · web`, quality `Balanced`/`High`/`Small`.
  - PDF merge: automatic `<first>_merged.pdf`; optional custom name.
  - PDF split: every page vs ranges; output folder `<name>_pages` by default.
  - Docs: PDF next to the source with a readable suffix.
- “Save to” shows `Next to the original` by default, with a `Change` action.
- The exact output path is shown before running: `photo.webp`, `photo-2.webp` if needed.
- Advanced disclosure contains technical controls: quality, resize, exact ranges, margins, encoder mode, custom output name.

### 3. Run

- One primary button with a count: `Convert 3 images`, `Merge 2 PDFs`, `Split PDF`.
- No separate Validate step. Validation happens inline and on submit; the backend plan is the source of truth.
- Progress uses stage labels and a single progress bar; no raw job IDs in the main view.
- Cancel is available while running.

### 4. Result

- Success: `3 images converted`, total size/time, output list with `Open` and `Show in folder`, `Convert more`.
- Partial: `2 of 3 converted`, failed rows with plain reason and `Retry failed`.
- Failure: plain title + explanation + action (`Choose another folder`, `Install LibreOffice`, `Retry`).
- Technical details (job id, error code, exact paths) live in a `Details` disclosure.

### 5. Persistent preferences

- Default output location: next to original (default) or a chosen folder.
- Never overwrite by default; automatic `-2`, `-3` suffix.
- Last used preset per tool.
- Theme already persisted.

## Output policy (single source of truth)

Default, unless the user changes it:

- Save next to the input file.
- Never overwrite. Auto-suffix `-2`, `-3`, … and show the resolved name.
- Batch with multiple inputs: next to each input by default; if the user chooses a folder, all outputs go there.
- Explicit custom output name that already exists: show an inline conflict with “Use another name” / “Save as <suffixed>”.
- Do not create directories silently unless the user chose a folder that does not exist; then show “Folder will be created”.
- Output names:
  - image convert: `<stem>.<format>`; if it equals the input, `<stem>-converted.<format>`
  - image crop: `<stem>_cropped.<ext>`
  - image annotate: `<stem>_annotated.<ext>`
  - PDF merge: `<firstStem>_merged.pdf`
  - PDF split: `<stem>_pages/` directory
  - PDF crop: `<stem>_cropped.pdf`
  - Markdown: `<stem>_md2pdf.pdf`
  - DOCX: `<stem>_docx2pdf.pdf`
  - video convert: `<stem>_converted.<fmt>`
  - video trim: `<stem>_trimmed.<fmt>`
  - video merge: `<firstStem>_merged.<fmt>`

## Backend changes

### Phase 1 — primitives + Image Convert reference

1. **Planner interface on tools**
   - Add optional `Planner` interface (`Plan(ctx, req) (PlanJobResponseV1, *JobErrorV1)`) next to `SingleExecutor`/`BatchExecutor`.
   - `Orchestrator.Plan` resolves the tool and delegates; `ToolingService.PlanJobV1` and `App.PlanJobV1` expose it.
   - Response: planned outputs (input, output, kind, exists, adjusted, warnings), effective options, warnings, error.
2. **Shared output resolver**
   - New package `internal/outputplan` for collision-safe paths and naming rules.
   - Implement `Plan` + execution integration for `tool.image.convert` first.
   - Remove the batch `outputDir` requirement: default to each input's directory.
   - Explicit `options.outputPath` that exists is a conflict; auto-derived names suffix.
   - Add `Outputs`/`OutputCount` to image convert results.
3. **File inspection**
   - `InspectFilesV1(paths)` → name, directory, extension, size, modified time, MIME, kind (image/pdf/video/doc/other), optional image dimensions, PDF page count later.
   - Used for file cards and result sizes.
4. **Native actions**
   - `OpenPath(path)` and `RevealInFolder(path)` using Wails `Browser.OpenFile` / `Env.OpenFileManager`.
   - Validate existence; return a result struct, no raw errors.
5. **Lifecycle bug**
   - Implement `ServiceStartup` on `App` to call `SetContext`, enabling job persistence/recovery.
6. **Bindings**
   - Regenerate with `wails3 generate bindings -clean=true`.
   - Update `frontend/src/app/services/wails.ts` IDs for new methods; do not edit generated files.
7. **Tests**
   - Go: planner naming/collision, image convert plan + auto execution, inspect, merge plan conflict.
   - Frontend: plan request, preset → request mapping, one-action run, result actions, error catalog.

### Phase 2 — shared frontend flow + roll out to PDF

- Generic `FileQueue`, `OutputPlanCard`, `ResultCard`, `ToolFlow` components.
- Migrate PDF Merge, PDF Split, PDF Crop, Markdown → PDF, DOCX → PDF.
- Remove Validate from the main UI; keep “Check settings” only in advanced/details if needed.
- Add real result panels with open/reveal/retry.
- Plain error catalog.

### Phase 3 — media/editor tools

- Video Convert/Trim/Merge: presets, trim editor with duration, output plan, result actions.
- Image Crop/Annotate: visual editors first; coordinates/margins only in advanced; add “Apply to all” for batch.
- Video thumbnails via ffmpeg.

### Phase 4 — global intake + history + preferences

- Global drop overlay in the shell: drop files anywhere → recommend tools by type.
- `Recent conversions` on Home with open/reveal.
- Preferences: default output location, overwrite policy, concurrency.
- First-run guidance (3 steps, dismissible).

## Phase 1 acceptance criteria

1. Image Convert is fully file-first: drop/choose files, thumbnails, automatic single/batch, no mode or path typing.
2. Presets: WebP (default), JPEG, PNG, GIF; advanced quality/resize.
3. Destination: next to originals by default; “Change” picks a folder; exact planned output shown before running.
4. One primary action (`Convert N images`); no separate Validate.
5. Results: success/partial/failed, output size, Open, Show in folder, Convert more, retry failed.
6. Never overwrite; auto-suffix, visibly reflected in the plan.
7. Plain-language error for wrong type, missing input, unwritable folder, unsupported format.
8. Backend: `PlanJobV1`, `InspectFilesV1`, `OpenPath`, `RevealInFolder`, `ServiceStartup`; bindings regenerated.
9. Go tests and frontend specs pass; build passes.
10. No generated binding edits by hand.

## Checks

- `go test ./...`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false`
- Manual: drop 1 image, drop 3 images, choose folder, run, open result, reveal result, collision suffix, unsupported file, partial failure.
- Wails dev smoke test for native open/reveal.

## Progress

- 2026-11-13: Deep audit of all 11 tool contracts, preview, dialogs and job lifecycle. Research synthesis. Phase 1 scope locked. No code changes yet.
- 2026-11-13: Phase 1 backend complete. Added `Planner` contract, `PlanJobV1`, `InspectFilesV1`, `OpenPath`, `RevealInFolder`, `ServiceStartup`/`ServiceShutdown`, `internal/outputplan`, `internal/inspect`; image convert now plans collision-safe outputs and uses the plan for execution; PDF merge plans auto `<first>_merged.pdf` while preserving explicit-output conflict behavior; bindings regenerated. Go build/tests pass.
- 2026-11-13: Enabled native Wails file drop (`EnableFileDrop`) so OS drops deliver real file paths; FileDrop now exposes `data-file-drop-target`, emits `pathsSelected`, supports a native picker mode, and accepts a custom headline.
- 2026-11-13: Phase 1 reference flow complete. Image Convert is now file-first: native drop/choose, thumbnails and metadata via `InspectFilesV1`/preview thumbnails, automatic single/batch, WebP/JPEG/PNG/GIF presets, advanced quality/max width, exact output plan, one primary action (no Validate), collision-safe auto naming, terminal result card with Open/Show in folder/Convert more/Retry failed, and plain-language errors.
- 2026-11-13: PDF Merge reference flow complete. Reorderable PDF queue (up/down), exact auto `<first>_merged.pdf` plan or advanced custom name, folder selection, one-action merge, no confirmation modal, result card with Open/Reveal/Retry. The backend plan now respects a chosen `OutputDir`.
- 2026-11-13: Verification for the two reference flows: Go build/tests pass; frontend build passes; 201/201 Karma specs pass. Visual QA at 1440/1000 dark/light. Next: roll the same flow to PDF Split/Crop, docs and video, then global intake/history/preferences.
- 2026-11-13: Document flows complete. Markdown → PDF: single-file intake, output plan, header/footer advanced section, one action, result card. DOCX → PDF: automatic single/batch, output plan, quiet fidelity note instead of a blocking confirm, retry failed, result card. Backend plans added for both; DOCX batch no longer requires an output directory and plans next to each input. Go tests pass; frontend 203/203 specs; build passes.
- 2026-11-13: Video flows complete. Convert/Trim/Merge are file-first with preset cards, output plans, one action, progress and result cards. Video duration is inspected via ffprobe; trim accepts `SS`/`MM:SS`/`HH:MM:SS`, validates the range and applies it to batch. Backend plans added for convert/trim/merge with collision-safe auto output and optional output folders. Go tests pass; frontend 218/218 specs; build passes.
- 2026-11-13: PDF Split/Crop flows complete. Split: PDF queue, Every page/Page ranges, collision-safe `<stem>_pages` directories, one action, grouped result folders. Crop: preset cards (Small/Medium/Large/None/Custom), all/custom page selection, custom margins in Advanced, auto `<stem>_cropped.pdf`, one action, result. Backend plans added; old modal/preview-canvas/mode/path/Validate chrome removed. Go tests pass; frontend 217/217 specs; build passes.
- 2026-11-13: Image Crop/Annotate flows complete. Crop keeps the interactive selection/preview but now has file-first intake, ratio cards, bounds in Advanced, output plan and one action. Annotate keeps the SVG editor/tool palette/operation list but now has file-first intake, output plan and one action; the redact confirm was removed. Backend plans added for both. Go tests pass; frontend 231/231 specs; build passes. All 11 tools now use the file-first flow.
