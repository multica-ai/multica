"""Section-replacement compression (v2).

Replaces verbose AGENTS.md sections with concise equivalents
while preserving all actionable information.
"""

from .profiler import split_sections, estimate_tokens


def _compress_metadata(text: str) -> str:
    return """## Issue Metadata

High-signal KV scratchpad. Pin only: materially important + re-read by future runs.
- Read on entry, write on exit (sparingly). Empty {} normal.
- Keys: `pr_url`, `pr_number`, `pipeline_status`, `deploy_url`, `waiting_on`, `blocked_reason`, `decision`
- No secrets, logs, summaries, run timestamps. Latest fact wins over stale metadata."""


def _compress_commands(text: str) -> str:
    return """## Available Commands

Use `--output json` for structured data. Run `<cmd> --help` for details.

Core: `issue get|create|update|status|comment add|comment list|metadata list|set|delete`
Squad: `squad member set-role`
Repo: `repo checkout <url> [--ref <branch>]`"""


def _compress_mentions(text: str) -> str:
    return """## Mentions

- `[MUL-123](mention://issue/<id>)` -- link (safe)
- `[@Name](mention://member/<id>)` -- notifies human
- `[@Name](mention://agent/<id>)` -- triggers agent run

Don't mention: in replies to agents, thank-yous, sign-offs.
Do mention: escalation, first delegation, user-requested."""


def _compress_comment_format(text: str) -> str:
    return """## Comment Formatting

Always `--content-stdin` with HEREDOC `<<'COMMENT'`. Never inline `--content`. Same `--parent` as trigger."""


def _compress_workflow(text: str) -> str:
    return """## Workflow

Triggered by NEW comment. Steps: get issue -> check metadata -> read thread -> do work -> post result comment -> (optional) pin metadata. Don't change status unless asked."""


def _compress_sub_issues(text: str) -> str:
    return """## Sub-issue Creation

`--status todo` = start now (agent fires). `--status backlog` = wait (promote later).
Parallel: all todo. Serial: only step 1 is todo."""


def _compress_attachments(text: str) -> str:
    return """## Attachments

Use `multica attachment --help` for file access. Don't open Multica URLs directly."""


def _compress_cli_reminder(text: str) -> str:
    return """## CLI Only

All Multica interactions via `multica` CLI. No curl/wget. Missing functionality -> comment to workspace owner."""


def _compress_output(text: str) -> str:
    return """## Output

Results via `multica issue comment add`. Terminal output invisible to user. Concise: outcome, not process."""


# Section name -> compression function (defined after all functions)
SECTION_STRATEGIES = {
    "issue metadata": _compress_metadata,
    "available commands": _compress_commands,
    "mentions": _compress_mentions,
    "comment formatting": _compress_comment_format,
    "workflow": _compress_workflow,
    "sub-issue creation": _compress_sub_issues,
    "attachments": _compress_attachments,
    "important: always use the multica cli": _compress_cli_reminder,
    "output": _compress_output,
}


def compress_section(section_name: str, text: str):
    """Apply section-specific compression. Returns None if no strategy."""
    key_lower = section_name.lower()
    for key, fn in SECTION_STRATEGIES.items():
        if key in key_lower or key_lower in key:
            return fn(text)
    return None


def compress_agents_md(filepath: str, output_path: str = None) -> dict:
    """Compress full AGENTS.md using section replacement."""
    with open(filepath) as f:
        text = f.read()

    sections = split_sections(text)
    result_parts = []
    compressed_sections = []
    kept_sections = []

    for name, content in sections.items():
        compressed = compress_section(name, content)
        if compressed:
            orig_tok = estimate_tokens(content)
            comp_tok = estimate_tokens(compressed)
            result_parts.append(compressed)
            compressed_sections.append({
                "section": name,
                "original_tokens": orig_tok,
                "compressed_tokens": comp_tok,
                "saved": orig_tok - comp_tok,
            })
        else:
            result_parts.append(content)
            kept_sections.append(name)

    result_text = "\n\n".join(result_parts)

    if output_path:
        with open(output_path, "w") as f:
            f.write(result_text)

    orig_total = estimate_tokens(text)
    comp_total = estimate_tokens(result_text)
    reduction = ((orig_total - comp_total) / orig_total * 100) if orig_total else 0

    return {
        "input": filepath,
        "output": output_path,
        "original_tokens": orig_total,
        "compressed_tokens": comp_total,
        "reduction_pct": round(reduction, 1),
        "sections_compressed": compressed_sections,
        "sections_kept": kept_sections,
    }
