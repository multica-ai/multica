#!/usr/bin/env python3
"""
Eidetix with/without eval — scorer.

Consumes results/runs.jsonl (from run_eval.py) and produces results/report.json
plus a printed summary across the three axes:

  1. Quality + grounding  — a blind LLM judge scores each output 1-5 on quality
     and 0-1 on factual grounding vs the task's graph_facts_expected. Plus a
     blind pairwise WITH-vs-WITHOUT preference per (task, trial).
  2. Consistency          — within each arm, mean pairwise lexical similarity of
     the per-trial outputs for a task, and the variance of grounding scores.
  3. Efficiency           — median Δ(tokens/turns/tool-calls) = WITH − WITHOUT,
     reported per-axis with sign NOT pre-judged.

The LLM judge is blind to which arm produced an output. If ANTHROPIC_API_KEY is
unset, the LLM axes are skipped and only efficiency + consistency (no-LLM) are
computed, so the harness is partially runnable offline.

Usage:
  pip install anthropic
  export ANTHROPIC_API_KEY=...            # for the quality/grounding/pairwise axes
  export EVAL_JUDGE_MODEL=claude-opus-4-8 # optional (default below)
  python3 evals/eidetix/judge.py
"""

import json
import os
import pathlib
import re
import shutil
import statistics
import subprocess
import tempfile
from collections import defaultdict

HERE = pathlib.Path(__file__).resolve().parent
RUNS_FILE = pathlib.Path(os.environ.get("EVAL_RUNS_FILE", str(HERE / "results" / "runs.jsonl")))
REPORT_FILE = pathlib.Path(os.environ.get("EVAL_REPORT_FILE", str(HERE / "results" / "report.json")))
TASKS_FILE = pathlib.Path(os.environ.get("EVAL_TASKS_FILE", str(HERE / "tasks.json")))
JUDGE_MODEL = os.environ.get("EVAL_JUDGE_MODEL", "claude-opus-4-8")

_WORD = re.compile(r"[a-z0-9]+")


def tokens(text: str) -> set:
    return set(_WORD.findall((text or "").lower()))


def jaccard(a: str, b: str) -> float:
    ta, tb = tokens(a), tokens(b)
    if not ta or not tb:
        return 0.0
    return len(ta & tb) / len(ta | tb)


def load():
    if not RUNS_FILE.exists():
        raise SystemExit(f"no runs at {RUNS_FILE}; run run_eval.py first")
    rows = [json.loads(l) for l in RUNS_FILE.read_text().splitlines() if l.strip()]
    suite = json.loads(TASKS_FILE.read_text())
    facts = {t["id"]: t.get("graph_facts_expected", []) for t in suite["tasks"]}
    relevant = {t["id"]: t.get("graph_relevant", True) for t in suite["tasks"]}
    titles = {t["id"]: t["title"] for t in suite["tasks"]}
    descs = {t["id"]: t["description"] for t in suite["tasks"]}
    return rows, facts, relevant, titles, descs


def get_judge():
    """Return an ask(prompt)->str callable, or None if no backend.

    Prefers the Anthropic SDK when ANTHROPIC_API_KEY is set; otherwise falls
    back to the local `claude` CLI (already logged in — no API key needed),
    which is the usual case on a dev box. The CLI runs with an empty,
    strict mcp-config so no tools/personal MCP servers load — the judge just
    reads + scores.
    """
    if os.environ.get("ANTHROPIC_API_KEY"):
        try:
            import anthropic
            c = anthropic.Anthropic()

            def ask_sdk(prompt: str, max_tokens: int = 400) -> str:
                m = c.messages.create(model=JUDGE_MODEL, max_tokens=max_tokens,
                                      messages=[{"role": "user", "content": prompt}])
                return m.content[0].text
            print(f"judge: using Anthropic SDK ({JUDGE_MODEL})")
            return ask_sdk
        except ImportError:
            print("WARN: anthropic SDK missing; trying the claude CLI instead.")

    claude_bin = os.environ.get("MULTICA_CLAUDE_PATH") or shutil.which("claude") \
        or os.path.expanduser("~/.local/bin/claude")
    if claude_bin and os.path.exists(claude_bin):
        empty = os.path.join(tempfile.gettempdir(), "eidetix-judge-empty-mcp.json")
        with open(empty, "w") as f:
            f.write('{"mcpServers":{}}')

        def ask_cli(prompt: str, max_tokens: int = 400) -> str:
            p = subprocess.run(
                [claude_bin, "-p", prompt, "--output-format", "json",
                 "--strict-mcp-config", "--mcp-config", empty, "--max-turns", "1"],
                capture_output=True, text=True, timeout=240)
            if p.returncode != 0:
                raise RuntimeError(p.stderr.strip()[:200])
            return json.loads(p.stdout).get("result", "")
        print(f"judge: using claude CLI at {claude_bin} (no API key needed)")
        return ask_cli

    print("WARN: no judge backend (set ANTHROPIC_API_KEY or install claude); "
          "skipping quality/grounding/pairwise.")
    return None


def score_one(ask, desc, facts, output):
    """Blind absolute scoring of a single output. Returns (quality 1-5, grounding 0-1) or None."""
    key = "\n".join(f"- {f}" for f in facts) if facts else "(none — generic task)"
    prompt = (
        "You are grading a marketing deliverable. You do NOT know how it was produced.\n\n"
        f"TASK:\n{desc}\n\nGROUND-TRUTH TEAM FACTS (the rubric key):\n{key}\n\n"
        f"OUTPUT:\n{output}\n\n"
        "Return STRICT JSON only: {\"quality\": <1-5 int>, \"grounding\": <0..1 float>, "
        "\"note\": \"<=20 words\"}. quality = overall craft/usefulness. grounding = fraction "
        "of the ground-truth facts correctly used minus any contradiction of them; if there "
        "are no facts, grounding = 1.0 when the output is reasonable."
    )
    try:
        m = re.search(r"\{.*\}", ask(prompt), re.S)
        d = json.loads(m.group(0))
        return float(d["quality"]), float(d["grounding"])
    except Exception as e:  # noqa: BLE001 — a flaky judge call must not kill the report
        print(f"  (score_one skipped: {type(e).__name__}: {str(e)[:80]})")
        return None


def pairwise(ask, desc, a_out, b_out):
    """Blind preference between two outputs. Returns 'A', 'B', or None."""
    prompt = (
        f"TASK:\n{desc}\n\nOUTPUT A:\n{a_out}\n\nOUTPUT B:\n{b_out}\n\n"
        "Which output is better for this team's marketing? Reply STRICT JSON only: "
        "{\"winner\": \"A\"|\"B\", \"why\": \"<=20 words\"}."
    )
    try:
        m = re.search(r"\{.*\}", ask(prompt), re.S)
        return json.loads(m.group(0))["winner"].strip().upper()
    except Exception as e:  # noqa: BLE001
        print(f"  (pairwise skipped: {type(e).__name__}: {str(e)[:80]})")
        return None


def main() -> int:
    rows, facts, relevant, titles, descs = load()
    client = get_judge()

    # index: by_task[task][arm] = list of rows (one per trial)
    by_task = defaultdict(lambda: defaultdict(list))
    for r in rows:
        by_task[r["task_id"]][r["arm"]].append(r)

    report = {"per_task": {}, "totals": {}}
    q_with, q_without, g_with, g_without = [], [], [], []
    wins_with = wins_total = 0
    dtok, dturn, dtool = [], [], []
    sim_with, sim_without = [], []

    for task_id, arms in by_task.items():
        w = arms.get("with", [])
        wo = arms.get("without", [])
        entry = {"title": titles.get(task_id), "graph_relevant": relevant.get(task_id, True)}

        # --- Efficiency: paired by trial index ---
        for a, b in zip(sorted(w, key=lambda x: x["trial"]), sorted(wo, key=lambda x: x["trial"])):
            if a.get("output_tokens") is not None and b.get("output_tokens") is not None:
                dtok.append((a["output_tokens"] or 0) - (b["output_tokens"] or 0))
            if a.get("messages") is not None and b.get("messages") is not None:
                dturn.append((a["messages"] or 0) - (b["messages"] or 0))
            if a.get("tool_calls") is not None and b.get("tool_calls") is not None:
                dtool.append((a["tool_calls"] or 0) - (b["tool_calls"] or 0))

        # --- Consistency: mean pairwise lexical similarity within each arm ---
        def arm_similarity(arm_rows):
            outs = [r.get("output", "") for r in arm_rows if r.get("output")]
            pairs = [jaccard(outs[i], outs[j]) for i in range(len(outs)) for j in range(i + 1, len(outs))]
            return round(statistics.mean(pairs), 4) if pairs else None
        entry["consistency_with"] = arm_similarity(w)
        entry["consistency_without"] = arm_similarity(wo)
        if entry["consistency_with"] is not None:
            sim_with.append(entry["consistency_with"])
        if entry["consistency_without"] is not None:
            sim_without.append(entry["consistency_without"])

        # --- Quality + grounding + pairwise (LLM) ---
        if client:
            tq_w, tg_w, tq_wo, tg_wo = [], [], [], []
            for r in w:
                if r.get("output"):
                    res = score_one(client, descs[task_id], facts.get(task_id, []), r["output"])
                    if res:
                        q, g = res
                        tq_w.append(q); tg_w.append(g); q_with.append(q); g_with.append(g)
            for r in wo:
                if r.get("output"):
                    res = score_one(client, descs[task_id], facts.get(task_id, []), r["output"])
                    if res:
                        q, g = res
                        tq_wo.append(q); tg_wo.append(g); q_without.append(q); g_without.append(g)
            entry["quality_with"] = round(statistics.mean(tq_w), 3) if tq_w else None
            entry["quality_without"] = round(statistics.mean(tq_wo), 3) if tq_wo else None
            entry["grounding_with"] = round(statistics.mean(tg_w), 3) if tg_w else None
            entry["grounding_without"] = round(statistics.mean(tg_wo), 3) if tg_wo else None
            entry["grounding_var_with"] = round(statistics.pvariance(tg_w), 4) if len(tg_w) > 1 else None
            entry["grounding_var_without"] = round(statistics.pvariance(tg_wo), 4) if len(tg_wo) > 1 else None
            # pairwise per trial (randomise A/B by trial parity to avoid position bias)
            for a, b in zip(sorted(w, key=lambda x: x["trial"]), sorted(wo, key=lambda x: x["trial"])):
                if not (a.get("output") and b.get("output")):
                    continue
                with_is_A = (a["trial"] % 2 == 0)
                first, second = (a, b) if with_is_A else (b, a)
                winner = pairwise(client, descs[task_id], first["output"], second["output"])
                if winner not in ("A", "B"):
                    continue
                with_won = (winner == "A") == with_is_A
                wins_total += 1
                wins_with += 1 if with_won else 0

        report["per_task"][task_id] = entry

    def med(xs):
        return round(statistics.median(xs), 2) if xs else None

    report["totals"] = {
        "quality_with": round(statistics.mean(q_with), 3) if q_with else None,
        "quality_without": round(statistics.mean(q_without), 3) if q_without else None,
        "grounding_with": round(statistics.mean(g_with), 3) if g_with else None,
        "grounding_without": round(statistics.mean(g_without), 3) if g_without else None,
        "pairwise_with_winrate": round(wins_with / wins_total, 3) if wins_total else None,
        "pairwise_n": wins_total,
        "consistency_with_mean": round(statistics.mean(sim_with), 4) if sim_with else None,
        "consistency_without_mean": round(statistics.mean(sim_without), 4) if sim_without else None,
        "median_delta_output_tokens": med(dtok),
        "median_delta_turns": med(dturn),
        "median_delta_tool_calls": med(dtool),
        "delta_sign_convention": "WITH minus WITHOUT (positive = WITH uses more)",
    }

    REPORT_FILE.write_text(json.dumps(report, indent=2))
    t = report["totals"]
    print("\n================ Eidetix eval summary ================")
    print(f"Quality   WITH {t['quality_with']}  vs WITHOUT {t['quality_without']}")
    print(f"Grounding WITH {t['grounding_with']}  vs WITHOUT {t['grounding_without']}")
    print(f"Pairwise  WITH win-rate {t['pairwise_with_winrate']} (n={t['pairwise_n']})")
    print(f"Consistency (intra-arm output similarity)  WITH {t['consistency_with_mean']}  vs WITHOUT {t['consistency_without_mean']}")
    print(f"Efficiency Δ (WITH−WITHOUT)  out_tokens {t['median_delta_output_tokens']}  "
          f"turns {t['median_delta_turns']}  tool_calls {t['median_delta_tool_calls']}")
    print(f"\nFull report: {REPORT_FILE}")
    if not client:
        print("NOTE: ANTHROPIC_API_KEY unset → quality/grounding/pairwise skipped (efficiency+consistency only).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
