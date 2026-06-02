"""Section-level token profiling for agent context files."""

import re
import os
from typing import Optional


def estimate_tokens(text: str) -> int:
    """Estimate token count. ~4 chars per token (GPT family heuristic)."""
    return max(1, len(text) // 4) if text.strip() else 0


def split_sections(text: str) -> dict[str, str]:
    """Split markdown text into sections by headers (## or ###)."""
    sections = {}
    current_key = "_preamble"
    current_buf: list[str] = []

    for line in text.split("\n"):
        if re.match(r"^#{1,4}\s+", line):
            if current_buf:
                sections[current_key] = "\n".join(current_buf)
            current_key = line.strip().lstrip("#").strip()
            current_buf = [line]
        else:
            current_buf.append(line)

    if current_buf:
        sections[current_key] = "\n".join(current_buf)

    return sections


def profile_file(filepath: str) -> dict:
    """Profile a single file, returning per-section token counts."""
    with open(filepath, "r") as f:
        text = f.read()

    sections = split_sections(text)
    total_tokens = estimate_tokens(text)
    section_stats = []

    for name, content in sections.items():
        tok = estimate_tokens(content)
        section_stats.append({
            "section": name,
            "tokens": tok,
            "chars": len(content),
            "pct": round(tok / total_tokens * 100, 1) if total_tokens else 0,
        })

    section_stats.sort(key=lambda s: s["tokens"], reverse=True)

    return {
        "file": os.path.basename(filepath),
        "total_tokens": total_tokens,
        "total_chars": len(text),
        "sections": section_stats,
    }


def profile_directory(dirpath: str, extensions: Optional[list[str]] = None) -> dict:
    """Profile all matching files in a directory."""
    if extensions is None:
        extensions = [".md", ".txt", ".yaml", ".yml", ".json"]

    results = []
    grand_total = 0

    for root, _, files in os.walk(dirpath):
        for fname in sorted(files):
            if any(fname.endswith(ext) for ext in extensions):
                fpath = os.path.join(root, fname)
                r = profile_file(fpath)
                results.append(r)
                grand_total += r["total_tokens"]

    return {
        "directory": dirpath,
        "file_count": len(results),
        "grand_total_tokens": grand_total,
        "files": results,
    }


def format_profile(profile: dict) -> str:
    """Format profile results as a readable table."""
    lines = []
    if "file" in profile:
        lines.append(f"# Token Profile: {profile['file']}")
        lines.append(f"Total: {profile['total_tokens']} tokens ({profile['total_chars']} chars)\n")
        lines.append(f"{'Section':<50} {'Tokens':>8} {'%':>6}")
        lines.append("-" * 68)
        for s in profile["sections"]:
            lines.append(f"{s['section'][:50]:<50} {s['tokens']:>8} {s['pct']:>5.1f}%")
    else:
        lines.append(f"# Directory Profile: {profile['directory']}")
        lines.append(f"Files: {profile['file_count']}, Total: {profile['grand_total_tokens']} tokens\n")
        for f in profile["files"]:
            lines.append(f"  {f['file']}: {f['total_tokens']} tokens")

    return "\n".join(lines)
