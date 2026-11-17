# Local Transcription (Whisper) — Sixth Slice

- Status: in progress
- Branch: `feat/transcription-whisper` (stacked on `feat/local-capabilities-polish`)
- Started: 2026-11-17
- Plan: `docs/differentiation-roadmap.md`
- Goal: transcribe video/audio locally with whisper.cpp, producing SRT/VTT/TXT with English/Spanish language selection and model status in Settings

## Installed runtime (this machine)

- whisper.cpp 1.9.5-dev built from source with `BUILD_SHARED_LIBS=OFF`.
- Binary: `~/.local/share/fileforge/whisper/whisper-cli` (3.4 MB; only system libs).
- Source/build: `~/.local/share/fileforge/src/whisper.cpp` (retained for rebuilds and the JFK sample).
- Model: `~/.local/share/fileforge/models/ggml-base.bin` (142 MB, multilingual).
- Verified:
  - English: `samples/jfk.wav` → “And so my fellow Americans, ask not what your country can do for you…”; SRT/VTT/TXT generated.
  - Spanish: `SpanishL1D2.ogg` (Wikimedia dialogue) → correct Spanish transcript with `-l es`.
- The model supports both languages; `-l auto` is also available. No cloud and no automatic downloads.

## Scope

1. **Transcription engine** (`internal/video/engine/transcribe.go`)
   - Resolve defaults:
     - binary: `~/.local/share/fileforge/whisper/whisper-cli`, then `~/.config/fileforge/whisper/whisper-cli`, then `whisper-cli` on PATH;
     - model: `~/.local/share/fileforge/models/ggml-base.bin`, then `~/.config/fileforge/models/ggml-base.bin`.
   - Capabilities: binary present, model present/size, resolved paths, language list (`auto`, `en`, `es`), model name.
   - Pipeline per media file:
     1. ffmpeg extracts mono 16 kHz PCM WAV in a temp dir;
     2. whisper.cpp runs with the selected model/language and requested output flags (`-otxt`, `-osrt`, `-ovtt`, `-of <prefix>`);
     3. outputs are published atomically to the final paths;
     4. text/segment counts are read for the result message; empty speech is a success with a clear warning;
     5. temp files are removed on success, failure or cancel.
   - `CommandRunner` injection for tests; context cancellation kills ffmpeg/whisper.
2. **Transcription tool** (`tool.video.transcribe`)
   - Inputs: one or more video/audio files (mp4, mov, mkv, webm, avi, mp3, m4a, wav, ogg, flac).
   - Options: `language` (`auto`, `en`, `es`), `formats` (`srt`, `vtt`, `txt`; default all), `whisperPath`, `modelPath` (optional; defaults resolve).
   - Outputs: `<stem>.srt`, `<stem>.vtt`, `<stem>.txt` collision-safe; only the requested formats.
   - Single and batch; runtime degraded with install guidance when binary/model missing; progress stages `extract` and `transcribe`.
   - Register in `internal/video/init.go`.
3. **Capability endpoint**
   - `GetTranscriptionCapabilitiesV1(whisperPath, modelPath)` returns availability, resolved paths, model name/size and supported languages.
4. **Settings transcription panel**
   - Show whisper status/model, resolved paths and a language note.
   - Choose whisper binary/model file (native file picker); persist overrides in `PreferencesService` (`whisperPath`, `whisperModelPath`).
   - Guidance when missing; refresh.
5. **Transcription page** (`/video-transcribe`)
   - Video/audio intake, language select (Auto/English/Spanish), format checkboxes (SRT/VTT/TXT), model status, output plan, run/cancel/progress, result, undo.
   - Show the result summary (segments/characters) and a capability banner when the model/binary is missing.
6. **Registry/tests**
   - Tool count becomes 14; Video category 4; navigation 18.
   - SRT/VTT classified as documents for verification.
   - Go engine/tool tests (fake runner + real whisper integration using the installed model/JFK fixture when available); frontend page/Settings specs; Home/registry count updates.

## Explicit decisions

- **No burn-in in this slice.** Subtitles (SRT/VTT) plus plain text are the core; burning is a separate video-render feature.
- **No automatic model downloads.** The model is installed locally; the app only reports missing paths and guidance. Users can choose another whisper.cpp model file.
- **Multilingual model, explicit language.** Auto/English/Spanish in the UI; the backend passes the language through.
- **Coarse progress.** whisper.cpp progress is not streamed in this slice; the job shows extract/transcribe stages and the UI stays responsive.
- **Defaults plus overrides.** The installed paths work out of the box; Settings can point to a different binary/model.
- **Empty speech is not a crash.** It produces valid empty subtitle/text outputs with a warning, not a fake success or a hard failure.
- **No recipe.** This is a direct tool; a transcript recipe can be added later.

## Acceptance criteria

1. `GetTranscriptionCapabilitiesV1` reports the installed binary/model and resolved paths; missing paths degrade with guidance.
2. Transcribing a short English video/audio produces non-empty SRT/VTT/TXT containing the expected words.
3. Transcribing a Spanish sample with `-l es` produces Spanish text.
4. Batch inputs produce one set of outputs per input, collision-safe; requested formats only.
5. Cancellation kills the process, cleans temp files and leaves no final outputs.
6. Settings shows transcription status and lets the user choose a binary/model; overrides persist.
7. `/video-transcribe` plans/runs with language/formats, shows progress and result, and records undoable outputs.
8. Tool count 14, Video 4, navigation 18; Home/registry specs updated.
9. Go tests, frontend specs and build pass; bindings regenerated only.
10. No regressions in OCR, automation, recipes, tools, undo or navigation.

## Checks

- `go test ./... -count=1`
- `cd frontend && npm run build:dev`
- `cd frontend && CHROME_BIN=/usr/bin/brave npx ng test --watch=false --browsers=ChromeHeadless`
- `npx tsc --noEmit -p tsconfig.app.json`
- Manual/visual: Settings transcription panel; `/video-transcribe` English/Spanish model status, run and result; dark/light.

## Progress

- 2026-11-17: whisper.cpp + multilingual base model installed and manually verified in English and Spanish.
- 2026-11-17: Slice scoped; backend first, then Settings/page/registry.

## Implementation notes

- Engine defaults should be cross-platform-safe: Linux `.local/share` install paths first, config dir second, PATH third. Frontend overrides win.
- Use the installed whisper sample for an optional real integration test; skip when the model/binary is absent. Never download in tests.
- Add `srt`/`vtt` to `inspect.kindFromExtension` as documents with text MIME types.
- The page reuses the standard plan/run/status/ResultCard pattern; no new job API.
- The Settings page passes the persisted whisper paths to the capabilities endpoint; empty values resolve defaults.
