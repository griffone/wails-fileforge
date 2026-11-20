#!/usr/bin/env python3
"""Copy only the libvips DLLs reachable from the staged Windows executable.

`scripts/build-windows.sh` stages the application executable and the full
libvips Windows runtime next to it. Only a subset of that runtime is imported
by the executable and its transitive dependencies. The optional libvips
modules (JXL, ImageMagick, Poppler, OpenSlide) pull in large libraries the app
never loads, and `vips-modules-*` directories are not imported at startup.

This script parses the PE import tables with objdump, walks the dependency
closure from the `.exe` files in the destination, and copies only the reachable
DLLs from the libvips `bin` directory. DLLs provided by Windows are ignored.

Usage:
    prune-windows-dlls.py --source <vips bin dir> --dest <stage dir>

The destination must already contain the executable. Unreachable top-level
DLLs already present in the destination are removed, so re-running the script
against a dirty stage still produces a pruned bundle.
"""

from __future__ import annotations

import argparse
import re
import shutil
import subprocess
import sys
from collections import deque
from pathlib import Path

IMPORT_RE = re.compile(r"^\s*DLL Name:\s*(\S+)\s*$", re.IGNORECASE | re.MULTILINE)

# DLLs provided by Windows itself. They never ship in the libvips package and
# must not be copied into the bundle.
SYSTEM_DLLS = frozenset(
    name.lower()
    for name in (
        "advapi32.dll",
        "bcrypt.dll",
        "bcryptprimitives.dll",
        "comctl32.dll",
        "comdlg32.dll",
        "crypt32.dll",
        "d3d11.dll",
        "dbghelp.dll",
        "dnsapi.dll",
        "dwmapi.dll",
        "dxgi.dll",
        "gdi32.dll",
        "gdiplus.dll",
        "glu32.dll",
        "imm32.dll",
        "iphlpapi.dll",
        "kernel32.dll",
        "mpr.dll",
        "msimg32.dll",
        "msvcrt.dll",
        "netapi32.dll",
        "ntdll.dll",
        "odbc32.dll",
        "ole32.dll",
        "oleacc.dll",
        "oleaut32.dll",
        "opengl32.dll",
        "powrprof.dll",
        "propsys.dll",
        "psapi.dll",
        "rpcrt4.dll",
        "sechost.dll",
        "secur32.dll",
        "setupapi.dll",
        "shcore.dll",
        "shell32.dll",
        "shlwapi.dll",
        "ucrtbase.dll",
        "user32.dll",
        "userenv.dll",
        "uxtheme.dll",
        "version.dll",
        "winhttp.dll",
        "winmm.dll",
        "winspool.drv",
        "wintrust.dll",
        "ws2_32.dll",
        "wtsapi32.dll",
    )
)

SYSTEM_PREFIXES = ("api-ms-win-", "ext-ms-win-")


def fail(message: str) -> None:
    print(f"error: {message}", file=sys.stderr)
    raise SystemExit(1)


def human_bytes(size: int) -> str:
    return f"{size / (1024 * 1024):.1f} MiB"


def is_system_dll(name: str) -> bool:
    lowered = name.lower()
    return lowered in SYSTEM_DLLS or lowered.startswith(SYSTEM_PREFIXES)


def find_objdump() -> str:
    for candidate in ("x86_64-w64-mingw32-objdump", "objdump"):
        path = shutil.which(candidate)
        if path:
            return path
    fail("objdump not found; install binutils-mingw-w64-x86-64")


def read_imports(objdump: str, binary: Path) -> list[str]:
    result = subprocess.run(
        [objdump, "-p", str(binary)],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or f"exit status {result.returncode}"
        fail(f"objdump failed for {binary}: {detail}")
    imports = IMPORT_RE.findall(result.stdout)
    if not imports:
        fail(f"no PE import table found in {binary}")
    return imports


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, help="libvips bin directory")
    parser.add_argument("--dest", required=True, help="staging directory with the .exe")
    args = parser.parse_args()

    source = Path(args.source)
    dest = Path(args.dest)
    if not source.is_dir():
        fail(f"source directory not found: {source}")
    if not dest.is_dir():
        fail(f"destination directory not found: {dest}")

    executables = sorted(dest.glob("*.exe"))
    if not executables:
        fail(f"no .exe found in {dest}")

    objdump = find_objdump()
    available = {
        path.name.lower(): path
        for path in source.glob("*.dll")
        if path.is_file() and not path.name.lower().startswith("vips-modules-")
    }
    if not available:
        fail(f"no DLLs found in {source}")

    reachable: dict[str, Path] = {}
    unresolved: dict[str, set[str]] = {}
    visited: set[str] = set()
    queue: deque[tuple[str, str]] = deque()

    for executable in executables:
        for dll_name in read_imports(objdump, executable):
            queue.append((dll_name, executable.name))

    while queue:
        dll_name, required_by = queue.popleft()
        key = dll_name.lower()
        if key in visited:
            continue
        visited.add(key)

        dll_path = available.get(key)
        if dll_path is None:
            if not is_system_dll(dll_name):
                unresolved.setdefault(dll_name, set()).add(required_by)
            continue

        reachable[key] = dll_path
        for dependency in read_imports(objdump, dll_path):
            queue.append((dependency, dll_path.name))

    if unresolved:
        lines = [
            f"  {name} (required by {', '.join(sorted(requirers))})"
            for name, requirers in sorted(unresolved.items())
        ]
        fail(
            "reachable imports missing from the source directory:\n"
            + "\n".join(lines)
        )

    kept_bytes = 0
    for dll_path in sorted(reachable.values(), key=lambda path: path.name.lower()):
        shutil.copy2(dll_path, dest / dll_path.name)
        kept_bytes += dll_path.stat().st_size

    removed: list[str] = []
    for dll_path in sorted(dest.glob("*.dll")):
        if dll_path.name.lower() not in reachable:
            dll_path.unlink()
            removed.append(dll_path.name)

    excluded = [path for key, path in available.items() if key not in reachable]
    excluded_bytes = sum(path.stat().st_size for path in excluded)

    print(f"kept {len(reachable)} DLLs ({human_bytes(kept_bytes)})")
    print(f"excluded {len(excluded)} unreachable DLLs ({human_bytes(excluded_bytes)})")
    if excluded:
        names = ", ".join(sorted(path.name for path in excluded))
        print(f"excluded: {names}")
    if removed:
        print(f"removed {len(removed)} stale DLLs from the stage")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
