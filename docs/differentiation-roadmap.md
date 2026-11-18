# FileForge Differentiation Roadmap

- Status: proposed
- Created: 2026-11-16
- Scope: five product differentiators on top of the current 12-tool file-first workbench
- Goal: stop competing as "another converter" and become the local workbench for **outcome recipes, verifiable outputs, and local automation**

## Positioning

Most alternatives are one of:

- single-purpose and polished (Squoosh, ImageOptim, Permute),
- broad but technical (XnConvert, PDF24, HandBrake),
- cloud web tools (Smallpdf, CloudConvert).

The free space is a **local-first desktop workbench that composes tools into outcomes, verifies the result, and can run automatically**. FileForge already has the right foundations: tool registry, plan-first jobs, batch orchestration, output safety, previews, global drop, recents and settings.

## Principles

1. **Outcome over tool.** Users pick "Send by email", not "PDF Optimize + Merge".
2. **Local and private by default.** No uploads, accounts or cloud processing.
3. **Verifiable.** A green "done" is not enough; show page counts, dimensions, codecs, sizes and checksums where useful.
4. **Non-destructive.** Never overwrite; undo moves outputs to an app trash, never deletes user files.
5. **Progressive depth.** Curated recipes first; a general recipe builder later.
6. **Optional intelligence.** OCR/model features are opt-in packs with explicit dependency detection, never bundled models without consent.

---

## 1. Outcome Recipes / Publish Sets

### User promise

Drop files, choose a result, get a verified bundle: one step, multiple tools, multiple outputs.

### Built-in recipes (MVP)

| Recipe | Input | Steps | Final outputs |
| --- | --- | --- | --- |
| **Email PDF** | 1+ PDFs | Merge when >1 → optimize to target size | One small PDF |
| **Web image set** | 1 image | Normalize → resize to 1600/800 → WebP + JPEG → strip metadata | 4 images + ZIP + HTML `srcset` snippet |
| **Archive document** | 1 DOCX | DOCX → PDF/A → metadata | Archival PDF + checksum |
| **Social clip** | 1 video | Trim (optional) → MP4 + WebM → poster frame | 2 videos + poster |
| **Clean scan** | 1 PDF | OCR → optimize → verify page count | Searchable PDF |

"Clean scan" belongs to the Local Intelligence phase; it is listed here as the eventual recipe.

### Architecture

- New `internal/recipes` package with a curated `RecipeV1` model:
  - `ID`, `Name`, `Description`, `Category`, `InputKind`, `Steps`, `Outputs`.
  - A step references a tool ID and a small option template; outputs from a step can feed one or more later steps.
  - Phase 1 recipes are **built-in Go definitions**, not user-editable JSON.
- New `RecipeRunner`:
  - Creates a temp workspace for intermediates.
  - Submits each step as a normal job to the existing `jobs.Orchestrator` (reuses validation, retries, progress, cancellation, persistence).
  - Passes planned outputs of step N as inputs to step N+1.
  - Supports linear chains and final fan-out (one source → multiple output variants).
  - Emits `recipes/progress/v1` with `stepIndex`, `stepCount`, `toolName`, `stage`, `percent`, `eta`.
  - On failure: stop by default, clean intermediates, keep no partial final outputs unless the user chose "keep successful outputs".
  - On success: move/copy final outputs to the chosen destination with collision-safe naming; verify them; record job history.
- New endpoints on `App`:
  - `ListRecipesV1()`
  - `PlanRecipeV1(req)` → recipe metadata, input validation, final destination, warnings (no intermediate paths)
  - `RunRecipeV1(req)` → recipe job ID
  - `GetRecipeStatusV1(id)` → per-step progress + final JobResult-like output
  - `CancelRecipeV1(id)`
- New event: `recipes/progress/v1`.
- Recipe jobs persist alongside normal jobs so an interrupted recipe can be reported; automatic resume is out of scope.

### UI

- Home gets a **Recipes** section above the tool grid:
  - Cards with outcome name, one-line description, accepted types, and a "Use recipe" action.
- Global drop overlay recommends recipes first when the file type matches (e.g. PDF → Email PDF, Clean scan; image → Web image set).
- Recipe run view:
  - file cards, destination plan, recipe steps as a compact stepper;
  - one primary action with the recipe name;
  - per-step progress and cancel;
  - final result card with verified outputs and undo.
- Command palette: "Run recipe: Email PDF".
- Phase 2: a recipe detail/builder page for choosing options and duplicating a built-in recipe. A full drag-and-drop builder is not required.

### Acceptance

1. At least 3 built-in recipes run end-to-end from Home and global drop.
2. Each step reports progress; cancel stops the chain; temp files are cleaned.
3. Final outputs are collision-safe, verified and undoable.
4. Fan-out recipes (Web image set) produce multiple outputs and a ZIP/HTML snippet.
5. Go and frontend tests cover the runner, failure cleanup and fan-out.

### Risks

- Mapping tool options/outputs across steps; start with curated recipes to avoid a generic schema.
- Partial failures and intermediate cleanup.
- Long chains need clear progress and a cancel path.
- Do not build a general visual builder in Phase 1.

---

## 2. Target size ("under X MB")

### User promise

"Make this fit in 10 MB" without understanding quality, bitrate or DPI.

### Architecture

- Shared option `targetSizeBytes int64` on relevant job requests.
- Per-media strategies (image target-size compression shipped as
  `tool.image.compress`; video target size is still pending):
  - **Images** (shipped as `tool.image.compress`): quality search plus bounded
    downscale; PNG has no quality knob, so it downscales and reports "best
    effort"; the chosen quality, dimensions and actual size are reported.
  - **PDF** (`pdf.optimize`, optionally merge):
    * try pdfcpu optimize;
    * if still over target, try Ghostscript `-dPDFSETTINGS=/ebook`, then `/screen`; stop at the first result under target;
    * warn that aggressive PDF compression can downsample images and affect fidelity;
    * if Ghostscript is missing, show a clear runtime dependency message;
    * do not rasterize pages by default; if added later, require explicit consent and verify searchability.
  - **Video** (`video.convert`, `video.trim`, `video.merge`, pending):
    * compute total bitrate from target bytes and duration, subtract audio bitrate (AAC 128k / Opus 96k);
    * use two-pass encoding for x264/VP9;
    * retry up to 2 times at 90% bitrate if the output is still over target;
    * warn if the computed video bitrate is below a quality floor.
- Plan shows `Target ≤ X MB` and "best effort"; a plan warning explains which strategies will be tried.
- Result verification compares actual bytes with the target and shows a pass/warn state.

### UI

- Advanced `Target size` toggle + MB number input in:
  - Image Convert
  - PDF Optimize
  - Video Convert / Trim / Merge
- In the plan card: target chip and strategy note.
- In the result card: `8.2 MB / target 10 MB ✓` or `11.4 MB — best effort couldn't reach 10 MB` with an action "Try stronger compression" (re-run with a lower target/strategy).

### Acceptance

1. Images and videos reach the target within a small tolerance; the result reports the actual size.
2. PDF uses pdfcpu/Ghostscript and reports when the target is not reachable.
3. No infinite loops; bounded iterations; clear warnings.
4. Go and frontend tests cover success, best-effort failure and missing Ghostscript.

### Risks

- PDF target size is not always achievable without rasterization; be explicit.
- Video two-pass doubles encode time; show a forecast.
- Very small targets can produce unusable quality; enforce floors and warn.

---

## 3. Verified Outputs + Undo

### User promise

"Show me it worked, and let me take it back if I don't like it."

### Verification

- New `internal/verify` service and `VerifyJobV1(jobId)` endpoint.
- For every output:
  - exists, regular file, readable, size > 0;
  - extension/MIME matches the expected tool output;
  - image dimensions; PDF page count and page dimensions; video duration/codec/resolution/audio;
  - target size met, when requested;
  - optional SHA-256 for high-assurance recipes (or for undo fingerprints).
- Response: `VerificationReportV1{ success, checks[], outputs[], warnings[] }`.
- Frontend ResultCard fetches the verification after terminal status and shows:
  - `Verified` badge with 3–5 checks;
  - before/after size and percentage;
  - warnings when a check fails.
- Recent conversions store a compact verification summary.

### Undo

- New `internal/jobhistory` service:
  - records terminal jobs with `jobId`, `toolId`, outputs `{path,size,mtime}`, timestamp and `undone` state;
  - persisted in `os.UserConfigDir()/fileforge/job_history_v1.json`, capped (e.g. 50 jobs / 500 files);
  - `UndoJobV1(jobId)`:
    * verify each output still matches the recorded size/mtime (skip modified files);
    * move untouched outputs to `os.UserConfigDir()/fileforge/trash/<jobId>/`;
    * remove empty directories left by split outputs;
    * mark the job undone and report per-file results;
  - `RestoreJobV1(jobId)` moves files back when the destination is still free;
  - `EmptyTrashV1()` is explicit and confirmed.
- Hook recording into the orchestrator completion path (new `onJobComplete` callback or a `jobs/completed/v1` event). Do not rely on the frontend to record.
- UI:
  - `Undo` in the ResultCard and Recent conversions;
  - `Restore` for the last undone job; an "Undo history" section in Settings.
- Safety:
  - never delete files; app trash only;
  - skip modified/missing outputs and report them;
  - never touch paths that existed before the job (the app never overwrites).

### Acceptance

1. Every terminal job can be verified; the report matches the files.
2. Undo moves only untouched outputs; modified files are skipped with a clear message.
3. Restore returns outputs; trash can be emptied explicitly.
4. Job history survives restart.
5. No user file is deleted by the app.

### Risks

- Fingerprints must avoid deleting modified files.
- Directory outputs (PDF split) need per-file recording.
- Large job histories need caps/cleanup.

---

## 4. Local Automation

### User promise

"Put a file in this folder and it comes out processed, without opening the app."

### Architecture

- Add `github.com/fsnotify/fsnotify`.
- New `internal/automation` service:
  - `RuleV1{ ID, Name, Enabled, WatchDir, Recursive, Extensions, RecipeID, OutputDir, Options, DebounceSeconds, DeleteSource, CreatedAt }`.
  - Store in `os.UserConfigDir()/fileforge/automation_rules_v1.json`.
  - One watcher per rule (or a shared watcher with a rule map).
  - File stability: wait until size is unchanged for ~1s; debounce duplicate create/write events.
  - Ignore hidden/temp files, the output directory, and `.fileforge` workspace dirs; prevent loops.
  - Limits: max file size, max concurrent runs per rule, no recursive by default, ignore patterns.
  - Run the matching recipe with the file; write final outputs to `OutputDir`; emit `automation/run/v1` events with rule/file/status/outputs.
  - Persist last run/status per rule; start enabled rules on app startup and stop them on shutdown.
- Phase 2b (optional): CLI mode (`fileforge --recipe ... --input ...`) reusing the same recipe runner; OS context-menu integration is platform-specific and later.

### UI

- New `/automations` page (System group):
  - rule list with enabled toggle, watch folder, recipe, output folder, last run/status;
  - create/edit form: folder pickers, recipe select, extensions, delete-source warning (default off);
  - empty state and a short "How it works" explainer.
- Home status card: `2 rules active · last run 2 min ago`.
- Settings: start at login? (platform-specific; later), max concurrent runs, trash/empty.

### Acceptance

1. A file dropped into a watched folder runs the recipe exactly once and produces outputs.
2. Duplicate events and partial copies do not double-run.
3. Disabling a rule stops processing; deleting a rule cleans up watchers.
4. Loops are prevented when the output folder is inside the watch folder.
5. Missing recipe/folder/dependency shows a clear disabled/degraded state.
6. Rules persist across restart.

### Risks

- fsnotify semantics differ across OSes; test Linux/macOS/Windows.
- File stability and antivirus/cloud-sync delays.
- Resource limits for large folders.
- Do not delete source files by default.

---

## 5. Local Intelligence (optional pack)

### User promise

"Understand my files locally: OCR, subtitles, smart crop/rename. Nothing leaves the machine."

### OCR tool (`tool.pdf.ocr`)

- Inputs: 1+ PDFs; options language (`eng`, `spa`, `eng+spa`), output type (searchable PDF, text sidecar).
- Runtime deps: `pdftoppm`, `tesseract` (both available in this environment), optionally `pdftotext` for verification.
- Pipeline per PDF:
  1. rasterize each page at 300 DPI with `pdftoppm`;
  2. run `tesseract page.png page pdf -l <lang>`;
  3. merge per-page OCR PDFs with pdfcpu;
  4. optional `.txt` sidecar;
  5. verify page count and extracted text.
- Warn that pages are rasterized; vector text is replaced by an image + invisible text layer.
- Plan fails with clear install guidance when dependencies are missing.

### Transcription tool (`tool.video.transcribe`)

- Runtime deps: `whisper-cli` (whisper.cpp) + a model path configured in Settings.
- Pipeline: ffmpeg extracts mono 16 kHz audio → whisper-cli produces SRT/VTT; optional burn-in with ffmpeg subtitles filter.
- Do not bundle models; do not auto-download without explicit consent.
- Capability panel shows model status and a "Choose model" action.

### Smart crop / rename

- Auto-crop: detect uniform borders in Image Crop, preview the suggested crop, let the user accept it. Pure local heuristic (no model).
- Smart rename: suggest names from PDF title/OCR first line/metadata; user confirms before applying. This can be a recipe step later.

### Packaging and privacy

- `Local Intelligence` panel in Settings:
  - OCR: available / missing `tesseract` or `pdftoppm`;
  - Transcription: available / missing `whisper-cli` or model;
  - PDF compression: Ghostscript available / missing.
- No network calls unless the user explicitly downloads a model; prefer system binaries and user-provided model files.
- Keep the base app size small; intelligence is an optional capability pack.

### Acceptance

1. OCR produces a searchable PDF with the same page count; text extraction is verified.
2. Transcription produces a valid SRT/VTT; optional burn-in produces a video.
3. Missing dependencies show actionable setup guidance; the rest of the app keeps working.
4. No cloud calls and no automatic model downloads.

### Risks

- External dependency availability, licensing and performance.
- OCR rasterization quality/fidelity.
- Model size and CPU/GPU cost.
- Do not let optional intelligence degrade the core suite.

---

## Cross-cutting architecture

| Area | New code |
| --- | --- |
| Recipes | `internal/recipes`, `recipes/progress/v1`, `ListRecipesV1`, `PlanRecipeV1`, `RunRecipeV1`, `GetRecipeStatusV1`, `CancelRecipeV1` |
| Target size | `tool.image.compress` (quality search + bounded downscale), `targetSizeBytes` in `tool.pdf.optimize`; video target size pending |
| Verification | `internal/verify`, `VerifyJobV1`, `VerificationReportV1` |
| Undo/history | `internal/jobhistory`, `UndoJobV1`, `RestoreJobV1`, `EmptyTrashV1`, completion hook |
| Automation | `internal/automation`, fsnotify, rule store, `automation/run/v1`, `/automations` |
| Intelligence | `tool.pdf.ocr`, `tool.video.transcribe`, capability detection, Settings panel |
| Frontend | Recipes section/run view, target-size controls, verification/undo in ResultCard, Automations page, Local Intelligence settings |

## Delivery phases

### Phase 0 — Foundations (8–12 engineering days)

- Recipe model + RecipeRunner + plan/run/status endpoints + progress event.
- Verification service + `VerifyJobV1`.
- Job history store + undo/restore + trash.
- Image target-size compression shipped as `tool.image.compress` (quality search
  + bounded downscale); video target size remains pending.
- Tests and docs.

### Phase 1 — Differentiator MVP (8–12 days)

- Built-in recipes: Email PDF, Web image set, Archive document, Social clip (Clean scan after OCR).
- Target-size UI in Image Convert, PDF Optimize, Video tools.
- Verified outputs + Undo in ResultCard and Recent conversions.
- First vertical slice: **Email PDF** (linear: merge → optimize target → verify → undo), then **Web image set** (fan-out).

### Phase 2 — Local automation (6–9 days)

- Watcher service, rule store, stability/loop protection.
- Automations page.
- Optional CLI; OS context menu later.

### Phase 3 — Local Intelligence (8–14 days)

- OCR tool + capability detection.
- Transcription tool + model setup.
- Auto-crop; smart rename.
- Settings capability panel.

### Phase 4 — Polish (ongoing)

- User-defined recipes / recipe builder.
- Preset sharing, CLI packaging, context menu.
- Performance and docs.

Total indicative effort: **30–47 engineering days**, delivered in small releases; not a single big-bang.

## Testing strategy

- Go unit/integration: recipe runner (linear/fan-out/failure/cancel), target-size loops (bounded), verification, job history, undo safety, watcher stability/loop prevention.
- Frontend specs: recipe cards/run/progress, target-size controls, verified/undo UI, automations CRUD, capability states.
- Manual: real PDFs with bookmarks, images, the Downloads MP4, locked/permission-denied folders, app restart with interrupted jobs.
- Failure cases: missing Ghostscript/Tesseract/whisper, modified output before undo, output folder inside watch folder, duplicate file events.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| Recipe engine scope creep | Curated recipes first; no general builder in Phase 1 |
| Target-size PDF fidelity | pdfcpu → Ghostscript settings; explicit warnings; verify actual size |
| Undo deleting user files | Fingerprints, skip modified files, app trash, never permanent delete |
| Watcher duplicate/loops | Stability window, debounce, output-dir exclusion, limits |
| Local AI dependencies/models | Optional pack, detection, user-provided model, no cloud |
| Performance | Bounded iterations, per-step progress, cancel, two-pass video warnings |

## Recommended first vertical slice

**Email PDF**:

1. Drop 1+ PDFs.
2. Recipe merges if needed, optimizes to a target size, verifies pages/size and shows an undoable result.
3. It exercises the recipe runner, target size, verification and undo with a linear chain and no new heavy dependency (Ghostscript is already present).

Then **Web image set** to exercise fan-out and publish bundles.

## Open product decisions

1. Recipe execution: curated recipes only in Phase 1, or include a simple two-step user builder?
2. PDF target size: allow Ghostscript `/screen` aggressive compression automatically, or ask before aggressive mode?
3. Automation: allow "delete source after processing" in Phase 2, or keep it permanently off?
4. Local intelligence: require system binaries only, or offer a guided model download?
