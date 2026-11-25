# FileForge Differentiation Roadmap

- Status: mostly delivered in v1.2; each section carries its own status
- Created: 2026-11-16
- Last reconciled: 2026-11-25 against `feat/v1.2-stabilization`
- Scope: five product differentiators on top of the current 20-tool file-first workbench and curated recipes
- Goal: stop competing as "another converter" and become the local workbench for **outcome recipes, verifiable outputs, and local automation**

## Positioning

Most alternatives are one of:

- single-purpose and polished (Squoosh, ImageOptim, Permute),
- broad but technical (XnConvert, PDF24, HandBrake),
- cloud web tools (Smallpdf, CloudConvert).

The free space is a **local-first desktop workbench that composes tools into outcomes, verifies the result, and can run automatically**. FileForge already has the right foundations: tool registry, plan-first jobs, batch orchestration, output safety, previews, global drop, recents and settings.

## Delivery status (v1.2)

| Differentiator | Status | Evidence |
| --- | --- | --- |
| 1. Outcome recipes | **Done** for four of the five planned recipes; global-drop recipe recommendations pending | `internal/recipes/service.go` lists Email PDF, Web image set, Social clip, Clean scan and Burned-in subtitles; runs persist in `recipe_runs.json` and a non-terminal run is marked `interrupted` on restart. "Archive document" is not implemented, and the drop overlay recommends tools, not recipes. |
| 2. Target size | **Partial** | Image Compress and the Email PDF recipe accept a target; the PDF optimize engine accepts `targetSizeBytes`; the PDF Optimize page has no target control; video target size is not implemented. |
| 3. Verified outputs + undo | **Done** | `internal/verify` adds a `target_size` check, `internal/jobhistory` records outputs and moves untouched files to the app trash, and Settings → Trash and history exposes open/reveal/undo/restore/empty. |
| 4. Local automation | **Done** for the planned MVP | `internal/automation` has the rule store, watcher, debounce, loop protection, recursive watching and file-name patterns; the Automations page and Home status card exist. A max-file-size limit and a per-rule concurrency setting were not built. |
| 5. Local Intelligence | **Partial** | PDF/image OCR and video transcription shipped with dependency detection and a Settings capability panel; smart crop and smart rename are not implemented. |

## Principles

1. **Outcome over tool.** Users pick "Send by email", not "PDF Optimize + Merge".
2. **Local and private by default.** No uploads, accounts or cloud processing.
3. **Verifiable.** A green "done" is not enough; show page counts, dimensions, codecs, sizes and checksums where useful.
4. **Non-destructive.** Never overwrite; undo moves outputs to an app trash, never deletes user files.
5. **Progressive depth.** Curated recipes first; a general recipe builder later.
6. **Optional intelligence.** OCR/model features are opt-in packs with explicit dependency detection, never bundled models without consent.

---

## 1. Outcome Recipes / Publish Sets — done (four of five recipes)

### User promise

Drop files, choose a result, get a verified bundle: one step, multiple tools, multiple outputs.

### Built-in recipes (MVP)

| Recipe | Input | Steps | Final outputs | Status |
| --- | --- | --- | --- | --- |
| **Email PDF** | 1+ PDFs | Merge when >1 → optimize to target size | One small PDF | Shipped (`recipe.email_pdf`) |
| **Web image set** | 1 image | Normalize → resize to 1600/800 → WebP + JPEG → strip metadata | 4 images + ZIP + HTML `srcset` snippet | Shipped (`recipe.web_image_set`) |
| **Archive document** | 1 DOCX | DOCX → PDF/A → metadata | Archival PDF + checksum | Pending (not in `Service.List`) |
| **Social clip** | 1 video | Trim (optional) → MP4 + WebM → poster frame | 2 videos + poster | Shipped (`recipe.social_clip`) |
| **Clean scan** | 1 PDF | OCR → optimize → verify page count | Searchable PDF | Shipped (`recipe.clean_scan`) |
| **Burned-in subtitles** | 1 video | Transcribe → burn subtitles → verify | Subtitled MP4 | Shipped beyond the original table (`recipe.burned_subtitles`) |

"Clean scan" was planned as part of the Local Intelligence phase and shipped with
it.

### Architecture

- `internal/recipes` with a curated `Recipe` model:
  - `ID`, `Name`, `Description`, `Category`, `InputKind`, `Steps`, `Outputs`.
  - A step references a tool ID and a small option template; outputs from a step can feed one or more later steps.
  - Phase 1 recipes are **built-in Go definitions**, not user-editable JSON.
- New `RecipeRunner`:
  - Creates a temp workspace for intermediates.
  - Submits each step as a normal job to the shared `jobs.Engine` (reuses validation, retries, progress, cancellation, persistence).
  - Passes planned outputs of step N as inputs to step N+1.
  - Supports linear chains and final fan-out (one source → multiple output variants).
  - Emits `recipes/progress` with `stepIndex`, `stepCount`, `toolName`, `stage`, `percent`, `eta`, plus a compact `recipes/state` snapshot.
  - On failure: stop by default, clean intermediates, keep no partial final outputs unless the user chose "keep successful outputs".
  - On success: move/copy final outputs to the chosen destination with collision-safe naming; verify them; record job history.
- Endpoints, implemented after the v1 API rename as:
  - `ListRecipes`
  - `PlanRecipe(req)` → recipe metadata, input validation, final destination, warnings (no intermediate paths)
  - `RunRecipe(req)` → recipe job ID
  - `GetRecipeStatus(id)` → per-step progress + final JobResult-like output
  - `CancelRecipe(id)`
- Events: `recipes/progress` and `recipes/state` (unversioned).
- Recipe runs persist in `os.UserConfigDir()/fileforge/recipe_runs.json` through the shared `jsonstore`; on startup a non-terminal run is marked `interrupted` and is never resumed. Undo still goes through the shared job history. (Shipped in T3.)

### UI

- Home gets a **Recipes** section above the tool grid:
  - Cards with outcome name, one-line description, accepted types, and a "Use recipe" action.
- Global drop overlay recommends recipes first when the file type matches (e.g. PDF → Email PDF, Clean scan; image → Web image set). **Pending:** the overlay currently recommends tools, not recipes (`components/global-drop/drop-recommendations.ts`).
- Recipe run view:
  - file cards, destination plan, recipe steps as a compact stepper;
  - one primary action with the recipe name;
  - per-step progress and cancel;
  - final result card with verified outputs and undo.
- Command palette: recipes are included and grouped under a Recipes section (`command-palette.ts`).
- Phase 2: a recipe detail/builder page for choosing options and duplicating a built-in recipe. A full drag-and-drop builder is not required.

### Acceptance

1. At least 3 built-in recipes run end-to-end from Home and global drop.
2. Each step reports progress; cancel stops the chain; temp files are cleaned.
3. Final outputs are collision-safe, verified and undoable.
4. Fan-out recipes (Web image set) produce multiple outputs and a ZIP/HTML snippet.
5. Go and frontend tests cover the runner, failure cleanup and fan-out.

**Status v1.2:** partially met. Five recipes run end-to-end from Home and the
recipe page; Archive document and recipe recommendations in the global drop are
not built.

### Risks

- Mapping tool options/outputs across steps; start with curated recipes to avoid a generic schema.
- Partial failures and intermediate cleanup.
- Long chains need clear progress and a cancel path.
- Do not build a general visual builder in Phase 1.

---

## 2. Target size ("under X MB") — partial (images and PDF, no video)

### User promise

"Make this fit in 10 MB" without understanding quality, bitrate or DPI.

### Architecture

- Shared option `targetSizeBytes int64` on relevant job requests.
- Per-media strategies:
  - **Images (done, `tool.image.compress`)**: quality search plus bounded
    downscale; PNG has no quality knob, so it uses palette quantisation and
    reports "best effort"; the chosen quality, dimensions and actual size are
    reported.
  - **PDF (engine done, `tool.pdf.optimize`, optionally merge)**:
    * try pdfcpu optimize;
    * if still over target, try Ghostscript `-dPDFSETTINGS=/ebook`, then `/screen`; stop at the first result under target;
    * warn that aggressive PDF compression can downsample images and affect fidelity;
    * if Ghostscript is missing, show a clear runtime dependency message;
    * do not rasterize pages by default; if added later, require explicit consent and verify searchability.
  - **Video (pending)** (`video.convert`, `video.trim`, `video.merge`):
    * compute total bitrate from target bytes and duration, subtract audio bitrate (AAC 128k / Opus 96k);
    * use two-pass encoding for x264/VP9;
    * retry up to 2 times at 90% bitrate if the output is still over target;
    * warn if the computed video bitrate is below a quality floor.
- Plan shows `Target ≤ X MB` and "best effort"; a plan warning explains which strategies will be tried. **Partial:** Image Compress shows the target summary on the form and the result message reports best effort; `OutputPlanCard` has no target chip or strategy warning yet.
- Result verification compares actual bytes with the target and shows a pass/warn state. Shipped in T2: `VerifyPathsWithTarget` adds a `target_size` check.

### UI

- `Target size` MB input exists in:
  - **Image Compress** (shipped; not Image Convert)
  - **Email PDF** recipe (shipped; passes `targetSizeBytes` to the optimize step)
  - PDF Optimize: **pending**; the engine accepts the option, the page has no field
  - Video Convert / Trim / Merge: **pending**
- In the plan card: target chip and strategy note. **Pending:** the target lives on the tool page; `OutputPlanCard` has no target chip (`components/tool-flow/output-plan-card.*`).
- In the result card: `8.2 MB / target 10 MB ✓` or `11.4 MB — best effort couldn't reach 10 MB` with an action "Try stronger compression" (re-run with a lower target/strategy). The comparison ships; the one-click "try stronger" action does not.

### Acceptance

1. Images and videos reach the target within a small tolerance; the result reports the actual size.
2. PDF uses pdfcpu/Ghostscript and reports when the target is not reachable.
3. No infinite loops; bounded iterations; clear warnings.
4. Go and frontend tests cover success, best-effort failure and missing Ghostscript.

**Status v1.2:** items 2–4 are met for images and PDF. Item 1 is met for images
only; video target size, the PDF Optimize input and the "try stronger" action
remain pending.

### Risks

- PDF target size is not always achievable without rasterization; be explicit.
- Video two-pass doubles encode time; show a forecast.
- Very small targets can produce unusable quality; enforce floors and warn.

---

## 3. Verified Outputs + Undo — done

### User promise

"Show me it worked, and let me take it back if I don't like it."

### Verification

- `internal/verify` and the `VerifyJob(jobId)` endpoint (unversioned after the v1 API rename).
- For every output:
  - exists, regular file, readable, size > 0;
  - extension/MIME matches the expected tool output;
  - image dimensions; PDF page count and page dimensions; video duration/codec/resolution/audio;
  - target size met, when requested;
  - optional SHA-256 for high-assurance recipes (or for undo fingerprints).
- Response: `VerificationReport{ success, checks[], outputs[], warnings[] }`.
- Frontend ResultCard fetches the verification after terminal status and shows:
  - `Verified` badge with 3–5 checks;
  - before/after size and percentage;
  - warnings when a check fails.
- Recent conversions store a compact verification summary. **Pending:** recent
  conversions keep the job summary but not the verification report
  (`recent-conversions.service.ts`).

### Undo

- `internal/jobhistory`:
  - records terminal jobs with `jobId`, `toolId`, outputs `{path,size,mtime}`, timestamp and `undone` state;
  - persisted in `os.UserConfigDir()/fileforge/job_history.json`, capped by job count and trash size/age;
  - `UndoJob(jobId)`:
    * verify each output still matches the recorded size/mtime (skip modified files);
    * move untouched outputs to `os.UserConfigDir()/fileforge/trash/<jobId>/`;
    * remove empty directories left by split outputs;
    * mark the job undone and report per-file results;
  - `RestoreJob(jobId)` moves files back when the destination is still free;
  - `EmptyTrash()` is explicit and confirmed.
- The execution facade records terminal jobs in job history (`s.history.RecordJobWithTarget` in `internal/services/tooling.go`), not the frontend. Recipe runs record through the same store.
- UI (shipped):
  - `Undo` in the ResultCard and Recent conversions;
  - `Restore`, open and reveal per job; a **Trash and history** panel in Settings
    with real trash size and an explicit empty action.
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

**Status v1.2:** met. Job history survives restart, undo skips modified files,
and the trash panel is explicit about what it removes.

### Risks

- Fingerprints must avoid deleting modified files.
- Directory outputs (PDF split) need per-file recording.
- Large job histories need caps/cleanup.

---

## 4. Local Automation — done for the planned MVP

### User promise

"Put a file in this folder and it comes out processed, without opening the app."

### Architecture

- Add `github.com/fsnotify/fsnotify`.
- `internal/automation` (shipped):
  - `AutomationRule{ ID, Name, Enabled, WatchDir, Recursive, Extensions, Patterns, RecipeID, OutputDir, Options, DeleteSource, CreatedAt, UpdatedAt, LastRunAt, LastStatus, LastMessage, LastFile, LastOutputs }`.
  - Store in `os.UserConfigDir()/fileforge/automation_rules.json`; runs in `automation_runs.json`.
  - One watcher per rule.
  - File stability: wait until size is unchanged; debounce duplicate create/write events.
  - Ignore hidden/temp files, the output directory, and `.fileforge` workspace dirs; prevent loops. A recursive rule watches at most 256 directories.
  - `Recursive` and `Patterns` shipped in v1.2; patterns match the base name with `filepath.Match` semantics and are validated at save time.
  - Not built: a max-file-size limit and a per-rule concurrency setting. Runs for one rule are serialized through the watcher queue.
  - Run the matching recipe with the file; write final outputs to `OutputDir`; emit `automation/run` events with rule/file/status/outputs.
  - Persist last run/status per rule; start enabled rules on app startup and stop them on shutdown.
- Phase 2b (optional): CLI mode (`fileforge --recipe ... --input ...`) reusing the same recipe runner; OS context-menu integration is platform-specific and later.

### UI

- New `/automations` page (System group):
  - rule list with enabled toggle, watch folder, recipe, output folder, last run/status;
  - create/edit form: folder pickers, recipe select, extensions, recursive
    toggle, file-name patterns with save-time validation, delete-source warning
    (default off);
  - empty state and a short "How it works" explainer.
- Home status card: `2 rules active · last run 2 min ago`.
- Settings: trash/empty ships in the Trash and history panel. Start at login and
  a per-rule concurrency setting are **not built**.

### Acceptance

1. A file dropped into a watched folder runs the recipe exactly once and produces outputs.
2. Duplicate events and partial copies do not double-run.
3. Disabling a rule stops processing; deleting a rule cleans up watchers.
4. Loops are prevented when the output folder is inside the watch folder.
5. Missing recipe/folder/dependency shows a clear disabled/degraded state.
6. Rules persist across restart.

**Status v1.2:** met, including recursion and file-name patterns. The planned
max-file-size limit and per-rule concurrency setting were not built.

### Risks

- fsnotify semantics differ across OSes; test Linux/macOS/Windows.
- File stability and antivirus/cloud-sync delays.
- Resource limits for large folders.
- Do not delete source files by default.

---

## 5. Local Intelligence (optional pack) — partial (OCR and transcription shipped)

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

### Smart crop / rename — pending

- Auto-crop: detect uniform borders in Image Crop, preview the suggested crop, let the user accept it. Pure local heuristic (no model). **Not implemented.**
- Smart rename: suggest names from PDF title/OCR first line/metadata; user confirms before applying. This can be a recipe step later. **Not implemented.**

### Packaging and privacy

The capability panel shipped in v1.2 (`internal/capabilities`, Settings →
Tools & Models). It reports every dependency it can detect:

- `pdftoppm`, `tesseract`, `pdftotext` for OCR;
- `gs` for PDF compression and PDF/A;
- `ffmpeg`, `ffprobe` for video;
- `libvips`, `cwebp` for images;
- `pdf2docx`, the background-removal pack and Whisper;
- each row carries install guidance when missing.

No network calls happen unless the user explicitly downloads a model; system
binaries and user-provided model files are preferred. Intelligence stays an
optional capability pack, not a bundled model set.

### Acceptance

1. OCR produces a searchable PDF with the same page count; text extraction is verified.
2. Transcription produces a valid SRT/VTT; optional burn-in produces a video.
3. Missing dependencies show actionable setup guidance; the rest of the app keeps working.
4. No cloud calls and no automatic model downloads.

**Status v1.2:** items 1–4 are met for `tool.pdf.ocr` and
`tool.video.transcribe`. Smart crop and smart rename remain pending.

### Risks

- External dependency availability, licensing and performance.
- OCR rasterization quality/fidelity.
- Model size and CPU/GPU cost.
- Do not let optional intelligence degrade the core suite.

---

## Cross-cutting architecture

| Area | Code | Status |
| --- | --- | --- |
| Recipes | `internal/recipes`, `recipes/progress`, `recipes/state`, `ListRecipes`, `PlanRecipe`, `RunRecipe`, `GetRecipeStatus`, `CancelRecipe`, `recipe_runs.json` | Done for four recipes plus Burned-in subtitles; Archive document pending |
| Target size | `tool.image.compress`, `targetSizeBytes` in `tool.pdf.optimize`, target check in `internal/verify` | Partial: images + PDF engine + Email PDF; no video, no PDF Optimize field |
| Verification | `internal/verify`, `VerifyJob`, `VerificationReport` | Done |
| Undo/history | `internal/jobhistory`, `UndoJob`, `RestoreJob`, `EmptyTrash`, completion hook | Done, with the Settings trash/history panel |
| Automation | `internal/automation`, fsnotify, rule/run stores, `automation/run`, `/automations`, recursion + patterns | Done for the MVP; no max-file-size or per-rule concurrency setting |
| Intelligence | `tool.pdf.ocr`, `tool.image.ocr`, `tool.video.transcribe`, `internal/capabilities`, Settings panel | OCR + transcription done; smart crop/rename pending |
| Frontend | Recipes section/run view, target-size controls where shipped, verification/undo in ResultCard, Automations page, capability panel | Done for shipped backends |

## Delivery phases

### Phase 0 — Foundations (8–12 engineering days) — done

- Recipe model + RecipeRunner + plan/run/status endpoints + progress event.
- Verification service + `VerifyJob`.
- Job history store + undo/restore + trash.
- Image target-size compression shipped as `tool.image.compress` (quality search
  + bounded downscale); video target size remains pending.
- Tests and docs.

### Phase 1 — Differentiator MVP (8–12 days) — mostly done

- Built-in recipes shipped: Email PDF, Web image set, Social clip, Clean scan and Burned-in subtitles. Archive document is pending.
- Target-size UI shipped in Image Compress and the Email PDF recipe; PDF Optimize and video controls are pending.
- Verified outputs + Undo in ResultCard and Recent conversions: done.
- Vertical slices shipped: **Email PDF** and **Web image set**.

### Phase 2 — Local automation (6–9 days) — done

- Watcher service, rule store, stability/loop protection: done.
- Recursive watching and file-name patterns: done in v1.2.
- Automations page: done.
- Optional CLI and OS context menu: pending (backlog).

### Phase 3 — Local Intelligence (8–14 days) — partial

- OCR tool + capability detection: done.
- Transcription tool + model setup: done.
- Auto-crop; smart rename: pending.
- Settings capability panel: done.

### Phase 4 — Polish (ongoing) — not started

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

## Product decisions, as resolved in v1.2

1. Recipe execution: **curated only**. There is no user recipe builder; Phase 4 keeps it as a proposal.
2. PDF target size: the optimizer tries pdfcpu, then Ghostscript `/ebook`, then `/screen` automatically. The plan warns when compression may downsample images and reports when the target is not reachable.
3. Automation: **"delete source after processing" is opt-in** (`DeleteSource`, default off) and only runs after a full success on an unchanged file.
4. Local intelligence: **guided managed download** through Settings → Tools & Models, with system-binary detection in the capability panel. No automatic model download at run time.
