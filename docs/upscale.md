# Local upscale pack

`tool.image.upscale` enlarges images 2x or 4x on the user's machine. It is an
optional pack: nothing is downloaded until the user opens **Settings → Tools &
Models** and runs **Install / repair**, and no weights are bundled with the
installers.

## What the pack contains

| Component | Contents | Size |
| --- | --- | --- |
| Upscale engine | `upscale-venv` with `onnxruntime==1.32.0`, `numpy==2.3.5` and `pillow==12.3.0` | about 50 MB download |
| General model | `realesr-general-x4v3` ONNX graph + external weights | 4.5 MB download |
| Custom anime model | user-provided `.onnx` file | not downloaded |

A run uses the managed interpreter, the embedded `upscale_bridge.py` and the
pinned model. The bridge starts once per batch so the model loads once. It
reports JSON lines on stdout; the Go engine validates every output before
publishing it atomically, and a failed or cancelled job removes everything it
published.

## Pinned model decision

The shipped model is the **Real-ESRGAN general x4v3** network exported to ONNX
by **Qualcomm AI Hub Models v0.67.0**. It was chosen over the larger
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
  upstream licence. This repository ships no weights; the user downloads them
  after consent.
- **Integrity:** the installer verifies the archive SHA-256 and byte size, then
  verifies the extracted graph and external weights by SHA-256 and size.

Pinned values, captured on 2026-11-27:

| Artifact | Bytes | SHA-256 |
| --- | --- | --- |
| `real_esrgan_general_x4v3-onnx-float.zip` | 4,541,448 | `4f3619c89eb0727ff9170b977578d160fa4d696680a0cac4c7f870b52973033b` |
| `real_esrgan_general_x4v3.onnx` | 158,541 | `ca44c913c9669c5e656d25bc900da6856e828b98422c17f31ddec69d99b331a0` |
| `real_esrgan_general_x4v3.data` | 4,836,096 | `512d0ec9940c2e9d85d27f2952f12a0b77b7841dc22df4ce9f3ea458bc98f37f` |

The values live in `internal/toolchain/platform.go`; change them there and in
this table together.

## The anime option

No trustworthy anime ONNX artifact with a clearly verified licence could be
pinned, so FileForge does **not** ship or download one. The `anime` model choice
uses a file the user provides:

1. Obtain a Real-ESRGAN x4 anime ONNX export. The upstream weights are
   `RealESRGAN_x4plus_anime_6B` from the Real-ESRGAN project (BSD-3-Clause
   code; check the weight terms yourself before redistributing anything).
2. Export or download a self-contained `.onnx` file with a 4x output.
3. Choose it in **Settings → Tools & Models → Upscale pack → Anime model file**.
4. Select **Anime (custom file)** on the tool page.

The bridge reads the model input shape: a fixed square shape is used as the
tile, a dynamic shape uses the `tileSize` option. Custom models must accept
RGB NCHW `float32` input in the 0–1 range, as Real-ESRGAN exports do.

## Behaviour and limits

- Output dimensions are always `input × scale`; the engine rejects any file
  whose decoded dimensions or format differ, and reports the achieved size.
- Output formats are PNG (lossless, alpha preserved), JPEG and WebP. Quality
  applies to JPEG/WebP. 2x output is a Lanczos downsample of the 4x result.
- The bundled model runs on fixed 128 px tiles. `tileSize` (64–512 px) applies
  to custom models with dynamic input shapes. Tiles overlap and only their
  centre is written, so the result has no doubled edges and the exact size is
  preserved.
- Outputs are capped at 64 megapixels in memory. A 4x pass therefore starts
  from an image under 4000 × 4000 px; larger requests plan a validation error
  instead of exhausting memory.
- Animated inputs use the first frame.
- Inference is CPU-only through `CPUExecutionProvider`. GPU providers can be
  added later without changing the pack layout.

## Verification

- `internal/image/upscale_test.go` covers option validation, missing runtime
  and model, collision naming, batch planning, the bridge argv contract,
  bounded stderr, cancellation and process-tree cleanup, and dimension/format
  verification with a stub process.
- `internal/image/upscale_real_test.go` runs one real 2x inference when
  `FILEFORGE_UPSCALE_TEST_PYTHON` and `FILEFORGE_UPSCALE_TEST_MODEL` are set;
  otherwise it skips.

To run the real test locally after installing the pack:

```bash
FILEFORGE_UPSCALE_TEST_PYTHON="$HOME/.local/share/fileforge/toolchain/upscale-venv/bin/python" \
FILEFORGE_UPSCALE_TEST_MODEL="$HOME/.local/share/fileforge/toolchain/upscale/models/general/real_esrgan_general_x4v3.onnx" \
go test ./internal/image -run TestUpscaleRealModel -count=1 -v
```
