#!/usr/bin/env python3
"""Render GitHub release notes into Cerebro docs MDX."""
from __future__ import annotations

import argparse
import re
from datetime import UTC, datetime
from pathlib import Path

HEADING_PATTERN = re.compile(r"^###\s+(?P<title>.+?)\s*$")
BULLET_PATTERN = re.compile(r"^- ")
INDEX_ENTRY_PATTERN = re.compile(
    r"^- \[\*\*(?P<tag>v\d+\.\d+\.\d+)\*\* - (?P<date>[^]]+)\]\([^)]+\) - .*$"
)


def yaml_quote(value: str) -> str:
    return '"' + value.replace("\\", "\\\\").replace('"', '\\"') + '"'


# MDX treats `<` and `{` in prose as JSX tag / JS-expression syntax. Release-note
# bullets are plain text pulled from GitHub, so a literal `<value>` (parsed as an
# unclosed JSX tag — breaks the build) or `{id}` (parsed as a JS expression
# referencing an undefined identifier — crashes rendering) must be escaped. We
# escape only outside inline-code spans, where backticks already make them
# literal. Regression source: TECH-3412 (`<value>` build break + `{id}` render
# crash both shipped from unescaped bullets).
_INLINE_CODE_SPLIT = re.compile(r"(`[^`]*`)")


def escape_mdx_text(text: str) -> str:
    parts = _INLINE_CODE_SPLIT.split(text)
    for i, part in enumerate(parts):
        if i % 2 == 0:  # outside backtick code spans
            parts[i] = part.replace("<", r"\<").replace("{", r"\{")
    return "".join(parts)


def release_date(value: str | None) -> str:
    if value:
        return value[:10]
    return datetime.now(UTC).date().isoformat()


def display_date(iso_date: str) -> str:
    months = {
        "01": "januar",
        "02": "februar",
        "03": "marts",
        "04": "april",
        "05": "maj",
        "06": "juni",
        "07": "juli",
        "08": "august",
        "09": "september",
        "10": "oktober",
        "11": "november",
        "12": "december",
    }
    year, month, day = iso_date.split("-")
    return f"{int(day)}. {months[month]} {year}"


def parse_sections(markdown: str, tag: str) -> tuple[str, list[tuple[str, list[str]]]]:
    intro = ""
    sections: list[tuple[str, list[str]]] = []
    current_title: str | None = None
    current_lines: list[str] = []

    for raw_line in markdown.splitlines():
        line = raw_line.rstrip()
        if line == f"## {tag}" or not line:
            continue
        if line.startswith("_Changes since ") or line.startswith("_Initial release "):
            intro = line.strip("_")
            continue

        heading = HEADING_PATTERN.match(line)
        if heading:
            if current_title is not None:
                sections.append((current_title, current_lines))
            current_title = heading.group("title")
            current_lines = []
            continue

        if current_title is not None:
            current_lines.append(line)

    if current_title is not None:
        sections.append((current_title, current_lines))

    return intro, sections


def summarize(sections: list[tuple[str, list[str]]]) -> str:
    counts = [
        (title, sum(1 for line in lines if BULLET_PATTERN.match(line)))
        for title, lines in sections
    ]
    counts = [(title, count) for title, count in counts if count > 0]
    total = sum(count for _, count in counts)
    if not counts:
        return "Release notes for Multica."

    labels = ", ".join(f"{count} {title.lower()}" for title, count in counts[:3])
    if len(counts) > 3:
        labels += f", plus {sum(count for _, count in counts[3:])} more"
    return f"{total} release bullets: {labels}."


def render_mdx(markdown: str, tag: str, date: str) -> str:
    intro, sections = parse_sections(markdown, tag)
    description = summarize(sections)
    lines = [
        "---",
        f"title: {yaml_quote(f'Multica {tag}')}",
        f"description: {yaml_quote(description)}",
        f"date: {date}",
        "---",
        "",
        f"**Release summary:** {description}",
        "",
    ]

    if intro:
        lines.extend([escape_mdx_text(intro), ""])

    for title, body in sections:
        lines.append(f"## {escape_mdx_text(title)}")
        lines.append("")
        lines.extend(escape_mdx_text(line) for line in body)
        lines.append("")

    return "\n".join(lines).rstrip() + "\n"


def update_index(index_path: Path, tag: str, date: str, summary: str) -> None:
    entry = f"- [**{tag}** - {display_date(date)}](/docs/cerebro/release-notes/{tag}) - {summary}"
    if index_path.exists():
        lines = index_path.read_text().splitlines()
    else:
        lines = [
            "---",
            "title: Hvad er nyt",
            "description: Oversigt over Multica-versioner - nyeste foerst.",
            "---",
            "",
            "Her staar alle Multica-udgivelser. Klik en version for at se hvad den indeholdt.",
            "",
        ]

    lines = [
        line
        for line in lines
        if not (INDEX_ENTRY_PATTERN.match(line) and f"**{tag}**" in line)
    ]
    insert_at = next((i for i, line in enumerate(lines) if INDEX_ENTRY_PATTERN.match(line)), len(lines))
    lines.insert(insert_at, entry)
    index_path.write_text("\n".join(lines).rstrip() + "\n")


def render_docs_release_notes(
    notes_path: Path,
    notes_dir: Path,
    index_path: Path,
    tag: str,
    date: str,
) -> Path:
    markdown = notes_path.read_text()
    mdx = render_mdx(markdown, tag, date)
    notes_dir.mkdir(parents=True, exist_ok=True)
    output_path = notes_dir / f"{tag}.mdx"
    output_path.write_text(mdx)
    _, sections = parse_sections(markdown, tag)
    update_index(index_path, tag, date, summarize(sections))
    return output_path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--notes", default="release-notes.md")
    parser.add_argument("--notes-dir", default="apps/web/content/docs/cerebro/release-notes")
    parser.add_argument("--index", default="apps/web/content/docs/cerebro/release-notes/index.mdx")
    parser.add_argument("--tag", required=True)
    parser.add_argument("--date")
    args = parser.parse_args()

    output = render_docs_release_notes(
        notes_path=Path(args.notes),
        notes_dir=Path(args.notes_dir),
        index_path=Path(args.index),
        tag=args.tag,
        date=release_date(args.date),
    )
    print(f"Docs release notes written to {output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
