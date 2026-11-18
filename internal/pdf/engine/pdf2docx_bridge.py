"""FileForge PDF to DOCX bridge.

Runs pdf2docx in the managed virtual environment. pdf2docx logs page progress
through the standard logging module; the Go engine turns those lines into
progress events while the command streams.

Usage: python pdf2docx_bridge.py <input.pdf> <output.docx>
"""

import logging
import sys
import traceback


def main():
    if len(sys.argv) != 3:
        print(
            "usage: pdf2docx_bridge.py <input.pdf> <output.docx>",
            file=sys.stderr,
        )
        return 2

    # pdf2docx reports "Parsing pages"/"Creating pages" plus "(n/total) Page n"
    # through logging. Send those lines to stdout so the engine can stream them.
    logging.basicConfig(level=logging.INFO, format="%(message)s", stream=sys.stdout)

    source, destination = sys.argv[1], sys.argv[2]
    converter = None
    try:
        from pdf2docx import Converter

        converter = Converter(source)
        converter.convert(destination)
    except Exception:
        traceback.print_exc()
        return 1
    finally:
        if converter is not None:
            try:
                converter.close()
            except Exception:
                pass

    print("DONE")
    return 0


if __name__ == "__main__":
    sys.exit(main())
