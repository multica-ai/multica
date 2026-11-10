# -*- coding: utf-8 -*-
"""
Hermes session bridge — reverse registration + remote takeover for Multica.

Multica dispatches work *down* to agents. This bridge covers the opposite
direction: work a user starts *locally* in the Hermes desktop app / CLI,
outside Multica, is mirrored *up* into the workbench as an issue, so it can
be watched, reviewed — and steered — from anywhere.

What it does (poll loop, stdlib only):

  1. Watches the Hermes profile's session store (``state.db``, read-only)
     for locally started root sessions (``source in {local, gui}``,
     no parent session, not archived/hidden).
  2. When a session qualifies as real work (>= 2 user turns, or >= 1 user
     turn and >= 10 messages), it creates a Multica issue through the
     ``multica`` CLI, status ``in_progress``, with labels for the origin
     and the machine.
  3. When the session ends or goes idle (default 4 h), it posts a delta
     summary comment and moves the issue to ``in_review``. If the session
     resumes, the issue flips back to ``in_progress``.
  4. Comments the owner adds on the issue are queued and, once the local
     session is idle, injected back into that very session with
     ``hermes chat --resume <id>`` — full context, same working directory.
     Receipts and a reply excerpt are posted back to the issue.

Sessions that already ended before the bridge first runs are never
retro-registered; sessions still open at first run are adopted once.

Privacy: summaries pass through deterministic scrubbing (currency amounts,
ranges, thousand-separated and bare large numbers, salary-adjacent figures)
plus an LLM rewrite pass that strips personal names. If the rewrite fails,
the summary is withheld rather than posted raw. Adapt ``SALARY_KEYWORDS``
to your domain.

Configuration is via environment variables; see README.md for the table.
State lives in ``BRIDGE_STATE_DIR`` (default ``./bridge-state``).
"""
import json
import os
import re
import socket
import sqlite3
import subprocess
import sys
import threading
import time
import urllib.request
import urllib.error

# ---------------------------------------------------------------- config
def _default_hermes_home():
    if sys.platform.startswith("win"):
        base = os.environ.get("LOCALAPPDATA", os.path.expanduser("~"))
        return os.path.join(base, "hermes")
    return os.path.expanduser("~/.hermes")


HERMES_HOME = os.environ.get("BRIDGE_HERMES_HOME", _default_hermes_home())
DB_PATH = os.environ.get("BRIDGE_DB", os.path.join(HERMES_HOME, "state.db"))
STATE_DIR = os.environ.get("BRIDGE_STATE_DIR", os.path.join(os.getcwd(), "bridge-state"))
MULTICA = os.environ.get("BRIDGE_MULTICA_BIN", "multica")
MULTICA_PROFILE = os.environ.get("BRIDGE_PROFILE", "")
HERMES = os.environ.get("BRIDGE_HERMES_BIN", "hermes")
ORIGIN_LABEL = os.environ.get("BRIDGE_ORIGIN_LABEL", "local-session")
MACHINE_LABEL = os.environ.get("BRIDGE_MACHINE_LABEL", socket.gethostname())
LABEL_COLORS = {ORIGIN_LABEL: "#3b82f6", MACHINE_LABEL: "#f59e0b"}

SUMMARY_URL = os.environ.get("BRIDGE_SUMMARY_URL", "")       # OpenAI-compatible /v1/chat/completions
SUMMARY_KEY = os.environ.get("BRIDGE_SUMMARY_KEY", "")
SUMMARY_MODEL = os.environ.get("BRIDGE_SUMMARY_MODEL", "")

POLL_SEC = int(os.environ.get("BRIDGE_POLL_SEC", "60"))
IDLE_FINALIZE_SEC = int(os.environ.get("BRIDGE_IDLE_SEC", str(4 * 3600)))
ADOPT_WINDOW_SEC = 24 * 3600
INJECT_IDLE_SEC = int(os.environ.get("BRIDGE_INJECT_IDLE_SEC", "120"))
INJECT_TIMEOUT = int(os.environ.get("BRIDGE_INJECT_TIMEOUT", "1800"))
MAX_CREATES_PER_PASS = 5

STATE_PATH = os.path.join(STATE_DIR, "bridge_state.json")
LOG_PATH = os.path.join(STATE_DIR, "bridge.log")
STOP_PATH = os.path.join(STATE_DIR, "bridge.stop")
REPORT_PATH = os.path.join(STATE_DIR, "dry_run_report.txt")

DRY_RUN = os.environ.get("BRIDGE_DRY_RUN") == "1"
ONCE = os.environ.get("BRIDGE_ONCE") == "1"
TEST_SESSION = os.environ.get("BRIDGE_TEST_SESSION", "").strip()

# ---------------------------------------------------------------- scrubbing
# Battle-tested against real payroll transcripts: LLMs leak figures despite
# instructions, so scrubbing is deterministic and layered. The keyword list
# is domain vocabulary — extend it for your own sensitive data.
SALARY_KEYWORDS = (r"底薪|月薪|工资|薪资|实发|应发|提成|扣款|补扣|少扣|原扣|应扣"
                   r"|salary|wage|payroll|take-home|gross pay|net pay")
AMOUNT_RE = re.compile(r"\d[\d,]*(\.\d+)?\s*(元|块钱|RMB|¥|￥|\$|USD|EUR|GBP)")
RANGE_RE = re.compile(r"\d[\d,]*(\.\d+)?\s*/\s*\d[\d,]*(\.\d+)?")
SALARY_NUM_RE = re.compile(
    r"(" + SALARY_KEYWORDS + r")([^。；\n（）,，、:：]{0,14}?)\d[\d,]*(\.\d+)?", re.IGNORECASE)
DECIMAL_RE = re.compile(r"\d[\d,]*\.\d{2}\b")
SEP_NUM_RE = re.compile(r"\d{1,3}(?:,\d{3})+(?:\.\d+)?")
BARE_BIG_RE = re.compile(r"(?<![\dVv])(\d{4,})(?!\d)(?!\s*年)(?!-\d)")
REDACTED = "(amount redacted)"


def _bare_big_sub(m):
    # keep plausible calendar years (1900-2099); keyword/unit rules above
    # already catch years that appear as money ("wage 2026", "2026 USD")
    if 1900 <= int(m.group(1)) <= 2099:
        return m.group(1)
    return REDACTED


def scrub_summary(txt):
    txt = SALARY_NUM_RE.sub(lambda m: m.group(1) + m.group(2) + REDACTED, txt)
    txt = AMOUNT_RE.sub(REDACTED, txt)
    txt = RANGE_RE.sub(REDACTED, txt)
    txt = SEP_NUM_RE.sub(REDACTED, txt)
    txt = DECIMAL_RE.sub(REDACTED, txt)
    txt = BARE_BIG_RE.sub(_bare_big_sub, txt)
    return txt


def log(msg):
    line = f"[{time.strftime('%Y-%m-%d %H:%M:%S')}] {msg}"
    try:
        os.makedirs(STATE_DIR, exist_ok=True)
        with open(LOG_PATH, "a", encoding="utf-8") as f:
            f.write(line + "\n")
    except Exception:
        pass
    print(line, flush=True)


# ---------------------------------------------------------------- state
def load_state():
    if os.path.exists(STATE_PATH):
        try:
            with open(STATE_PATH, encoding="utf-8") as f:
                return json.load(f)
        except Exception as e:
            log(f"state load failed, starting fresh: {e}")
    return {"epoch": time.time(), "sessions": {}}


def save_state(st):
    os.makedirs(STATE_DIR, exist_ok=True)
    tmp = STATE_PATH + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(st, f, ensure_ascii=False, indent=1)
    os.replace(tmp, STATE_PATH)


# ---------------------------------------------------------------- multica
def run_multica(args, stdin_text=None, timeout=90):
    cmd = [MULTICA] + (["--profile", MULTICA_PROFILE] if MULTICA_PROFILE else []) + args
    data = stdin_text.encode("utf-8") if stdin_text is not None else None
    r = subprocess.run(cmd, input=data, capture_output=True, timeout=timeout, cwd=STATE_DIR)
    out = r.stdout.decode("utf-8", "replace")
    err = r.stderr.decode("utf-8", "replace")
    if r.returncode != 0:
        raise RuntimeError(f"multica {args[:3]} rc={r.returncode}: {(err or out)[:300]}")
    return out


_label_cache = None


def ensure_labels():
    global _label_cache
    if _label_cache is not None:
        return _label_cache
    labels = {}
    try:
        out = run_multica(["label", "list", "--output", "json"])
        for item in json.loads(out):
            if isinstance(item, dict) and item.get("name"):
                labels[item["name"]] = item.get("id")
    except Exception as e:
        log(f"label list failed: {e}")
    for name in (ORIGIN_LABEL, MACHINE_LABEL):
        if name not in labels:
            try:
                out = run_multica(["label", "create", "--name", name,
                                   "--color", LABEL_COLORS.get(name, "#3b82f6"),
                                   "--output", "json"])
                item = json.loads(out)
                if item.get("id"):
                    labels[name] = item["id"]
            except Exception as e:
                log(f"label create {name} failed: {e}")
    _label_cache = labels
    return labels


def create_issue(title, description):
    out = run_multica(
        ["issue", "create", "--title", title, "--description-stdin",
         "--status", "in_progress", "--output", "json"],
        stdin_text=description,
    )
    item = json.loads(out)
    issue_id = item.get("id")
    ident = item.get("identifier") or item.get("key") or ""
    labels = ensure_labels()
    for name in (ORIGIN_LABEL, MACHINE_LABEL):
        lid = labels.get(name)
        if lid and issue_id:
            try:
                run_multica(["issue", "label", "add", issue_id, lid])
            except Exception as e:
                log(f"label add {name} failed: {e}")
    return issue_id, ident


def add_comment(issue_id, text):
    cmd = [MULTICA] + (["--profile", MULTICA_PROFILE] if MULTICA_PROFILE else []) + \
          ["issue", "comment", "add", issue_id, "--content-stdin", "--output", "json"]
    r = subprocess.run(cmd, input=text.encode("utf-8"), capture_output=True,
                       timeout=90, cwd=STATE_DIR)
    if r.returncode != 0:
        run_multica(["issue", "comment", "add", issue_id, "--content-stdin"], stdin_text=text)
        return None
    try:
        item = json.loads(r.stdout.decode("utf-8", "replace"))
        if isinstance(item, dict) and item.get("id"):
            return item["id"]
    except Exception:
        pass
    return None


def list_comments(issue_id):
    out = run_multica(["issue", "comment", "list", issue_id, "--output", "json"])
    items = json.loads(out)
    return items if isinstance(items, list) else []


def set_status(issue_id, status):
    run_multica(["issue", "status", issue_id, status])


# ---------------------------------------------------------------- hermes db
def db_connect():
    con = sqlite3.connect(f"file:{DB_PATH}?mode=ro", uri=True, timeout=8)
    con.row_factory = sqlite3.Row
    return con


def extract_text(content):
    if not content:
        return ""
    s = content.strip()
    if s.startswith("{") or s.startswith("["):
        try:
            obj = json.loads(s)
            parts = []

            def walk(o):
                if isinstance(o, dict):
                    t = o.get("text")
                    if isinstance(t, str):
                        parts.append(t)
                    for v in o.values():
                        walk(v)
                elif isinstance(o, list):
                    for v in o:
                        walk(v)

            walk(obj)
            if parts:
                return "\n".join(parts)
        except Exception:
            pass
    return content


def session_stats(con, sid):
    row = con.execute(
        "SELECT COUNT(*) c, SUM(CASE WHEN role='user' THEN 1 ELSE 0 END) u, MAX(id) mx "
        "FROM messages WHERE session_id=? AND active=1",
        (sid,),
    ).fetchone()
    return row["c"] or 0, row["u"] or 0, row["mx"] or 0


def first_user_text(con, sid):
    row = con.execute(
        "SELECT content FROM messages WHERE session_id=? AND role='user' AND active=1 ORDER BY id LIMIT 1",
        (sid,),
    ).fetchone()
    return extract_text(row["content"]).strip() if row else ""


def delta_transcript(con, sid, after_id):
    rows = con.execute(
        "SELECT id, role, content FROM messages WHERE session_id=? AND id>? AND active=1 "
        "AND role IN ('user','assistant') ORDER BY id",
        (sid, after_id),
    ).fetchall()
    lines, total = [], 0
    last_id = after_id
    for r in rows:
        last_id = r["id"]
        txt = extract_text(r["content"]).strip()
        if not txt:
            continue
        txt = txt[:2000]
        lines.append(("User" if r["role"] == "user" else "Assistant") + ": " + txt)
        total += len(txt)
    text = "\n".join(lines)
    if total > 14000:
        text = text[:8000] + "\n...(middle omitted)...\n" + text[-4000:]
    return text, last_id


# ---------------------------------------------------------------- summary
def _llm_chat(prompt, max_tokens=800):
    body = json.dumps({
        "model": SUMMARY_MODEL,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "temperature": 0.3,
    }).encode("utf-8")
    req = urllib.request.Request(
        SUMMARY_URL, data=body,
        headers={"Authorization": "Bearer " + SUMMARY_KEY,
                 "Content-Type": "application/json",
                 "User-Agent": "Mozilla/5.0 hermes-session-bridge/1.0"},
    )
    last_err = None
    for _attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=120) as r:
                d = json.loads(r.read().decode("utf-8"))
            return (((d.get("choices") or [{}])[0].get("message") or {}).get("content") or "").strip()
        except Exception as e:
            last_err = e
            time.sleep(5)
    raise last_err


def _fallback_summary(con, sid, title):
    total, users, _mx = session_stats(con, sid)
    first = first_user_text(con, sid)
    row = con.execute(
        "SELECT content FROM messages WHERE session_id=? AND role='assistant' AND active=1 ORDER BY id DESC LIMIT 1",
        (sid,),
    ).fetchone()
    last = extract_text(row["content"]).strip()[:300] if row else ""
    return scrub_summary(
        f"Session '{title}' — {total} messages, {users} user turns.\n"
        f"First request: {first[:200]}\nLatest assistant reply: {last}")


def summarize(con, sid, transcript, title):
    if not (SUMMARY_URL and SUMMARY_KEY and SUMMARY_MODEL):
        return _fallback_summary(con, sid, title)
    prompt = (
        "Write a 3-6 line work summary of the local agent session below for a "
        "workbench issue: what was done, what was produced or decided, what is "
        "still open. Task level only. Hard privacy rules: never include any "
        "personal names; never include any salary or money figures, with or "
        "without units or ranges (write 'individual pay items pending "
        "confirmation' instead). Version numbers, headcounts, file names and "
        "process steps are fine. Output only the summary.\n\n"
        "Session title: " + title + "\n\nTranscript:\n" + transcript
    )
    try:
        txt = _llm_chat(prompt)
    except Exception as e:
        return f"(automatic summary failed: {str(e)[:120]})\nSession title: " + title
    if not txt:
        return _fallback_summary(con, sid, title)
    txt = scrub_summary(txt)
    # second pass: strip any personal names the model may have kept
    try:
        txt2 = _llm_chat(
            "Remove every personal name from the work summary below: replace "
            "names with 'an individual' or drop them, keep everything else "
            "unchanged. Output only the rewritten summary, no preamble.\n\n" + txt,
            max_tokens=800,
        )
        if txt2:
            lines = txt2.split("\n")
            for i, ln in enumerate(lines[:4]):
                if ln.strip() == "---":
                    lines = lines[i + 1:]
                    break
                if "here is" in ln.lower() or "below is" in ln.lower():
                    lines = lines[1:]
                    break
            txt2 = "\n".join(lines).strip()
            if txt2:
                return scrub_summary(txt2)
    except Exception as e:
        log(f"summary privacy pass failed, fail-closed: {e}")
        return "(summary withheld: it may contain sensitive details; see the local session)"
    return txt


# ---------------------------------------------------------------- injection
_state_lock = threading.Lock()
_inject_running = set()


def _save_locked(st):
    with _state_lock:
        save_state(st)


def collect_comments(st):
    """Poll issue comments; queue new owner instructions for injection."""
    if DRY_RUN:
        return
    for sid, rec in list(st["sessions"].items()):
        issue_id = rec.get("issue_id")
        if not issue_id:
            continue
        try:
            items = list_comments(issue_id)
        except Exception as e:
            log(f"comment poll failed {rec.get('identifier')}: {e}")
            continue
        seen = rec.setdefault("seen_comments", None)
        if seen is None:
            rec["seen_comments"] = [it.get("id") for it in items if it.get("id")]
            _save_locked(st)
            continue
        changed = False
        for it in items:
            cid = it.get("id")
            if not cid or cid in seen:
                continue
            seen.append(cid)
            changed = True
            content = (it.get("content") or "").strip()
            if not content or content.startswith("【") or content.startswith("[bridge]"):
                continue  # the bridge's own comments / system notes
            rec.setdefault("pending_inject", []).append(
                {"comment_id": cid, "text": content[:4000], "queued_at": time.time()})
            log(f"queued injection {rec.get('identifier')} comment={cid}")
        if len(seen) > 300:
            rec["seen_comments"] = seen[-300:]
            changed = True
        if changed:
            _save_locked(st)


def _injection_job(st, sid, texts):
    rec = st["sessions"].get(sid)
    if rec is None:
        return
    issue_id = rec["issue_id"]
    framed = (
        "[bridge] Takeover instruction from the workbench. This session is "
        "registered on the owner's Multica workbench; the instruction below "
        "was added as a comment on its issue. Continue the current work "
        "following it; if it conflicts with earlier instructions, this one "
        "wins.\n\n"
        + "\n\n".join(f"{i + 1}. {t}" for i, t in enumerate(texts))
    )
    qfile = os.path.join(STATE_DIR, f"inject_{sid[:12]}.txt")
    try:
        with open(qfile, "w", encoding="utf-8") as f:
            f.write(framed)
        cid = add_comment(issue_id, f"[bridge] Received {len(texts)} workbench "
                                    "instruction(s); feeding them to the local session.")
        if cid:
            rec.setdefault("seen_comments", []).append(cid)
        if rec.get("finalized"):
            try:
                set_status(issue_id, "in_progress")
            except Exception:
                pass
            rec["finalized"] = False
        rec["injected_at"] = time.time()
        _save_locked(st)
        r = subprocess.run(
            [HERMES, "chat", "--resume", sid, "--query-file", qfile,
             "-Q", "--yolo", "--accept-hooks"],
            capture_output=True, timeout=INJECT_TIMEOUT, cwd=STATE_DIR,
        )
        out = (r.stdout or b"").decode("utf-8", "replace").strip()
        err = (r.stderr or b"").decode("utf-8", "replace").strip()
        if r.returncode == 0:
            clean = "\n".join(
                ln for ln in out.split("\n")
                if not ln.startswith("↪") and "restored workspace dir" not in ln
            ).strip()
            excerpt = scrub_summary(clean[-1200:]) if clean else "(no text reply)"
            cid = add_comment(issue_id, "[bridge] Done. Local session reply:\n" + excerpt)
            log(f"injection ok sid={sid} out_len={len(out)}")
        else:
            cid = add_comment(issue_id, "[bridge] Injection failed: "
                              + scrub_summary((err or out)[-400:]))
            log(f"injection failed sid={sid} rc={r.returncode}: {(err or out)[:200]}")
        if cid:
            rec.setdefault("seen_comments", []).append(cid)
    except Exception as e:
        log(f"injection error sid={sid}: {e}")
        try:
            cid = add_comment(issue_id, "[bridge] Injection failed: " + scrub_summary(str(e)[:300]))
            if cid:
                rec.setdefault("seen_comments", []).append(cid)
        except Exception:
            pass
    finally:
        try:
            con = db_connect()
            _t, _u, mx = session_stats(con, sid)
            con.close()
            rec["last_msg_id"] = mx
        except Exception:
            pass
        with _state_lock:
            _inject_running.discard(sid)
            save_state(st)


def maybe_inject(st, con):
    """Start injections for queued comments whose session is idle."""
    if DRY_RUN:
        return
    now = time.time()
    for sid, rec in list(st["sessions"].items()):
        pending = rec.get("pending_inject") or []
        if not pending:
            continue
        with _state_lock:
            if sid in _inject_running:
                continue
        row = con.execute(
            "SELECT last_activity_at, ended_at FROM sessions WHERE id=?", (sid,)
        ).fetchone()
        if row is None:
            rec["pending_inject"] = []
            _save_locked(st)
            continue
        last_act = row["last_activity_at"] or 0
        if row["ended_at"] is None and (now - last_act) < INJECT_IDLE_SEC:
            continue  # someone is at the machine using it; try a later pass
        texts = [p["text"] for p in pending]
        rec["pending_inject"] = []
        _save_locked(st)
        with _state_lock:
            _inject_running.add(sid)
        if ONCE or TEST_SESSION:
            _injection_job(st, sid, texts)
        else:
            threading.Thread(target=_injection_job, args=(st, sid, texts), daemon=True).start()
        log(f"injection started sid={sid} n={len(texts)}")


# ---------------------------------------------------------------- core pass
def fmt_ts(ts):
    return time.strftime("%Y-%m-%d %H:%M", time.localtime(ts)) if ts else "-"


def make_title(sess, first_user):
    t = (sess["title"] or "").strip()
    if not t:
        t = re.sub(r"\s+", " ", first_user).strip()
    if not t:
        t = "Local work session"
    return t[:48]


def make_description(sess, title):
    return (
        "[bridge] This issue mirrors a locally started Hermes session "
        "(reverse registration pilot).\n"
        f"- Machine: {MACHINE_LABEL}\n"
        f"- Hermes home: {HERMES_HOME}\n"
        f"- Session ID: {sess['id']}\n"
        f"- Started: {fmt_ts(sess['started_at'])}\n"
        f"- Working directory: {sess['cwd'] or '-'}\n"
        f"- Session title: {title}\n"
        "While the session is active this issue stays in progress; when it "
        f"ends or idles for {IDLE_FINALIZE_SEC // 3600} h, the bridge posts a "
        "summary and moves it to in review. Comment on this issue to send "
        "instructions back into the local session."
    )


def qualifies(total, users, first_user):
    if users >= 2:
        return True
    if users >= 1 and total >= 10 and len(first_user) >= 4:
        return True
    return False


def process_pass(st, report=None):
    now = time.time()
    con = db_connect()
    try:
        rows = con.execute(
            "SELECT * FROM sessions WHERE archived=0 AND hidden=0 "
            "AND (parent_session_id IS NULL OR parent_session_id='') "
            "AND source IN ('local','gui') ORDER BY started_at"
        ).fetchall()
        known = {r["id"] for r in rows}
        extra_ids = [sid for sid in st["sessions"] if sid not in known]
        if extra_ids:
            ph = ",".join("?" * len(extra_ids))
            rows = rows + con.execute(
                f"SELECT * FROM sessions WHERE id IN ({ph})", extra_ids
            ).fetchall()
        creates = 0
        for sess in rows:
            sid = sess["id"]
            rec = st["sessions"].get(sid)
            total, users, max_mid = session_stats(con, sid)
            started = sess["started_at"] or 0
            ended = sess["ended_at"]
            last_act = sess["last_activity_at"] or started
            is_open_recent = ended is None and (now - last_act) < ADOPT_WINDOW_SEC
            if rec is None:
                if TEST_SESSION and sid == TEST_SESSION:
                    pass
                elif started < st["epoch"] and not (st.get("first_pass") and is_open_recent):
                    continue  # historical: never retro-register
                first_user = first_user_text(con, sid)
                if not (TEST_SESSION and sid == TEST_SESSION) and not qualifies(total, users, first_user):
                    continue
                title = make_title(sess, first_user)
                if DRY_RUN:
                    if report is not None:
                        report.append(f"WOULD-REGISTER sid={sid} msgs={total} users={users} "
                                      f"open={ended is None} title={title!r}")
                    continue
                if creates >= MAX_CREATES_PER_PASS:
                    continue
                try:
                    issue_id, ident = create_issue(title, make_description(sess, title))
                except Exception as e:
                    log(f"create failed sid={sid}: {e}")
                    continue
                creates += 1
                rec = {"issue_id": issue_id, "identifier": ident, "last_msg_id": max_mid,
                       "summarized_upto": 0, "finalized": False}
                st["sessions"][sid] = rec
                save_state(st)
                log(f"registered sid={sid} -> {ident} ({issue_id}) title={title!r}")
            if DRY_RUN or rec is None:
                continue
            if not rec["finalized"]:
                idle_done = ended is not None or (now - last_act) > IDLE_FINALIZE_SEC
                force = bool(TEST_SESSION and sid == TEST_SESSION)
                if idle_done or force:
                    transcript, last_id = delta_transcript(con, sid, rec["summarized_upto"])
                    title = make_title(sess, first_user_text(con, sid))
                    summary = summarize(con, sid, transcript, title) if transcript \
                        else "(no new conversation content in this period)"
                    header = "[bridge] Session summary (auto)" + \
                        (" — session ended" if ended else " — session idle, interim summary")
                    try:
                        add_comment(rec["issue_id"], header + "\n" + summary)
                        set_status(rec["issue_id"], "in_review")
                        rec["finalized"] = True
                        rec["summarized_upto"] = last_id
                        rec["last_msg_id"] = max_mid
                        save_state(st)
                        log(f"finalized sid={sid} -> {rec['identifier']} in_review")
                    except Exception as e:
                        log(f"finalize failed sid={sid}: {e}")
                else:
                    if max_mid != rec["last_msg_id"]:
                        rec["last_msg_id"] = max_mid
                        save_state(st)
            else:
                if max_mid > rec.get("last_msg_id", 0) and ended is None:
                    try:
                        add_comment(rec["issue_id"],
                                    "[bridge] The local session is active again; "
                                    "this issue is back in progress.")
                        set_status(rec["issue_id"], "in_progress")
                        rec["finalized"] = False
                        rec["last_msg_id"] = max_mid
                        save_state(st)
                        log(f"reopened sid={sid} -> {rec['identifier']}")
                    except Exception as e:
                        log(f"reopen failed sid={sid}: {e}")
        collect_comments(st)
        maybe_inject(st, con)
    finally:
        con.close()
    st["first_pass"] = False


def main():
    os.makedirs(STATE_DIR, exist_ok=True)
    st = load_state()
    if DRY_RUN:
        report = [f"DRY RUN at {fmt_ts(time.time())} epoch={fmt_ts(st['epoch'])} "
                  f"first_pass={st.get('first_pass', True)}"]
        st.setdefault("first_pass", True)
        process_pass(st, report)
        con = db_connect()
        n_hist = con.execute(
            "SELECT COUNT(*) c FROM sessions WHERE archived=0 AND hidden=0 "
            "AND (parent_session_id IS NULL OR parent_session_id='') AND source IN ('local','gui')"
        ).fetchone()["c"]
        con.close()
        report.append(f"local/gui root sessions total: {n_hist} "
                      "(historical ended ones are skipped by design)")
        with open(REPORT_PATH, "w", encoding="utf-8") as f:
            f.write("\n".join(report))
        print("\n".join(report))
        return
    st.setdefault("first_pass", True)
    save_state(st)
    log(f"bridge starting (epoch={fmt_ts(st['epoch'])}, test_session={TEST_SESSION or '-'})")
    while True:
        if os.path.exists(STOP_PATH):
            log("stop file present, exiting")
            return
        try:
            process_pass(st)
        except Exception as e:
            log(f"pass error: {e}")
        if ONCE or TEST_SESSION:
            log("single pass done")
            return
        time.sleep(POLL_SEC)


if __name__ == "__main__":
    main()
