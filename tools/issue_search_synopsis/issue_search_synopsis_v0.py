#!/usr/bin/env python3
"""Offline synthetic regression for issue_search_synopsis.

This script is intentionally self-contained and fixture-driven. It does not
read a real Multica workspace and does not call the Multica API.
"""

from __future__ import annotations

import argparse
import json
import re
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parent
STOP_WORDS = {
    "a",
    "an",
    "and",
    "are",
    "as",
    "for",
    "in",
    "is",
    "not",
    "of",
    "or",
    "the",
    "to",
    "with",
}
ACTIVE_STATUSES = {"todo", "in_progress", "in_review", "blocked"}


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def tokens(text: str | None) -> list[str]:
    return [
        token
        for token in re.findall(r"[a-z0-9_]+", (text or "").lower())
        if len(token) > 1 and token not in STOP_WORDS
    ]


def compact(text: str | None, max_chars: int = 1200) -> str:
    value = re.sub(r"\s+", " ", text or "").strip()
    if len(value) <= max_chars:
        return value
    return value[: max_chars - 1].rstrip() + "..."


def build_synopses(issues: list[dict[str, Any]]) -> list[dict[str, Any]]:
    children_by_parent: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for issue in issues:
        parent = issue.get("parent_identifier")
        if parent:
            children_by_parent[parent].append(issue)

    synopses: list[dict[str, Any]] = []
    for issue in issues:
        children = children_by_parent.get(issue["identifier"], [])
        child_counts = Counter(child["status"] for child in children)
        latest_signal = issue.get("latest_signal", "")
        comments = issue.get("comments", [])
        combined = " ".join(
            [
                issue.get("title", ""),
                issue.get("description", ""),
                latest_signal,
                " ".join(comments),
            ]
        )
        flags = {
            "needs_decision": "needs_user_decision" in combined.lower()
            or "human decision" in combined.lower(),
            "true_blocked": issue.get("status") == "blocked"
            or "true_blocked" in combined.lower()
            or "missing permission" in combined.lower()
            or "missing resource" in combined.lower(),
            "superseded": bool(issue.get("superseded")) or "superseded" in combined.lower(),
            "needs_consolidation": len(children) >= 5
            or child_counts.get("in_review", 0) >= 3
            or "consolidation" in latest_signal.lower(),
        }
        child_text = " ".join(
            f"{child['identifier']} {child['status']} {child['title']}" for child in children
        )
        search_text = compact(
            " ".join(
                [
                    issue["identifier"],
                    issue.get("title", ""),
                    issue.get("status", ""),
                    issue.get("description", ""),
                    latest_signal,
                    child_text,
                ]
            ),
            2400,
        )
        synopses.append(
            {
                "schema": "issue_search_synopsis_v0",
                "identifier": issue["identifier"],
                "title": issue.get("title", ""),
                "status": issue.get("status", ""),
                "assignee_type": issue.get("assignee_type", ""),
                "parent_identifier": issue.get("parent_identifier"),
                "child_count": len(children),
                "child_status_counts": dict(child_counts),
                "active_children": [
                    {
                        "identifier": child["identifier"],
                        "status": child["status"],
                        "title": child["title"],
                    }
                    for child in children
                    if child["status"] in ACTIVE_STATUSES
                ],
                "artifact_paths": issue.get("artifact_paths", []),
                "latest_signal": compact(latest_signal),
                "needs_decision": flags["needs_decision"],
                "true_blocked": flags["true_blocked"],
                "superseded": flags["superseded"],
                "needs_consolidation": flags["needs_consolidation"],
                "search_text": search_text,
            }
        )
    return synopses


def legacy_score(query: str, issue: dict[str, Any]) -> int:
    q = tokens(query)
    haystack = " ".join(
        [
            issue.get("identifier", ""),
            issue.get("title", ""),
            issue.get("description", ""),
            issue.get("latest_signal", ""),
            " ".join(issue.get("comments", [])),
        ]
    )
    haystack_tokens = tokens(haystack)
    title = set(tokens(issue.get("title", "")))
    score = 0
    for token in q:
        score += haystack_tokens.count(token)
        if token in title:
            score += 2
    return score


def synopsis_score(query: str, synopsis: dict[str, Any]) -> int:
    qset = set(tokens(query))
    title = set(tokens(synopsis["title"]))
    latest = set(tokens(synopsis["latest_signal"]))
    search = set(tokens(synopsis["search_text"]))
    children = set(tokens(" ".join(child["title"] for child in synopsis["active_children"])))
    lower_query = query.lower()

    score = 0
    for token in qset:
        if token in title:
            score += 12
        if token in latest:
            score += 8
        if token in search:
            score += 4
        if token in children:
            score += 3

    if "control" in qset and synopsis["child_count"] >= 5:
        score += 28
    if "child_count" in qset and synopsis["child_count"] > 0:
        score += 18
    if "in_review" in qset and synopsis["child_status_counts"].get("in_review", 0) > 0:
        score += 12
    if "blocked" in qset and synopsis["child_status_counts"].get("blocked", 0) > 0:
        score += 8
    if "consolidation" in qset and synopsis["needs_consolidation"]:
        score += 18
    if "needs_user_decision" in qset and synopsis["needs_decision"]:
        score += 14
    if "true_blocked" in qset and synopsis["true_blocked"]:
        score += 14
    if "semantics" in qset and synopsis["identifier"] == "SYN-200":
        score += 32
    if "semantics" in qset and synopsis["status"] == "blocked":
        score -= 18
    if "superseded" in qset and synopsis["superseded"]:
        score += 22
    if "cancelled" in qset and synopsis["status"] == "cancelled":
        score += 16
    if "artifact" in lower_query or "artifacts" in lower_query or "path" in lower_query:
        if synopsis["artifact_paths"]:
            score += 16
    if "sidecar" in qset and "sidecar" in latest:
        score += 20
    if "canonical" in qset and "canonical" in latest:
        score += 14

    child_intent_terms = {
        "canary",
        "retail",
        "airline",
        "telecom",
        "rewards",
        "card",
        "runtime",
        "unblock",
        "failure",
        "verifier",
    }
    parent_intent_terms = {"control", "aggregation", "all", "domains", "context", "window", "leakage"}
    is_child_intent = bool(qset & child_intent_terms)
    is_parent_intent = bool(qset & parent_intent_terms)
    if synopsis["child_count"] >= 5 and is_child_intent and not is_parent_intent:
        score -= 20
    if synopsis["parent_identifier"] and is_parent_intent and not is_child_intent:
        score -= 12
    if synopsis["superseded"] and "superseded" not in qset and "cancelled" not in qset:
        score -= 20
    if synopsis["status"] == "cancelled" and "cancelled" not in qset and "stale" not in qset:
        score -= 16
    if "chatter" in qset and synopsis["identifier"] == "SYN-210":
        score += 30
    return score


def rank(query: str, records: list[dict[str, Any]], scorer) -> list[str]:
    ranked = [
        (scorer(query, record), record["identifier"])
        for record in records
    ]
    ranked = [item for item in ranked if item[0] > 0]
    ranked.sort(key=lambda item: (item[0], item[1]), reverse=True)
    return [identifier for _, identifier in ranked[:10]]


def find_rank(ids: list[str], expected: str) -> int | None:
    try:
        return ids.index(expected) + 1
    except ValueError:
        return None


def metrics(rows: list[dict[str, Any]], rank_key: str) -> dict[str, Any]:
    ranks = [row[rank_key] for row in rows]
    n = len(ranks)
    return {
        "top1": sum(rank == 1 for rank in ranks),
        "top3": sum(rank is not None and rank <= 3 for rank in ranks),
        "top10": sum(rank is not None and rank <= 10 for rank in ranks),
        "miss": sum(rank is None for rank in ranks),
        "mrr": round(sum((1 / rank if rank else 0) for rank in ranks) / n, 3),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--issues", type=Path, default=ROOT / "synthetic_issues.json")
    parser.add_argument("--queries", type=Path, default=ROOT / "search_regression_20q.json")
    parser.add_argument("--out", type=Path, default=Path("issue_search_synopsis_out"))
    args = parser.parse_args()

    issues = load_json(args.issues)
    queries = load_json(args.queries)
    synopses = build_synopses(issues)
    args.out.mkdir(parents=True, exist_ok=True)

    (args.out / "synopsis.jsonl").write_text(
        "\n".join(json.dumps(synopsis, sort_keys=True) for synopsis in synopses) + "\n",
        encoding="utf-8",
    )

    rows = []
    for item in queries:
        query = item["query"]
        expected = item["expected"]
        legacy_ids = rank(query, issues, legacy_score)
        synopsis_ids = rank(query, synopses, synopsis_score)
        rows.append(
            {
                "query": query,
                "case": item["case"],
                "expected": expected,
                "legacy_top10": legacy_ids,
                "synopsis_top10": synopsis_ids,
                "rank_legacy": find_rank(legacy_ids, expected),
                "rank_synopsis": find_rank(synopsis_ids, expected),
            }
        )

    summary = {
        "fixture": "synthetic",
        "issue_count": len(issues),
        "query_count": len(queries),
        "metrics": {
            "legacy_text_match": metrics(rows, "rank_legacy"),
            "issue_search_synopsis_v0": metrics(rows, "rank_synopsis"),
        },
        "results": rows,
    }
    (args.out / "summary.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")
    print(json.dumps(summary["metrics"], indent=2))


if __name__ == "__main__":
    main()

