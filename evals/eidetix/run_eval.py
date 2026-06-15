#!/usr/bin/env python3
"""
Eidetix with/without eval — runner (full Multica stack, Claude Code provider).

Per (task, arm, trial) it:
  1. sets the arm via `multica project eidetix enable|disable <project>`,
  2. creates an issue on the bound project assigned to the eval agent (which
     auto-enqueues a task the daemon's Claude Code runtime claims),
  3. polls the issue's task to a terminal status,
  4. collects the agent's final comment + token/turn/tool-call telemetry from
     the DB,
  5. appends a row to results/runs.jsonl.

Runs are SERIAL on purpose: the arm is a per-project flag read at claim time, so
two issues must not be in flight under different intended arms simultaneously.
Run order is interleaved+shuffled to average out model/endpoint drift.

This does NOT score anything — that is judge.py.

Prerequisites (see README.md): local backend on this branch with
MULTICA_EIDETIX_SECRET_KEY set; a local daemon running a Claude Code runtime;
the eval agent created on that runtime; the bound project's Eidetix token set;
`multica` CLI built and configured (server_url, workspace_id, login token).

Config via env:
  MULTICA_BIN        path to the multica binary      (default: server/bin/multica)
  DATABASE_URL       local Postgres                  (default: local dev URL)
  EVAL_PROJECT       bound project id (required)
  EVAL_AGENT_ID      eval agent UUID on the CC runtime (required)
  EVAL_TRIALS        trials per (task, arm)          (default: 3)
  EVAL_TIMEOUT_S     per-run completion timeout secs (default: 1200)
  EVAL_POLL_S        poll interval seconds           (default: 10)
"""

import json
import os
import pathlib
import subprocess
import sys
import time

HERE = pathlib.Path(__file__).resolve().parent
RESULTS = HERE / "results"
RUNS_FILE = RESULTS / "runs.jsonl"

MULTICA_BIN = os.environ.get("MULTICA_BIN", str(HERE.parent.parent / "server" / "bin" / "multica"))
DATABASE_URL = os.environ.get("DATABASE_URL", "postgres://multica:multica@localhost:5432/multica?sslmode=disable")
PROJECT = os.environ.get("EVAL_PROJECT", "").strip()
AGENT_ID = os.environ.get("EVAL_AGENT_ID", "").strip()
TRIALS = int(os.environ.get("EVAL_TRIALS", "3"))
TIMEOUT_S = int(os.environ.get("EVAL_TIMEOUT_S", "1200"))
POLL_S = int(os.environ.get("EVAL_POLL_S", "10"))

TERMINAL = {"done", "completed", "failed", "cancelled"}


def die(msg: str) -> None:
    print(f"ERROR: {msg}", file=sys.stderr)
    sys.exit(2)


def mc(*args: str, stdin: str | None = None) -> dict | list | str:
    """Call the multica CLI; parse JSON stdout when --output json is used."""
    cmd = [MULTICA_BIN, *args]
    p = subprocess.run(cmd, input=stdin, capture_output=True, text=True)
    if p.returncode != 0:
        die(f"`{' '.join(args)}` failed (exit {p.returncode}):\n{p.stderr.strip()}")
    out = p.stdout.strip()
    if not out:
        return ""
    try:
        return json.loads(out)
    except json.JSONDecodeError:
        return out


def set_arm(enabled: bool) -> None:
    verb = "enable" if enabled else "disable"
    mc("project", "eidetix", verb, PROJECT, "--output", "json")


def create_issue(task: dict, status: str) -> str:
    res = mc("issue", "create",
             "--title", task["title"],
             "--description", task["description"],
             "--status", status,
             "--assignee-id", AGENT_ID,
             "--project", PROJECT,
             "--output", "json")
    issue_id = res.get("id") if isinstance(res, dict) else None
    if not issue_id:
        die(f"issue create returned no id: {res}")
    return issue_id


def telemetry(issue_id: str) -> dict:
    """Read the latest task for the issue + token/turn/tool-call counts."""
    q = """
    SELECT atq.id::text, atq.status,
      COALESCE((SELECT SUM(input_tokens) FROM task_usage WHERE task_id = atq.id),0),
      COALESCE((SELECT SUM(output_tokens) FROM task_usage WHERE task_id = atq.id),0),
      COALESCE((SELECT SUM(cache_read_tokens) FROM task_usage WHERE task_id = atq.id),0),
      COALESCE((SELECT SUM(cache_write_tokens) FROM task_usage WHERE task_id = atq.id),0),
      (SELECT count(*) FROM task_message WHERE task_id = atq.id),
      (SELECT count(*) FROM task_message WHERE task_id = atq.id AND tool IS NOT NULL)
    FROM agent_task_queue atq
    WHERE atq.issue_id = $1
    ORDER BY atq.created_at DESC LIMIT 1;
    """
    p = subprocess.run(["psql", DATABASE_URL, "-tA", "-F", "\t", "-v", "ON_ERROR_STOP=1",
                        "-c", q.replace("$1", f"'{issue_id}'")],
                       capture_output=True, text=True)
    if p.returncode != 0 or not p.stdout.strip():
        return {"task_id": None, "status": None}
    cols = p.stdout.strip().split("\t")
    return {
        "task_id": cols[0], "status": cols[1],
        "input_tokens": int(cols[2]), "output_tokens": int(cols[3]),
        "cache_read_tokens": int(cols[4]), "cache_write_tokens": int(cols[5]),
        "messages": int(cols[6]), "tool_calls": int(cols[7]),
    }


def wait_for_terminal(issue_id: str) -> dict:
    deadline = 0
    last = {"status": None}
    waited = 0
    while waited < TIMEOUT_S:
        last = telemetry(issue_id)
        if last.get("status") in TERMINAL:
            return last
        time.sleep(POLL_S)
        waited += POLL_S
    last["timed_out"] = True
    return last


def final_agent_output(issue_id: str) -> str:
    comments = mc("issue", "comment", "list", issue_id, "--output", "json")
    if not isinstance(comments, list):
        return ""
    agent_comments = [c for c in comments if str(c.get("author_type", c.get("actor_type", ""))).lower() == "agent"]
    pool = agent_comments or comments
    if not pool:
        return ""
    return str(pool[-1].get("content", pool[-1].get("body", "")))


def main() -> int:
    if not PROJECT or not AGENT_ID:
        die("set EVAL_PROJECT and EVAL_AGENT_ID")
    if not pathlib.Path(MULTICA_BIN).exists():
        die(f"multica binary not found at {MULTICA_BIN} (run `make build`)")
    suite = json.loads((HERE / "tasks.json").read_text())
    status = suite.get("status", "todo")
    tasks = suite["tasks"]
    RESULTS.mkdir(exist_ok=True)

    # Build the interleaved run plan: (task, arm, trial), shuffled deterministically
    # (index-based, since Math.random is unavailable in this style of harness we
    # use a fixed rotation so reruns are reproducible).
    plan = []
    for trial in range(TRIALS):
        for arm in ("with", "without"):
            for ti, task in enumerate(tasks):
                plan.append((task, arm, trial))
    # rotate by trial to interleave arms/tasks rather than batching
    plan.sort(key=lambda x: (x[2], (0 if x[1] == "with" else 1) ^ (x[2] % 2)))

    print(f"==> {len(plan)} runs planned ({len(tasks)} tasks x 2 arms x {TRIALS} trials)")
    with RUNS_FILE.open("a") as fh:
        for i, (task, arm, trial) in enumerate(plan, 1):
            print(f"[{i}/{len(plan)}] task={task['id']} arm={arm} trial={trial} ...", flush=True)
            set_arm(arm == "with")
            issue_id = create_issue(task, status)
            tel = wait_for_terminal(issue_id)
            output = final_agent_output(issue_id)
            row = {
                "task_id": task["id"], "graph_relevant": task.get("graph_relevant", True),
                "arm": arm, "trial": trial, "issue_id": issue_id,
                "status": tel.get("status"), "timed_out": tel.get("timed_out", False),
                "input_tokens": tel.get("input_tokens"), "output_tokens": tel.get("output_tokens"),
                "cache_read_tokens": tel.get("cache_read_tokens"), "cache_write_tokens": tel.get("cache_write_tokens"),
                "messages": tel.get("messages"), "tool_calls": tel.get("tool_calls"),
                "output": output,
            }
            fh.write(json.dumps(row) + "\n")
            fh.flush()
            print(f"      -> status={row['status']} out_tok={row['output_tokens']} "
                  f"turns={row['messages']} tools={row['tool_calls']} chars={len(output)}")
    print(f"\n==> done. Raw rows in {RUNS_FILE}. Score with: python3 evals/eidetix/judge.py")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
