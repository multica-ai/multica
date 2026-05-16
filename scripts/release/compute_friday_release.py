#!/usr/bin/env python3
"""Compute the Friday release: pick the next semver, generate release notes,
and honour the do-not-release PR label.

Reads from env, writes:
  - GITHUB_OUTPUT  — tag, prev_tag, bump, included_count, skipped_count
  - RELEASE_NOTES_FILE (default ./release-notes.md) — markdown body

Required env: GH_TOKEN (for the gh CLI).
Optional env:
  MULTICA_BASE_URL  default https://app.multica.ai
  ID_PREFIX         default JEH
  BUMP_OVERRIDE     auto|patch|minor|major (default auto)
  DRY_RUN           true|false — DRY_RUN only affects logging here.
                    The workflow gates the actual tag push.
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from collections import OrderedDict
from pathlib import Path

MULTICA_BASE_URL = os.environ.get("MULTICA_BASE_URL", "https://app.multica.ai").rstrip("/")
ID_PREFIX = os.environ.get("ID_PREFIX", "JEH")
BUMP_OVERRIDE = (os.environ.get("BUMP_OVERRIDE") or "auto").strip()
DRY_RUN = os.environ.get("DRY_RUN", "false").lower() == "true"
DO_NOT_RELEASE_LABEL = "do-not-release"

# Conventional commit type → display label, in render order.
TYPE_GROUPS: "OrderedDict[str, str]" = OrderedDict([
    ("feat", "New"),
    ("fix", "Fixed"),
    ("perf", "Improved"),
    ("refactor", "Improved"),
    ("docs", "Improved"),
])
OTHER_GROUP = "Other changes"
INTERNAL_NOTE_TYPES = {"test", "build", "ci", "chore"}

ID_PATTERN = re.compile(rf"\b({re.escape(ID_PREFIX)}-\d+)\b")
PR_PATTERN = re.compile(r"\(#(\d+)\)\s*$")
HEADER_PATTERN = re.compile(
    r"^(?P<type>[a-zA-Z]+)(?:\((?P<scope>[^)]+)\))?(?P<bang>!?):\s*(?P<subject>.*)$"
)
BREAKING_PATTERN = re.compile(r"BREAKING[ -]CHANGE", re.IGNORECASE)


def run(cmd: list[str]) -> str:
    return subprocess.check_output(cmd).decode().rstrip("\n")


def latest_tag() -> str | None:
    out = run([
        "git", "tag", "--list", "v[0-9]*.[0-9]*.[0-9]*",
        "--sort=-version:refname",
    ])
    tags = [t for t in out.splitlines() if t]
    return tags[0] if tags else None


def parse_version(tag: str) -> tuple[int, int, int]:
    core = tag.lstrip("v").split("-", 1)[0]
    parts = core.split(".")
    if len(parts) != 3:
        raise SystemExit(f"unrecognised tag format: {tag}")
    return int(parts[0]), int(parts[1]), int(parts[2])


def fetch_prs(since_iso: str) -> dict[int, dict]:
    out = run([
        "gh", "pr", "list",
        "--state", "merged",
        "--base", "main",
        "--limit", "500",
        "--search", f"merged:>={since_iso}",
        "--json", "number,title,labels,url,author",
    ])
    return {pr["number"]: pr for pr in json.loads(out)}


def commits_since(tag: str | None) -> list[dict]:
    rev = f"{tag}..HEAD" if tag else "HEAD"
    fmt = "%H%x09%s%x09%b%x1e"
    raw = run(["git", "log", rev, f"--pretty=format:{fmt}", "--no-merges"])
    commits: list[dict] = []
    for record in raw.split("\x1e"):
        record = record.strip("\n")
        if not record:
            continue
        sha, subject, body = record.split("\t", 2)
        commits.append({"sha": sha, "subject": subject, "body": body})
    return commits


def classify(subject: str, body: str) -> tuple[str, bool]:
    m = HEADER_PATTERN.match(subject)
    breaking = BREAKING_PATTERN.search(body) is not None
    if not m:
        return ("other", breaking or BREAKING_PATTERN.search(subject) is not None)
    type_ = m.group("type").lower()
    if m.group("bang") == "!":
        breaking = True
    return (type_, breaking)


def pr_number_from_subject(subject: str) -> int | None:
    m = PR_PATTERN.search(subject)
    return int(m.group(1)) if m else None


def link_ids(text: str) -> str:
    return ID_PATTERN.sub(
        lambda m: f"[{m.group(1)}]({MULTICA_BASE_URL}/search?q={m.group(1)})",
        text,
    )


def emit(key: str, value: str) -> None:
    print(f"{key}={value}", file=sys.stderr)
    out_path = os.environ.get("GITHUB_OUTPUT")
    if out_path:
        with open(out_path, "a") as fh:
            fh.write(f"{key}={value}\n")


def render_line(c: dict) -> str:
    subject = product_subject(c["subject"])
    subject = link_ids(subject)
    pr = c.get("pr")
    pr_md = f" [#{pr['number']}]({pr['url']})" if pr else ""
    return f"- {subject}{pr_md}"


def product_subject(subject: str) -> str:
    subject = PR_PATTERN.sub("", subject).rstrip()
    m = HEADER_PATTERN.match(subject)
    if m:
        subject = m.group("subject").strip()
    return subject[:1].upper() + subject[1:] if subject else subject


def main() -> int:
    last = latest_tag()
    print(f"Last tag: {last or '(none)'}", file=sys.stderr)
    if DRY_RUN:
        print("DRY_RUN=true (computation only)", file=sys.stderr)

    since_iso = (
        run(["git", "log", "-1", "--format=%aI", last]) if last else "1970-01-01T00:00:00Z"
    )

    prs = fetch_prs(since_iso)
    print(f"Found {len(prs)} merged PRs since {since_iso}", file=sys.stderr)

    raw_commits = commits_since(last)
    if not raw_commits:
        print("No commits since last tag — nothing to release.", file=sys.stderr)
        emit("tag", "")
        emit("prev_tag", last or "")
        emit("included_count", "0")
        emit("skipped_count", "0")
        return 0

    included: list[dict] = []
    skipped: list[dict] = []
    for c in raw_commits:
        pr_num = pr_number_from_subject(c["subject"])
        pr = prs.get(pr_num) if pr_num else None
        labels = {l["name"] for l in (pr or {}).get("labels", [])}
        type_, breaking = classify(c["subject"], c["body"])
        c.update({"pr": pr, "labels": labels, "type": type_, "breaking": breaking})
        if DO_NOT_RELEASE_LABEL in labels:
            skipped.append(c)
        else:
            included.append(c)

    if not included:
        print("All commits carry do-not-release — nothing to release.", file=sys.stderr)
        emit("tag", "")
        emit("prev_tag", last or "")
        emit("included_count", "0")
        emit("skipped_count", str(len(skipped)))
        return 0

    if BUMP_OVERRIDE != "auto":
        bump = BUMP_OVERRIDE
    else:
        bump = "patch"
        for c in included:
            if c["breaking"]:
                bump = "major"
                break
            if c["type"] == "feat":
                bump = "minor"

    major, minor, patch = parse_version(last) if last else (0, 0, 0)
    if bump == "major":
        major, minor, patch = major + 1, 0, 0
    elif bump == "minor":
        minor, patch = minor + 1, 0
    else:
        patch += 1
    new_tag = f"v{major}.{minor}.{patch}"

    breaking_section = [c for c in included if c["breaking"]]
    groups: dict[str, list[dict]] = OrderedDict()
    for label in TYPE_GROUPS.values():
        groups.setdefault(label, [])
    groups[OTHER_GROUP] = []
    for c in included:
        if c["type"] in INTERNAL_NOTE_TYPES and not c["breaking"]:
            continue
        label = TYPE_GROUPS.get(c["type"], OTHER_GROUP)
        groups[label].append(c)

    lines: list[str] = []
    lines.append(f"## {new_tag}")
    lines.append("")
    if last:
        compare = f"https://github.com/{os.environ.get('GITHUB_REPOSITORY', '')}/compare/{last}...{new_tag}"
        lines.append(f"_Changes since [{last}]({compare}) — bump: **{bump}**_")
    else:
        lines.append(f"_Initial release — bump: **{bump}**_")
    lines.append("")

    if breaking_section:
        lines.append("### Breaking changes")
        for c in breaking_section:
            lines.append(render_line(c))
        lines.append("")

    for label in groups:
        items = groups[label]
        if not items:
            continue
        lines.append(f"### {label}")
        for c in items:
            lines.append(render_line(c))
        lines.append("")

    if skipped:
        lines.append("### Held back (do-not-release)")
        lines.append("")
        lines.append("_These were merged to main for staging tests but are excluded from this release._")
        lines.append("")
        for c in skipped:
            lines.append(render_line(c))
        lines.append("")

    notes_path = os.environ.get("RELEASE_NOTES_FILE", "release-notes.md")
    Path(notes_path).write_text("\n".join(lines).rstrip() + "\n")
    print(f"Notes written to {notes_path}", file=sys.stderr)
    print("--- notes ---", file=sys.stderr)
    print("\n".join(lines), file=sys.stderr)
    print("--- /notes ---", file=sys.stderr)

    emit("tag", new_tag)
    emit("prev_tag", last or "")
    emit("bump", bump)
    emit("included_count", str(len(included)))
    emit("skipped_count", str(len(skipped)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
