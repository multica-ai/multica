"""Task-aware skill/context filtering.

Matches task keywords to skill categories and only injects
relevant skills. Rest are lazy-loaded on demand via skill_view().
"""

import os
from typing import Optional
from .profiler import estimate_tokens


# Category -> keyword mappings for task classification
CATEGORY_KEYWORDS = {
    "github": ["git", "pr", "pull request", "merge", "branch", "commit", "repo", "code review", "ci", "cd"],
    "devops": ["deploy", "docker", "container", "kubernetes", "k8s", "pipeline", "devops", "infrastructure"],
    "data-science": ["data", "analysis", "jupyter", "notebook", "pandas", "visualization", "chart"],
    "mlops": ["model", "train", "inference", "gpu", "llm", "ml", "ai", "fine-tune", "benchmark", "eval"],
    "creative": ["design", "mockup", "diagram", "ascii", "pixel", "art", "video", "animation"],
    "research": ["paper", "arxiv", "research", "study", "academic", "literature", "polymarket"],
    "productivity": ["email", "calendar", "notion", "airtable", "slides", "presentation", "docs"],
    "software-development": ["debug", "test", "tdd", "refactor", "code", "implement", "feature", "bug"],
    "social-media": ["twitter", "post", "social", "x.com", "tweet"],
    "gaming": ["minecraft", "game", "server", "modpack"],
    "smart-home": ["hue", "lights", "smart home", "automation"],
}


def classify_task(text: str) -> list[str]:
    """Classify task text into relevant categories."""
    text_lower = text.lower()
    matched = []
    for category, keywords in CATEGORY_KEYWORDS.items():
        score = sum(1 for kw in keywords if kw in text_lower)
        if score > 0:
            matched.append((category, score))
    matched.sort(key=lambda x: x[1], reverse=True)
    return [cat for cat, _ in matched]


def load_skill_index(skills_dir: str = None) -> list[dict]:
    """Load skill metadata from skills directory."""
    if skills_dir is None:
        skills_dir = os.path.expanduser("~/.hermes/skills")

    skills = []
    if not os.path.isdir(skills_dir):
        return skills

    for root, dirs, files in os.walk(skills_dir):
        for f in files:
            if f == "SKILL.md":
                fpath = os.path.join(root, f)
                rel = os.path.relpath(fpath, skills_dir)
                category = rel.split("/")[0] if "/" in rel else "general"
                name = os.path.basename(os.path.dirname(fpath))
                try:
                    with open(fpath) as fh:
                        content = fh.read()
                    skills.append({
                        "name": name,
                        "category": category,
                        "path": fpath,
                        "tokens": estimate_tokens(content),
                    })
                except Exception:
                    pass
    return skills


def filter_skills(skills: list[dict], categories: list[str],
                  max_inject: int = 15) -> dict:
    """Filter skills: inject matched, lazy-load the rest.

    The savings come from NOT injecting non-matched skills into context.
    If there were N skills total and we inject only M, we save the tokens
    of the (N - M) skills that are lazy-loaded.
    """
    inject = []
    lazy = []

    for skill in skills:
        if skill["category"] in categories and len(inject) < max_inject:
            inject.append(skill)
        else:
            lazy.append(skill)

    inject_tokens = sum(s["tokens"] for s in inject)
    lazy_tokens = sum(s["tokens"] for s in lazy)
    all_tokens = inject_tokens + lazy_tokens

    return {
        "inject": inject,
        "lazy": lazy,
        "inject_count": len(inject),
        "lazy_count": len(lazy),
        "inject_tokens": inject_tokens,
        "lazy_tokens": lazy_tokens,
        "total_skills": len(skills),
        "categories_used": categories,
        "reduction_pct": round(lazy_tokens / all_tokens * 100, 1) if all_tokens > 0 else 0,
    }


def build_selective_context(task_text: str, skills_dir: str = None,
                            max_inject: int = 15) -> dict:
    """Full pipeline: classify task -> filter skills -> compute savings."""
    categories = classify_task(task_text)
    skills = load_skill_index(skills_dir)
    result = filter_skills(skills, categories, max_inject)
    result["task_keywords"] = task_text[:100]
    return result
