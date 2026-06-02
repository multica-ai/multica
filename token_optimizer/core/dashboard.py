"""Per-run token usage tracking with baseline comparison and alerts."""

import json
import os
from datetime import datetime, timezone
from typing import Optional


DEFAULT_LOG_DIR = os.path.expanduser("~/.hermes/token_dashboard")


def log_run(agent_id: str, tokens_used: int, context_sections: dict,
            log_dir: str = None, baseline_tokens: int = None) -> dict:
    """Log a single run's token usage to JSONL."""
    if log_dir is None:
        log_dir = DEFAULT_LOG_DIR
    os.makedirs(log_dir, exist_ok=True)

    entry = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "agent_id": agent_id,
        "tokens_used": tokens_used,
        "sections": context_sections,
        "baseline_tokens": baseline_tokens,
    }

    if baseline_tokens:
        entry["reduction_pct"] = round(
            (baseline_tokens - tokens_used) / baseline_tokens * 100, 1
        )

    log_path = os.path.join(log_dir, f"{agent_id}.jsonl")
    with open(log_path, "a") as f:
        f.write(json.dumps(entry) + "\n")

    return {"logged": True, "path": log_path, "entry": entry}


def read_runs(agent_id: str, limit: int = 50, log_dir: str = None) -> list[dict]:
    """Read recent runs for an agent."""
    if log_dir is None:
        log_dir = DEFAULT_LOG_DIR
    log_path = os.path.join(log_dir, f"{agent_id}.jsonl")
    if not os.path.exists(log_path):
        return []
    runs = []
    with open(log_path) as f:
        for line in f:
            line = line.strip()
            if line:
                runs.append(json.loads(line))
    return runs[-limit:]


def compute_stats(agent_id: str, log_dir: str = None) -> dict:
    """Compute aggregate stats for an agent's runs."""
    runs = read_runs(agent_id, log_dir=log_dir)
    if not runs:
        return {"agent_id": agent_id, "runs": 0}

    tokens = [r["tokens_used"] for r in runs]
    reductions = [r.get("reduction_pct", 0) for r in runs if "reduction_pct" in r]

    return {
        "agent_id": agent_id,
        "runs": len(runs),
        "avg_tokens": sum(tokens) // len(tokens),
        "max_tokens": max(tokens),
        "min_tokens": min(tokens),
        "avg_reduction_pct": round(sum(reductions) / len(reductions), 1) if reductions else None,
        "last_run": runs[-1]["timestamp"],
    }


def check_budget_alert(agent_id: str, tokens_used: int, max_tokens: int,
                       warn_threshold: float = 0.8) -> Optional[dict]:
    """Check if usage exceeds budget threshold."""
    utilization = tokens_used / max_tokens if max_tokens else 0
    if utilization >= 1.0:
        return {
            "level": "critical",
            "message": f"OVER BUDGET: {tokens_used}/{max_tokens} tokens ({utilization:.0%})",
            "agent_id": agent_id,
        }
    elif utilization >= warn_threshold:
        return {
            "level": "warning",
            "message": f"Approaching budget: {tokens_used}/{max_tokens} tokens ({utilization:.0%})",
            "agent_id": agent_id,
        }
    return None


def format_dashboard(agent_id: str, log_dir: str = None) -> str:
    """Format dashboard view for an agent."""
    stats = compute_stats(agent_id, log_dir)
    if stats["runs"] == 0:
        return f"No runs logged for agent {agent_id}"

    lines = [
        f"# Token Dashboard: {agent_id}",
        f"Runs: {stats['runs']} | Avg: {stats['avg_tokens']} tok | "
        f"Range: {stats['min_tokens']}-{stats['max_tokens']}",
    ]
    if stats["avg_reduction_pct"] is not None:
        lines.append(f"Avg Reduction: {stats['avg_reduction_pct']}%")
    lines.append(f"Last Run: {stats['last_run']}")

    return "\n".join(lines)
