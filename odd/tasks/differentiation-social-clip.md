# Differentiation Third Slice — Social Clip

- Status: implemented on branch; pending desktop end-to-end pass and merge
- Branch: `feat/differentiation-social-clip`
- Started: 2026-11-16
- Plan: `docs/differentiation-roadmap.md`
- Previous slices: Email PDF and Web image set (merged to main)
- Goal: prove the recipe engine on **video fan-out**: one source video → MP4 + WebM + poster → verified metadata → undo

## Why this slice instead of Archive document

The roadmap’s Archive document recipe depends on LibreOffice/pandoc for DOCX → PDF/A. This environment has neither (`libreoffice`, `soffice`, `pandoc` are absent), so that recipe would not be runnable or verifiable here. Social clip uses the installed ffmpeg/ffprobe toolchain, so it can be tested end-to-end with real media. Archive document stays in the roadmap for a machine with LibreOffice installed.

## Scope

1. **Social clip recipe** (`recipe.social_clip`)
   - Input: exactly one video (`.mp4`, `.mov`, `.mkv`, `.webm`, `.avi`).
   - Outputs:
     - `<stem>-social.mp4` — H.264 + AAC, high quality, source resolution.
     - `<stem>-social.webm` — VP9 + Opus, high quality, source resolution.
     - `<stem>-poster.jpg` — JPEG frame from the first second (or the midpoint for clips shorter than 2 s).
   - All outputs are collision-safe, use a web-safe stem, and land in the chosen folder (default: next to the source).
   - No resolution scaling; preserve the source dimensions. No trim in this slice.
2. **Video metadata inspection**
   - `FileInfoV1` gains `videoCodec` and `audioCodec`.
   - `inspect.ProbeVideoMetadata` reads duration, video width/height, video codec and audio codec through one ffprobe JSON call.
   - `InspectFile` fills those fields for video files; `ProbeVideoDuration` stays available for trim/merge.
   - `verify.videoChecks` adds dimensions, video codec and audio codec checks when metadata is available.
3. **Social clip verification**
   - Generic checks for all three outputs.
   - Recipe checks:
     - MP4 video codec `h264`, audio `aac` when the source has audio.
     - WebM video codec `vp9`, audio `opus` when the source has audio.
     - Output duration matches the source within `max(0.5 s, 2%)`.
     - Poster is a valid JPEG with dimensions.
   - Any mismatch makes the run `partial_success`, never a silent pass.
4. **Generic recipe UI**
   - Add the Social clip card to Home.
   - The existing route-driven recipe page handles the video input kind, single-file intake, hidden target size, three steps, three outputs, verification and undo.
   - Queue metadata shows size, duration and resolution.
5. **Failure policy**
   - If MP4, WebM or poster fails, remove only the files created by that run; never delete pre-existing files.
   - Cancel removes partial outputs and reports `cancelled`.

## Explicit decisions

- **Source resolution, not 1080p.** The existing video tool scales by height and would shrink portrait video incorrectly. The recipe keeps source dimensions; the high preset controls quality.
- **Two sequential encodes.** Progress is emitted per format; a large source takes longer. No parallel encodes in this slice.
- **Poster time**: 1 second when the duration is at least 2 seconds, otherwise half the duration. The command uses ffmpeg with a fast seek.
- **No audio source**: outputs are video-only; the audio codec check is skipped.
- **Codec checks are explicit**: generic verification records codec presence; the recipe checks the expected encoder.
- **No ZIP**: the roadmap calls for two videos + poster, not a bundle.
- **Shared stem helper**: the Web image set sanitizer was renamed to `recipeSourceStem` and is reused for social output names.

## Acceptance criteria

1. A real source video produces MP4, WebM and JPEG poster outputs in the chosen folder; all files are non-empty.
2. MP4 metadata reports `h264` video and `aac` audio; WebM reports `vp9` and `opus`; durations match the source within tolerance.
3. A source without audio still succeeds and skips the audio check.
4. A short clip (<2 s) produces a poster from the midpoint and a plan warning.
5. The plan rejects zero/multiple inputs and unsupported extensions.
6. The recipe runs from Home through the generic UI, shows per-format progress, can be cancelled, and the result lists all three files with verification and undo.
7. Failure in the second encode or the poster removes the files created by that run.
8. Go tests, frontend specs and build pass; bindings are regenerated, never hand-edited.
9. No regressions in Email PDF, Web image set, the 12 tools, or ResultCard/Undo.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: Home with three recipe cards; Social clip plan/run/result and undo; dark and light themes.

## Progress

- 2026-11-16: Slice scoped after confirming ffmpeg/ffprobe and the libx264/libvpx-vp9/aac/libopus encoders are installed, while LibreOffice/pandoc are not.
- 2026-11-16: Backend implemented: `FileInfoV1` codecs, `ProbeVideoMetadata`, video verification checks, shared stem helper, Social clip plan/execute/poster/verification/cleanup, service wiring and regenerated bindings. Commit `cd5a8ad`.
- 2026-11-16: Frontend implemented: Social clip fallback/card, video input kind copy, route, duration/resolution metadata and tests. Commit `0b9231a`.

## Verification evidence

- `go build ./...` — pass.
- `go test ./... -count=1` — pass, including `internal/recipes` real-video tests (MP4/WebM/poster, codecs, duration, silent source, short clip, failure/cancel cleanup), `internal/inspect` metadata parsing and `internal/verify` codec/dimension checks.
- `cd frontend && npm run build:dev` — success. Pre-existing budget warnings remain (initial bundle; `home.css`, `result-card.css`, other component styles), all under the 8 kB error cap.
- `CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless` — 410/410 success.
- `npx tsc --noEmit -p tsconfig.app.json` — exit 0.
- `npx agent-browser` visual check at 1440×900:
  - Home shows Email PDF, Web image set and Social clip cards;
  - Social clip page shows the video intake, hidden target, source-resolution highlights and three steps with details;
  - dark and light themes render correctly.
  - Preview mode only (`ng serve`), so IPC was not exercised.
- Backend tests run real ffmpeg conversions on generated 160×120 clips and assert the encoded files, codecs and poster.
- Pending: desktop end-to-end in the Wails window with a real user video and undo. The IPC path is covered by Go integration tests, regenerated bindings and mocked frontend specs.

## Status by acceptance criterion

| # | Status | Evidence |
| --- | --- | --- |
| 1 | Automated | `TestRunSocialClipProducesVerifiedBundle` |
| 2 | Automated | codec/duration assertions in the run test and `addSocialClipChecks` |
| 3 | Automated | `TestRunSocialClipSilentShortClipSkipsAudioChecks` |
| 4 | Automated | plan warnings in `TestPlanSocialClipPlansBothEncodesAndPoster`, poster existence in the silent short test |
| 5 | Automated | `TestPlanSocialClipRejectsInvalidRequests` |
| 6 | Automated + visual | recipe-run Social clip specs, backend cancel/failure tests, visual page check |
| 7 | Automated | `TestRunSocialClipFailureOnSecondEncodeRemovesCreatedFiles`, `TestRunSocialClipPosterFailureRemovesCreatedFiles` |
| 8 | Verified | Go tests, 410 Karma specs, build, tsc; bindings regenerated only |
| 9 | Verified | Full Go and frontend suites include Email PDF, Web image set and all 12 tools |

## Next steps

1. Run the desktop end-to-end pass with a real video and record the result.
2. Merge the branch when the user requests it.
3. Next roadmap slices: Archive document on a machine with LibreOffice, Clean scan (OCR with tesseract/pdftoppm), or Local Automation (watch folders).
