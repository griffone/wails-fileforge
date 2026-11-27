"""FileForge local upscale bridge.

Runs an ONNX Real-ESRGAN style x4 model inside the managed upscale virtual
environment. The Go engine writes this file into a temporary workspace, starts
one interpreter for the whole batch so the model loads once, and publishes each
validated image.

The model is fully convolutional but must be fed a fixed square tile on the
pinned general model (128x128) and accepts dynamic tiles on most custom
exports. The bridge reads the input shape from the session: a fixed shape is
used as the tile, an empty dimension falls back to --tile. Tiles overlap and
each tile writes only its centre, so every output pixel is produced once and
the final size is exactly input x scale.

Every stdout line is a compact JSON event, flushed immediately:

    {"event":"progress","index":0,"total":2}
    {"event":"done","index":0,"output":"/tmp/.../out-0.png","width":512,"height":384}
    {"event":"error","index":1,"message":"..."}

Indices are 0-based positions in the input argument list. A failing image emits
an error event and the batch continues. Fatal start-up problems (missing model,
broken environment) go to stderr and exit non-zero before any event.

Usage:
    python upscale_bridge.py --model <path> --scale 2|4 --tile <px> \
        --format png|jpeg|webp [--quality 1-100] --out-dir <dir> \
        <input> [<input> ...]

No new Python dependency is introduced: onnxruntime, numpy and Pillow are the
pinned packages of the managed upscale environment.
"""

import argparse
import json
import os
import sys

import numpy as np

# Real-ESRGAN and its community ONNX exports are all x4 networks. A 2x request
# runs the model and downsamples the result, which keeps one artifact for both
# scales.
MODEL_SCALE = 4
DEFAULT_TILE = 128
MIN_OVERLAP = 8


def emit(event, **fields):
    payload = {"event": event}
    payload.update(fields)
    print(json.dumps(payload, separators=(",", ":")), flush=True)


def fail(message):
    print(message, file=sys.stderr, flush=True)
    return 2


def tile_geometry(tile, overlap):
    stride = tile - 2 * overlap
    if stride <= 0:
        raise ValueError("tile size is too small for the overlap")

    return stride


def load_session(model_path, threads):
    import onnxruntime as ort

    if not os.path.isfile(model_path) or os.path.getsize(model_path) == 0:
        raise RuntimeError("the upscale model file is missing or empty: " + model_path)

    options = ort.SessionOptions()
    if threads > 0:
        options.intra_op_num_threads = threads
    session = ort.InferenceSession(
        model_path,
        sess_options=options,
        providers=["CPUExecutionProvider"],
    )

    inputs = session.get_inputs()
    if len(inputs) != 1:
        raise RuntimeError("the upscale model must expose exactly one input")

    shape = list(inputs[0].shape)
    if len(shape) != 4:
        raise RuntimeError("the upscale model input must be NCHW, got " + str(shape))

    fixed = []
    for value in shape[2:]:
        if isinstance(value, int) and value > 0:
            fixed.append(value)
        else:
            fixed.append(0)

    return session, inputs[0].name, fixed


def upscale_tile(session, input_name, tile_rgb):
    """Runs one tile and returns a uint8 RGB array at MODEL_SCALE."""
    array = tile_rgb.astype(np.float32) / 255.0
    tensor = np.ascontiguousarray(array.transpose(2, 0, 1)[None])
    output = session.run(None, {input_name: tensor})[0]
    output = output[0].transpose(1, 2, 0)
    return np.clip(output * 255.0, 0, 255).round().astype(np.uint8)


def upscale_image(session, input_name, pixels, tile, overlap):
    """Upscales an RGB uint8 image with overlapping tiles."""
    height, width = pixels.shape[:2]
    stride = tile_geometry(tile, overlap)

    columns = max(1, -(-width // stride))
    rows = max(1, -(-height // stride))
    needed_width = columns * stride + 2 * overlap
    needed_height = rows * stride + 2 * overlap

    pad_width = max(0, needed_width - width)
    pad_height = max(0, needed_height - height)
    padded = np.pad(
        pixels,
        ((overlap, overlap + pad_height), (overlap, overlap + pad_width), (0, 0)),
        mode="edge",
    )

    ratio = MODEL_SCALE
    canvas = np.zeros((height * ratio, width * ratio, 3), dtype=np.uint8)
    centre = stride * ratio

    for row in range(rows):
        for column in range(columns):
            top = row * stride
            left = column * stride
            window = padded[top : top + tile, left : left + tile]

            output = upscale_tile(session, input_name, window)
            crop = output[overlap * ratio : overlap * ratio + centre, overlap * ratio : overlap * ratio + centre]

            destination_top = top * ratio
            destination_left = left * ratio
            visible_height = min(centre, height * ratio - destination_top)
            visible_width = min(centre, width * ratio - destination_left)
            if visible_height <= 0 or visible_width <= 0:
                continue

            canvas[
                destination_top : destination_top + visible_height,
                destination_left : destination_left + visible_width,
            ] = crop[:visible_height, :visible_width]

    return canvas


def finish_image(pixels, alpha, scale, format_name):
    from PIL import Image

    height, width = pixels.shape[:2]
    resized = Image.fromarray(pixels, mode="RGB")

    if scale == 2:
        # The canvas is exactly input x 4; halving it lands on input x 2.
        resized = resized.resize((width // 2, height // 2), Image.LANCZOS)

    if format_name == "jpeg" or alpha is None:
        return resized

    alpha_image = Image.fromarray(alpha, mode="L")
    alpha_image = alpha_image.resize(resized.size, Image.LANCZOS)
    resized = resized.convert("RGBA")
    resized.putalpha(alpha_image)

    return resized


def save_image(image, path, format_name, quality):
    from PIL import Image

    if format_name == "jpeg":
        if image.mode == "RGBA":
            background = Image.new("RGB", image.size, (255, 255, 255))
            background.paste(image, mask=image.getchannel("A"))
            image = background
        elif image.mode != "RGB":
            image = image.convert("RGB")
        image.save(path, format="JPEG", quality=quality, subsampling=0, optimize=True)
        return
    if format_name == "webp":
        image.save(path, format="WEBP", quality=quality, method=4)
        return

    image.save(path, format="PNG", optimize=True)


def main():
    parser = argparse.ArgumentParser(
        description="Upscale images locally with a managed ONNX x4 model."
    )
    parser.add_argument("--model", required=True)
    parser.add_argument("--scale", type=int, choices=(2, 4), default=4)
    parser.add_argument("--tile", type=int, default=DEFAULT_TILE)
    parser.add_argument("--format", default="png", choices=("png", "jpeg", "webp"))
    parser.add_argument("--quality", type=int, default=90)
    parser.add_argument("--threads", type=int, default=0)
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("inputs", nargs="+")
    args = parser.parse_args()

    try:
        from PIL import Image
        from PIL import ImageOps  # noqa: F401 - imported to fail early when Pillow is broken

        session, input_name, fixed = load_session(args.model, args.threads)
        os.makedirs(args.out_dir, exist_ok=True)
    except Exception as exc:  # pragma: no cover - reported through stderr
        return fail(str(exc) or exc.__class__.__name__)

    fixed_width, fixed_height = fixed
    if fixed_width and fixed_height:
        tile = fixed_width if fixed_width == fixed_height else min(fixed_width, fixed_height)
    else:
        tile = max(32, args.tile)

    overlap = max(MIN_OVERLAP, tile // 8)
    if overlap * 2 >= tile:
        overlap = max(1, tile // 4)

    total = len(args.inputs)
    for index, source in enumerate(args.inputs):
        try:
            with Image.open(source) as opened:
                oriented = ImageOps.exif_transpose(opened)
                alpha = None
                has_alpha = oriented.mode in ("RGBA", "LA") or (
                    oriented.mode == "P" and "transparency" in oriented.info
                )
                if has_alpha and args.format == "jpeg":
                    # JPEG has no alpha channel: flatten onto white before the
                    # model so transparent pixels cannot keep stale colours.
                    rgba = oriented.convert("RGBA")
                    flat = Image.new("RGB", rgba.size, (255, 255, 255))
                    flat.paste(rgba, mask=rgba.getchannel("A"))
                    pixels = np.array(flat, dtype=np.uint8)
                elif has_alpha:
                    rgba = oriented.convert("RGBA")
                    alpha = np.array(rgba.getchannel("A"), dtype=np.uint8)
                    pixels = np.array(rgba.convert("RGB"), dtype=np.uint8)
                else:
                    pixels = np.array(oriented.convert("RGB"), dtype=np.uint8)

            upscaled = upscale_image(session, input_name, pixels, tile, overlap)
            image = finish_image(upscaled, alpha, args.scale, args.format)

            extension = "jpg" if args.format == "jpeg" else args.format
            output = os.path.join(args.out_dir, "out-%d.%s" % (index, extension))
            save_image(image, output, args.format, args.quality)
            width, height = image.size
            emit("done", index=index, output=output, width=width, height=height)
        except Exception as exc:
            emit("error", index=index, message=str(exc) or exc.__class__.__name__)
        finally:
            emit("progress", index=index, total=total)

    return 0


if __name__ == "__main__":
    sys.exit(main())
