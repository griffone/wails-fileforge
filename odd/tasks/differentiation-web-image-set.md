# Differentiation Second Slice — Web Image Set

- Status: implemented on branch; pending desktop end-to-end pass and merge
- Branch: `feat/differentiation-web-image-set`
- Started: 2026-11-16
- Plan: `docs/differentiation-roadmap.md`
- Previous slice: `odd/tasks/differentiation-email-pdf.md` (merged to main)
- Goal: prove the recipe engine’s **fan-out** path: one source image → responsive variants → ZIP + `srcset` snippet → verified outputs → undo

## Scope

Implement the second vertical slice from the differentiation roadmap.

1. **Web image set recipe** (`recipe.web_image_set`)
   - Input: exactly one image (`.png`, `.jpg`, `.jpeg`, `.webp`).
   - Output widths: 1600 and 800 px, capped at the source width (never upscale); dedupe equal widths.
   - Formats per width: WebP (quality 80) and JPEG (quality 85), metadata stripped.
   - JPEG variants composite transparency on white.
   - Final outputs: the image variants, a ZIP bundle (variants + snippet), and a ready-to-paste `srcset` HTML snippet.
   - All outputs are collision-safe and land in the chosen folder (default: next to the source).
   - No 1600 variant file is fabricated from a smaller source: when the source is narrower, the variant is capped and the plan/result warns.
2. **Recipe model extensions**
   - `RecipeV1.MaxInputs` (0 = unlimited), `RecipeV1.Highlights`, `RecipeStepV1.Detail`.
   - Email PDF keeps unlimited inputs and gains the same metadata.
3. **Inspection / verification extensions**
   - Inspect and verify WebP dimensions (`golang.org/x/image/webp`).
   - Classify `.zip` (archive) and `.html`/`.htm` (document) so a valid bundle does not fail verification’s kind check.
   - Add exact-variant-width checks to the recipe verification report.
4. **Image tool background option**
   - `tool.image.convert` accepts `background` (`#RRGGBB` or `RRGGBB`) and applies it when flattening transparency; the Web image set passes white for JPEG.
5. **Generic recipe UI**
   - One route-driven recipe page for Email PDF and Web image set.
   - Email PDF keeps its target-size field and multi-PDF intake; Web image set hides the target field and enforces a single input.
   - Steps show per-step detail; plan shows all six outputs; result lists every output with verification and undo.
   - Live `recipes/progress/v1` events drive the stepper and progress bar between polls.
6. **Home**
   - Second recipe card for Web image set.

## Explicit decisions

- **No upscaling.** Widths are capped at the source width. A 1200 px source produces 1200 and 800 px variants; a 600 px source produces only a 600 px variant. A warning explains the cap.
- **One input only** for this recipe (`MaxInputs: 1`). Animated GIF/WebP is out of scope; only PNG/JPEG/WebP are accepted.
- **JPEG names use `.jpg`** while the backend format is `jpeg`; the recipe passes explicit `outputPath` values so the extension and bytes match.
- **Web-safe base names**: the source stem is lowercased and restricted to ASCII letters, digits, dots, underscores and dashes; spaces, ampersands and other unsafe runs collapse into a single dash. This keeps folder names and `srcset` URLs clean.
- **ZIP contents**: every image variant plus `srcset.html`. The standalone snippet is the same HTML.
- **`sizes` attribute**: `(max-width: 800px) 100vw, 800px`; width descriptors are the actual variant widths.
- **Failure policy**: if a variant, ZIP, or snippet fails, the recipe removes the files it created in that run and fails; it never deletes a pre-existing file.
- **Verification**: generic checks for all outputs plus a `variant_width` check per image. Missing exact width marks the run `partial_success`, never a silent pass.
- **No archive tool**: ZIP/HTML packaging lives in the recipe package using stdlib `archive/zip` and `html`.
- **Generic UI**: the page reads `recipeId` from route `data`; static fallbacks keep both recipes usable in preview mode without IPC.

## Acceptance criteria

1. A 2000 px source produces 1600 and 800 px WebP + JPEG variants, a ZIP with all variants plus `srcset.html`, and a standalone HTML snippet; verification passes.
2. A 1200 px source produces 1200 and 800 px variants, no upscaled file, and a warning.
3. A 600 px source produces only a 600 px variant pair and a warning.
4. The HTML snippet references the actual file names and widths, with escaped attributes.
5. Transparent PNG → JPEG uses a white background; WebP keeps alpha.
6. The recipe runs from Home through the generic recipe page, shows variant progress and step detail, can be cancelled, and the terminal result lists every file with verification and undo.
7. A failure mid-run leaves no partial outputs from that run.
8. Go tests, frontend specs and build pass; bindings are regenerated, never hand-edited.
9. No regressions in Email PDF, the 12 tools, or the existing ResultCard/Undo behavior.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: Home with two recipe cards; Web image set plan, run, result and undo; dark and light themes.

## Progress

- 2026-11-16: Slice scoped after mapping `internal/image`, `internal/recipes`, inspect/verify, ZIP and frontend recipe-page reuse.
- 2026-11-16: Backend implemented: recipe models, generic service plan/resolve/execute, Web image set fan-out, ZIP/snippet writers, variant-width verification, image `background` option, WebP/ZIP/HTML inspection, and regenerated bindings. Commit `c344f18`.
- 2026-11-16: Generic frontend recipe page implemented from the Email page: route data, target visibility, single/multi intake, highlights, step details, per-output result rows, six-output plan limit, and Home second card. Commit `7a4be22` and `e000939`.
- 2026-11-16: Live `recipes/progress/v1` wiring added to the Wails service and recipe page; the stepper and bar update immediately, with the polled status remaining the source of truth for outputs. Part of `7a4be22`.
- 2026-11-16: Web image set output base names sanitized to lowercase web-safe ASCII so folder names and `srcset` URLs never contain spaces or ampersands; snippet attribute escaping is still covered by a dedicated unit test.
- 2026-11-16: Recipe partial results now show the backend message instead of the generic “Retry them below” copy, which does not apply when the files exist but verification needs attention. Commit `ec3a23d`.

## Verification evidence

- `go build ./...` — pass.
- `go test ./... -count=1` — pass (including `internal/recipes`, `internal/image`, `internal/inspect`, `internal/verify`).
- `cd frontend && npm run build:dev` — success. Pre-existing budget warnings remain (initial bundle; `home.css`, `result-card.css`, other component styles), all under the 8 kB error cap.
- `CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless` — 406/406 success.
- `npx tsc --noEmit -p tsconfig.app.json` — exit 0.
- `npx agent-browser` visual check at 1440×900:
  - Home shows Email PDF and Web image set cards side by side;
  - Web image set page hides the target field, shows the three steps with detail copy, the highlights row, and the single-image intake;
  - Email PDF still shows the 10 MB target field and multi-PDF intake.
  - Preview mode only (`ng serve`), so IPC was not exercised.
- Backend tests exercise real PNG → WebP/JPEG conversion, ZIP contents, snippet contents, variant widths, transparent-PNG flattening, cancellation and failure cleanup.
- Pending: desktop end-to-end in the Wails window with a real 2000/1200/600 px image and undo. The IPC path is covered by Go integration tests, regenerated bindings and mocked frontend specs.

## Status by acceptance criterion

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Automated | `TestRunWebImageSetProducesVerifiedBundle` |
| 2 | Automated | `TestPlanWebImageSetCapsTheWidestVariant` |
| 3 | Automated | `TestPlanWebImageSetNarrowSourceProducesOneWidth` |
| 4 | Automated | run test asserts srcset/ZIP names; `TestRunWebImageSetSanitizesSnippetNames`, `TestBuildWebImageSetSnippetEscapesAttributes` |
| 5 | Automated | `TestRunWebImageSetFlattensTransparencyOnJPEG`, image background tests |
| 6 | Automated + visual | recipe-run Email/Web specs, live progress spec, visual page check; backend cancel test |
| 7 | Automated | `TestRunWebImageSetFailureRemovesCreatedFiles`, `TestRunWebImageSetCancelRemovesPartialOutputs` |
| 8 | Verified | Go tests, 406 Karma specs, build, tsc; bindings regenerated only |
| 9 | Verified | Full Go suite and frontend suite include Email PDF and all 12 tools |

## Next steps

1. Run the desktop end-to-end pass with real images and record the result.
2. Merge the branch when the user requests it.
3. Next roadmap slices: Archive document (DOCX → PDF/A) or Social clip (video fan-out), then watch-folder automation.
