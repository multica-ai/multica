#!/usr/bin/env python3
"""Size-based log rotator for launchd-managed services.

Reads stdin, appends to <path>, rotates when the file crosses --max-bytes.
Keeps up to --keep numbered backups (foo.log.1 .. foo.log.N); older drops off.

Also rotates on startup if the existing file is already over the threshold —
this is how a stale multi-GB log gets archived to .1 on the next service
restart without manual intervention.

Used by .deploy/run-*.sh; not invoked directly. See .deploy/README.md.
"""
import argparse
import os
import sys


def rotate(path: str, keep: int) -> None:
    for i in range(keep - 1, 0, -1):
        src = f"{path}.{i}"
        if os.path.exists(src):
            os.replace(src, f"{path}.{i + 1}")
    if os.path.exists(path):
        os.replace(path, f"{path}.1")
    dropped = f"{path}.{keep + 1}"
    if os.path.exists(dropped):
        os.remove(dropped)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("path", help="Target log file (rotates to <path>.1 .. <path>.N)")
    ap.add_argument("--max-bytes", type=int, default=100 * 1024 * 1024,
                    help="Rotate when file reaches this size (default: 100 MB)")
    ap.add_argument("--keep", type=int, default=7,
                    help="Number of rotated generations to keep (default: 7)")
    args = ap.parse_args()

    os.makedirs(os.path.dirname(args.path) or ".", exist_ok=True)

    if os.path.exists(args.path) and os.path.getsize(args.path) >= args.max_bytes:
        rotate(args.path, args.keep)

    f = open(args.path, "ab", buffering=0)
    size = f.tell()

    try:
        for line in sys.stdin.buffer:
            f.write(line)
            size += len(line)
            if size >= args.max_bytes:
                f.close()
                rotate(args.path, args.keep)
                f = open(args.path, "ab", buffering=0)
                size = 0
    except KeyboardInterrupt:
        pass
    finally:
        f.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
