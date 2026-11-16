# Differentiation Second Slice — Web Image Set

- Status: in progress
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
   - `RecipeV1.MaxInputs` (0 = unlimited) and `RecipeV1.Highlights`.
   - `RecipeStepV1.Detail` for UI stepper copy.
   - Email PDF keeps unlimited inputs and gains the same metadata.
3. **Inspection / verification extensions**
   - Inspect and verify WebP dimensions (`golang.org/x/image/webp`).
   - Classify `.zip` (archive) and `.html`/`.htm` (document) so a valid bundle does not fail verification’s kind check.
   - Add exact-variant-width checks to the recipe verification report.
4. **Image tool background option**
   - `tool.image.convert` accepts `background` (`#RRGGBB` or `RRGGBB`) and applies it when flattening transparency; the Web image set passes white for JPEG.
5. **Generic recipe UI**
   - Replace the Email-PDF-only page with one `/recipe/:recipeId`-style page driven by route data and `RecipeV1`.
   - Email PDF keeps its target-size field and multi-PDF intake; Web image set hides the target field and enforces a single input.
   - Steps show per-step detail; plan shows all six outputs; result lists every output with verification and undo.
6. **Home**
   - Add a second recipe card for Web image set.

## Explicit decisions

- **No upscaling.** Widths are capped at the source width. A 1200 px source produces 1200 and 800 px variants; a 600 px source produces only a 600 px variant. Warnings explain the cap.
- **One input only** for this recipe (`MaxInputs: 1`). Animated GIF/WebP is out of scope; only PNG/JPEG/WebP are accepted.
- **JPEG names use `.jpg`** while the backend format is `jpeg`; the recipe passes explicit `outputPath` values so the extension and bytes match.
- **ZIP contents**: every image variant plus `srcset.html`. The standalone snippet is the same HTML.
- **`sizes` attribute**: `(max-width: 800px) 100vw, 800px`; width descriptors are the actual variant widths.
- **Failure policy**: if a variant, ZIP, or snippet fails, the recipe removes the files it created in that run and fails; it never deletes a pre-existing file.
- **Verification**: generic checks for all outputs plus a `variant_width` check per image. Missing exact width marks the run `partial_success`, never a silent pass.
- **No archive tool**: ZIP/HTML packaging lives in the recipe package using stdlib `archive/zip` and `html`.

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

- 2026-11-16: Slice scoped after mapping `internal/image`, `internal/recipes`, inspect/verify, ZIP and frontend recipe-page reuse. Backend first, then the generic recipe UI.

## Implementation notes

- `Service.plan` must stop hardcoding Email PDF’s single output and Email-specific tool warnings; `resolvedRecipe` carries `PlannedOutputs`, `PlanMessage`, and per-recipe extras.
- `Service.List` returns Email PDF first, then Web image set.
- `Service.resolve`/`execute` dispatch by recipe id.
- Recipe packaging helpers must be atomic (temp file in the output directory + rename) and clean up temp files.
- The generic frontend page reads the recipe id from route `data`; the static fallbacks keep the page usable in preview mode without IPC.
- All UI copy is English and token-based; no hardcoded colors.
