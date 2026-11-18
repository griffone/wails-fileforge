"""FileForge background removal bridge.

Runs rembg inside the managed background removal virtual environment. The Go
engine writes this file into a temporary workspace, starts one interpreter for
the whole batch so the model loads once, and publishes each validated PNG.

Every stdout line is a compact JSON event, flushed immediately:

    {"event":"progress","index":0,"total":2}
    {"event":"done","index":0,"output":"/tmp/.../out-0.png","width":1024,"height":768}
    {"event":"error","index":1,"message":"..."}

Indices are 0-based positions in the input argument list. A failing image emits
an error event and the batch continues. Fatal start-up problems (missing model,
broken environment) go to stderr and exit non-zero before any event.

Usage:
    python rembg_bridge.py --model <model> --out-dir <dir> \
        [--background '#RRGGBB'] <input> [<input> ...]

No new Python dependency is introduced: Pillow ships with rembg and the rest is
the standard library.
"""

import argparse
import io
import json
import os
import sys


def emit(event, **fields):
    payload = {"event": event}
    payload.update(fields)
    print(json.dumps(payload, separators=(",", ":")), flush=True)


def parse_background(value):
    text = (value or "").strip()
    if not text:
        return None
    if len(text) != 7 or not text.startswith("#"):
        raise ValueError("background must use the #RRGGBB format")
    try:
        red = int(text[1:3], 16)
        green = int(text[3:5], 16)
        blue = int(text[5:7], 16)
    except ValueError:
        raise ValueError("background must use the #RRGGBB format")
    return (red, green, blue)


def resolve_model_path(model):
    # U2NET_HOME takes precedence inside rembg; both variables point at the
    # managed home. Verifying the file here keeps pooch from attempting a
    # network download when the pack is incomplete.
    home = (os.environ.get("U2NET_HOME") or os.environ.get("REMBG_HOME") or "").strip()
    if not home:
        raise RuntimeError("REMBG_HOME and U2NET_HOME are not set")
    model_path = os.path.join(home, "models", model, model + ".onnx")
    if not os.path.isfile(model_path) or os.path.getsize(model_path) == 0:
        raise RuntimeError("the managed model file is missing or empty: " + model_path)
    return model_path


def main():
    parser = argparse.ArgumentParser(
        description="Remove image backgrounds locally with a managed rembg model."
    )
    parser.add_argument("--model", required=True)
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("--background", default="")
    parser.add_argument("inputs", nargs="+")
    args = parser.parse_args()

    try:
        background = parse_background(args.background)
        resolve_model_path(args.model)
        from PIL import Image
        from rembg import new_session, remove
    except Exception as exc:  # pragma: no cover - reported through stderr
        print(str(exc) or exc.__class__.__name__, file=sys.stderr, flush=True)
        return 2

    try:
        os.makedirs(args.out_dir, exist_ok=True)
    except Exception as exc:  # pragma: no cover - reported through stderr
        print("could not create the output directory: %s" % exc, file=sys.stderr, flush=True)
        return 2

    try:
        # Always pass the model explicitly: rembg's default is bria-rmbg, whose
        # weights are not licensed for this use. CPU is the supported runtime.
        session = new_session(args.model, providers=["CPUExecutionProvider"])
    except Exception as exc:  # pragma: no cover - reported through stderr
        print("could not start rembg: %s" % exc, file=sys.stderr, flush=True)
        return 1

    total = len(args.inputs)
    for index, source in enumerate(args.inputs):
        try:
            with open(source, "rb") as handle:
                data = handle.read()

            # decontaminate=True is the validated quality setting for this pack.
            cutout = remove(data, session=session, decontaminate=True)
            image = Image.open(io.BytesIO(cutout)).convert("RGBA")

            if background is not None:
                canvas = Image.new("RGBA", image.size, background + (255,))
                canvas.alpha_composite(image)
                image = canvas.convert("RGB")

            output = os.path.join(args.out_dir, "out-%d.png" % index)
            image.save(output, format="PNG")
            width, height = image.size
            emit("done", index=index, output=output, width=width, height=height)
        except Exception as exc:
            emit("error", index=index, message=str(exc) or exc.__class__.__name__)
        finally:
            emit("progress", index=index, total=total)

    return 0


if __name__ == "__main__":
    sys.exit(main())
