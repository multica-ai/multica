"""Pattern-based context compression (v1).

Applies regex-driven transformations to reduce token count
while preserving semantic meaning.
"""

import re


RULES = [
    (r"<!--.*?-->", "", "Remove HTML comments"),
    (r"\n{3,}", "\n\n", "Collapse blank lines"),
    (r"\s{2,}", " ", "Collapse whitespace"),
    (r"\[(-[-\w]+)\s+<[^>]+>\]", r"[\1 <arg>]", "Shorten CLI arg placeholders"),
    (r"^[-=]{10,}$", "---", "Shorten separator lines"),
    (r"[ \t]+$", "", "Strip trailing whitespace"),
]


def compress_text(text: str, rules=None):
    """Apply compression rules. Returns (compressed, applied_rules)."""
    if rules is None:
        rules = RULES
    applied = []
    result = text
    for pattern, replacement, description in rules:
        new_result = re.sub(pattern, replacement, result, flags=re.MULTILINE | re.DOTALL)
        if new_result != result:
            saved = len(result) - len(new_result)
            applied.append({"rule": description, "chars_saved": saved})
            result = new_result
    return result, applied


def compress_file(filepath: str, output_path: str = None) -> dict:
    """Compress a file and optionally write result."""
    with open(filepath) as f:
        original = f.read()
    compressed, applied = compress_text(original)
    if output_path:
        with open(output_path, "w") as f:
            f.write(compressed)
    from .profiler import estimate_tokens
    orig_tok = estimate_tokens(original)
    comp_tok = estimate_tokens(compressed)
    reduction = ((orig_tok - comp_tok) / orig_tok * 100) if orig_tok else 0
    return {
        "input": filepath,
        "output": output_path,
        "original_tokens": orig_tok,
        "compressed_tokens": comp_tok,
        "reduction_pct": round(reduction, 1),
        "rules_applied": applied,
    }
