#!/usr/bin/env python3
"""Copy skills (and their files) from one multica workspace to another.

Reads via the `multica` CLI (so it respects whatever auth is configured in
~/.multica/config.json) and writes back via the same CLI. Per-command
workspace targeting uses the `MULTICA_WORKSPACE_ID` env var override so
the user's global config is never mutated.

Usage:
    scripts/transfer-skills.py --src <src-ws-id> --dst <dst-ws-id>
    scripts/transfer-skills.py --src <src> --dst <dst> --names art,CleverTap
    scripts/transfer-skills.py --src <src> --dst <dst> --overwrite
    scripts/transfer-skills.py --src <src> --dst <dst> --dry-run

Behavior:
    - By default, skills whose name already exists in the destination are
      SKIPPED (safe). Use --overwrite to delete + recreate them in place.
    - --names limits the transfer to the comma-separated list of skill names.
    - --dry-run prints the plan without making any writes.

Exit codes:
    0  all targeted skills processed (created/skipped/overwritten)
    1  one or more skills failed
    2  invalid arguments / setup error
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from typing import Any


def run_cli(args: list[str], workspace_id: str | None) -> str:
    """Run a `multica` command, optionally targeted at a specific workspace."""
    env = os.environ.copy()
    if workspace_id:
        env["MULTICA_WORKSPACE_ID"] = workspace_id
    p = subprocess.run(args, capture_output=True, text=True, env=env, check=False)
    if p.returncode != 0:
        raise RuntimeError(
            f"command failed (rc={p.returncode}): {' '.join(args)}\n"
            f"stdout: {p.stdout}\nstderr: {p.stderr}"
        )
    return p.stdout


def jload(text: str) -> Any:
    """Tolerant JSON loader — multica CLI sometimes emits raw newlines in strings."""
    return json.loads(text, strict=False)


def list_skills(ws: str) -> list[dict]:
    return jload(run_cli(["multica", "skill", "list", "--output", "json"], ws))


def get_skill(ws: str, sid: str) -> dict:
    return jload(run_cli(["multica", "skill", "get", sid, "--output", "json"], ws))


def list_files(ws: str, sid: str) -> list[dict]:
    return jload(
        run_cli(["multica", "skill", "files", "list", sid, "--output", "json"], ws)
    )


def create_skill(
    ws: str, name: str, description: str, content: str, config: Any
) -> dict:
    args = [
        "multica", "skill", "create",
        "--name", name,
        "--description", description or "",
        "--content", content or "",
        "--output", "json",
    ]
    if config:
        args += ["--config", json.dumps(config)]
    return jload(run_cli(args, ws))


def upsert_file(ws: str, sid: str, path: str, content: str) -> None:
    run_cli([
        "multica", "skill", "files", "upsert", sid,
        "--path", path,
        "--content", content,
        "--output", "json",
    ], ws)


def delete_skill(ws: str, sid: str) -> None:
    run_cli(["multica", "skill", "delete", sid, "--yes"], ws)


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Copy skills + files from one multica workspace to another.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog=__doc__.split("Usage:")[1].split("Behavior")[0],
    )
    p.add_argument("--src", required=True, help="source workspace id")
    p.add_argument("--dst", required=True, help="destination workspace id")
    p.add_argument(
        "--names",
        default="",
        help="comma-separated skill names to transfer (default: all)",
    )
    p.add_argument(
        "--overwrite",
        action="store_true",
        help="delete + recreate skills in destination if name conflicts (default: skip)",
    )
    p.add_argument(
        "--dry-run",
        action="store_true",
        help="print the plan without writing anything",
    )
    return p.parse_args()


def main() -> int:
    args = parse_args()
    if args.src == args.dst:
        print("error: --src and --dst must differ", file=sys.stderr)
        return 2

    name_filter = {n.strip() for n in args.names.split(",") if n.strip()}

    print(f"source:      {args.src}")
    print(f"destination: {args.dst}")
    if name_filter:
        print(f"filter:      {sorted(name_filter)}")
    if args.overwrite:
        print("mode:        OVERWRITE conflicts")
    if args.dry_run:
        print("DRY RUN — no changes will be made")
    print()

    src_skills = list_skills(args.src)
    print(f"source has {len(src_skills)} skills")
    dst_skills = list_skills(args.dst)
    dst_by_name = {s["name"]: s for s in dst_skills}
    print(f"destination has {len(dst_skills)} skills\n")

    targeted = [
        s for s in src_skills if not name_filter or s["name"] in name_filter
    ]
    if name_filter:
        missing = name_filter - {s["name"] for s in targeted}
        for m in sorted(missing):
            print(f"  ! filter: '{m}' not found in source, ignoring")

    created: list[tuple[str, str, int]] = []
    overwritten: list[tuple[str, str, int]] = []
    skipped: list[tuple[str, str]] = []
    failed: list[tuple[str, str]] = []

    for s in targeted:
        name = s["name"]
        try:
            full = get_skill(args.src, s["id"])
            files = list_files(args.src, s["id"])

            if name in dst_by_name:
                if not args.overwrite:
                    skipped.append((name, "name exists in destination (no --overwrite)"))
                    print(f"  - {name}: skipped (already in destination)")
                    continue
                old_id = dst_by_name[name]["id"]
                if args.dry_run:
                    print(f"  ~ {name}: would delete dest id={old_id[:8]}... and recreate ({len(files)} files)")
                    overwritten.append((name, "(dry-run)", len(files)))
                    continue
                delete_skill(args.dst, old_id)

            if args.dry_run:
                print(f"  + {name}: would create ({len(files)} files)")
                created.append((name, "(dry-run)", len(files)))
                continue

            new_skill = create_skill(
                args.dst,
                name=full["name"],
                description=full.get("description", ""),
                content=full.get("content", ""),
                config=full.get("config"),
            )
            new_id = new_skill["id"]
            for f in files:
                upsert_file(args.dst, new_id, f["path"], f["content"])

            verb = "overwrote" if name in dst_by_name else "created"
            (overwritten if verb == "overwrote" else created).append(
                (name, new_id, len(files))
            )
            print(f"  ✓ {name}: {verb} (id={new_id[:8]}..., {len(files)} files)")
        except Exception as e:
            failed.append((name, str(e)))
            print(f"  ✗ {name}: FAILED — {e}", file=sys.stderr)

    print("\n=== summary ===")
    print(f"created:     {len(created)}")
    print(f"overwritten: {len(overwritten)}")
    print(f"skipped:     {len(skipped)}")
    print(f"failed:      {len(failed)}")
    if skipped:
        print("\nskipped skills (use --overwrite to replace):")
        for n, why in skipped:
            print(f"  - {n}  ({why})")
    if failed:
        print("\nfailed skills:")
        for n, why in failed:
            print(f"  ! {n}  ({why[:160]})")

    return 1 if failed else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except RuntimeError as e:
        print(f"\nfatal: {e}", file=sys.stderr)
        sys.exit(2)
