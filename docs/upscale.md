# Local upscale engine and models

`tool.image.upscale` enlarges images 2x or 4x on the user's machine. The
engine and its two pinned models install with the rest of the managed
toolchain: the first-run setup downloads them together with ffmpeg, tesseract,
poppler, ghostscript, Python and the Whisper model. No weights are bundled with
the installers, and **Settings → Tools & Models → Install / repair** restores
any component that is missing.

## What the pack contains

| Component | Contents | Size |
| --- | --- | --- |
| Upscale engine | the shared `onnx-venv` ONNX environment (see below) | shared with background removal: about 50 MB of packages |
| Photo model | `realesr-general-x4v3` ONNX graph + external weights | 4.5 MB download |
| Illustration model | `realesrgan_x4plus_anime_6b.onnx` | 17.9 MB download |

The two model downloads total about **22.5 MB**; a complete first install
downloads about **73 MB** including the engine packages, of which the engine is
shared with the background-removal feature.

A run uses the shared interpreter, the embedded `upscale_bridge.py` and one of
the pinned models. The bridge starts once per batch so the model loads once. It
reports JSON lines on stdout; the Go engine validates every output before
publishing it atomically, and a failed or cancelled job removes everything it
published.

## Shared ONNX environment

The upscale engine imports `onnxruntime==1.32.0`, `numpy==2.3.5` and
`pillow==12.3.0`. Those exact versions are also part of the background-removal
pin set (`rembg[cpu]==2.0.87` plus its transitive pins), so one virtual
environment, `onnx-venv`, serves both engines instead of duplicating 150–200 MB.
The installer installs the background-removal set (a superset) once, verifies a
single import of `rembg, onnxruntime, numpy, PIL`, and writes the
`onnx-venv.ready` marker. The two engines keep separate components and separate
weight folders; only the interpreter and packages are shared.

## Pinned models

### Photo model

The photo model is the **Real-ESRGAN general x4v3** network exported to ONNX by
**Qualcomm AI Hub Models v0.67.0**. It was chosen over the larger
`RealESRGAN_x4plus` export because it is a tenth of the size and roughly eight
times faster on CPU while remaining a genuine Real-ESRGAN model. It is still a
4x network; a 2x request runs the same model and downsamples the result to
exactly `input × 2`.

Why this artifact is pinnable:

- **Distributor:** the Qualcomm AI Hub Models organisation publishes the release
  under a versioned path (`v0.67.0`) on `qaihub-public-assets.s3.us-west-2.amazonaws.com`,
  with a matching Hugging Face model card at
  <https://huggingface.co/qualcomm/Real-ESRGAN-General-x4v3>.
- **Licence:** the upstream Real-ESRGAN repository is BSD-3-Clause
  (<https://github.com/xinntao/Real-ESRGAN/blob/master/LICENSE>) and the
  Qualcomm model card declares `license: bsd-3-clause` and points at that
  upstream licence. This repository ships no weights; the toolchain downloads
  them after consent.
- **Integrity:** the installer verifies the archive SHA-256 and byte size, then
  verifies the extracted graph and external weights by SHA-256 and size.

Pinned values, captured on 2026-11-27:

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| `real_esrgan_general_x4v3-onnx-float.zip` | 4,541,448 | `4f3619c89eb0727ff9170b977578d160fa4d696680a0cac4c7f870b52973033b` |
| `real_esrgan_general_x4v3.onnx` | 158,541 | `ca44c913c9669c5e656d25bc900da6856e828b98422c17f31ddec69d99b331a0` |
| `real_esrgan_general_x4v3.data` | 4,836,096 | `512d0ec9940c2e9d85d27f2952f12a0b77b7841dc22df4ce9f3ea458bc98f37f` |

### Illustration model

The illustration model (the `anime` option value) is the
**RealESRGAN_x4plus_anime_6B** network. It is tuned for illustrated content:
hard lines, flat colour, logos, comics, screenshots and digital art. Anime is
one example of that content, not the only one; the UI labels the choice
"Illustration & line art".

- **Weights:** from the Real-ESRGAN project
  (`RealESRGAN_x4plus_anime_6B`), BSD-3-Clause
  (<https://github.com/xinntao/Real-ESRGAN/blob/master/LICENSE>), the same
  licence as the photo model.
- **ONNX export:** published by `skillsafe-ai` on Hugging Face and pinned at
  commit `c001be50fae1c7a2bae8b2b4dec5e6fcbdbcca96`:
  <https://huggingface.co/skillsafe-ai/realesrgan-x4plus-anime-6b/resolve/c001be50fae1c7a2bae8b2b4dec5e6fcbdbcca96/model.onnx>
- **Shape:** a self-contained graph with a dynamic input
  (`['batch', 3, 'height', 'width']`) and a 4x output
  (`['batch', 3, 'height_x4', 'width_x4']`), so it has no fixed 128×128
  requirement and the bridge tiles it with the requested `tileSize`.

Pinned values, captured on 2026-11-27:

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| `realesrgan_x4plus_anime_6b.onnx` | 17,939,975 | `9a029dae374b2cd4c3eb2dbbed857e22e6d785687c767aa12828e7356b9fb378` |

Each model keeps its own URL, size and checksum, so updating one never
invalidates the other download. The values live in
`internal/toolchain/platform.go`; change them there and in these tables
together. A checksum mismatch rejects that model and keeps the previous files.

## Advanced: custom model override

The `anime` choice can be pointed at your own `.onnx` file for advanced use.
When a custom path is set in **Settings → Tools & Models → Upscale engine and
models → Illustration model (advanced override)**, that file wins and the
bundled illustration model is not required. Custom models must accept RGB NCHW
`float32` input in the 0–1 range and produce a 4x output, as Real-ESRGAN exports
do.

The override is never required: `model=anime` works after the normal install
with no file picker.

## Behaviour and limits

- Output dimensions are always `input × scale`; the engine rejects any file
  whose decoded dimensions or format differ, and reports the achieved size.
- Output formats are PNG (lossless, alpha preserved), JPEG and WebP. Quality
  applies to JPEG/WebP. 2x output is a Lanczos downsample of the 4x result.
- The photo model runs on fixed 128 px tiles. `tileSize` (64–512 px) applies to
  the illustration model and to custom models with dynamic input shapes. Tiles
  overlap and only their centre is written, so the result has no doubled edges
  and the exact size is preserved.
- Outputs are capped at 64 megapixels in memory. A 4x pass therefore starts
  from an image under 4000 × 4000 px; larger requests plan a validation error
  instead of exhausting memory.
- Animated inputs use the first frame.
- Inference is CPU-only through `CPUExecutionProvider`. GPU providers can be
  added later without changing the layout.

## Failure and repair

Each component is independent. A failed engine or model download is reported
against that component (Settings shows the error, the upscale page offers the
same Install / repair action inline) and does not block the rest of the
toolchain or the app. The previous model files are kept on any failure. The
upscale page also offers the install inline whenever the engine or a model is
missing, with progress and cancellation.

## Verification

- `internal/toolchain/upscale_test.go` pins both models, both checksums and the
  shared environment, and covers per-component failures, the install-by-default
  set and the survived-model guarantee.
- `internal/image/upscale_test.go` covers option validation, missing runtime
  and model, the illustration model without a user file, the custom override,
  collision naming, batch planning, the bridge argv contract, bounded stderr,
  cancellation and process-tree cleanup, and dimension/format verification with
  a stub process.
- `internal/image/upscale_real_test.go` runs one real 2x inference when
  `FILEFORGE_UPSCALE_TEST_PYTHON` and `FILEFORGE_UPSCALE_TEST_MODEL` are set;
  otherwise it skips.

To run the real test locally after installing the pack:

```bash
FILEFORGE_UPSCALE_TEST_PYTHON="$HOME/.local/share/fileforge/toolchain/onnx-venv/bin/python" \
FILEFORGE_UPSCALE_TEST_MODEL="$HOME/.local/share/fileforge/toolchain/upscale/models/general/real_esrgan_general_x4v3.onnx" \
go test ./internal/image -run TestUpscaleRealModel -count=1 -v
```

To run the same test against the illustration model, point
`FILEFORGE_UPSCALE_TEST_MODEL` at
`$HOME/.local/share/fileforge/toolchain/upscale/models/anime/realesrgan_x4plus_anime_6b.onnx`.
