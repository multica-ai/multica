#!/usr/bin/env python3
"""
外部补偿脚本：自动 rerun failed + transient error + attempt < max_attempts 的 issue task。

用法:
  python3 scripts/compensate_failed_runs.py          # 扫描并 rerun（dry-run 默认开启）
  python3 scripts/compensate_failed_runs.py --apply  # 实际执行 rerun
  python3 scripts/compensate_failed_runs.py --help   # 查看所有选项

部署建议：cron job，5min 间隔
  */5 * * * * cd /path/to/workdir && python3 scripts/compensate_failed_runs.py --apply
"""

import argparse
import json
import os
import subprocess
import sys
import time
from datetime import datetime, timezone

# ---- Configuration ----------------------------------------------------------

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
WORKDIR = os.path.dirname(SCRIPT_DIR)
STATE_FILE = os.path.join(WORKDIR, ".compensate_state.json")
COOLDOWN_SECONDS = 300  # 5 分钟冷却期

# Transient error 匹配模式（不区分大小写）
TRANSIENT_PATTERNS = [
    "timed out",
    "timeout",
    "context deadline exceeded",
    "empty output",
    "returned empty",
    "no output",
    "database is locked",
    "database locked",
    "connection refused",
    "connection reset",
    "connection closed",
    "broken pipe",
    "eof",
    "no route to host",
    "network is unreachable",
    "disk full",
    "out of memory",
    "cannot allocate memory",
    "sigkill",
    "killed",
    "daemon restart",
    "daemon stopped",
    "runtime offline",
    "runtime unavailable",
    "503",
    "502 bad gateway",
    "service unavailable",
    "temporarily unavailable",
    "rate limit",
    "too many requests",
]

# Permanent error 匹配模式（不应补偿）
PERMANENT_PATTERNS = [
    "auth",
    "authentication",
    "unauthorized",
    "forbidden",
    "permission denied",
    "invalid config",
    "missing config",
    "configuration error",
    "content policy",
    "content filter",
    "safety violation",
    "iteration limit",
    "loop detected",
    "exceeded iterations",
    "invalid api key",
    "quota exceeded",
    "billing",
    "not found",
    "does not exist",
    "invalid request",
]

# Autopilot 相关标题模式（用于排除 autopilot 任务）
# 这些模式匹配 autopilot 执行容器 issue 的标题特征
AUTOPILOT_TITLE_PATTERNS = [
    "每日 Issue 健康度巡检",
    "每日 Blocked Issue 超时升级",
    "每日 QA 验收队列",
    "每日 QA 任务推进",
    "每日后端任务推进",
    "每日前端任务推进",
    "每日设计任务推进",
    "每日管理类 Issue 噪声清理",
    "每日代码 Review",
    "每日执行失败状态同步",
    "每日 Issue 责任人流转",
    "每日 Issue-PR 一致性",
    "每日 GitHub Actions Budget 监控",
    "每小时项目推进",
    "每周测试与门禁有效性审查",
    "每周 Autopilot 健康度巡检",
    "每周 Issue 健康度深度巡检",
    "每日日报",
]

# Autopilot 相关描述标记（更可靠，存在于执行容器 issue 的描述中）
AUTOPILOT_DESC_MARKERS = [
    "本次运行是执行容器",
    "执行容器管理",
    "执行容器规则",
    "现在执行一次【",
]


# ---- Helpers ----------------------------------------------------------------

def multica(*args):
    """运行 multica CLI 命令并返回 JSON 解析结果。"""
    cmd = ["multica"] + list(args)
    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, timeout=30,
            env={**os.environ, "NO_COLOR": "1"}
        )
        if result.returncode != 0:
            stderr = result.stderr.strip()
            if stderr:
                print(f"  [WARN] multica {' '.join(args[:2])} failed: {stderr[:200]}", file=sys.stderr)
            return None
        output = result.stdout.strip()
        if not output:
            return None
        return json.loads(output)
    except subprocess.TimeoutExpired:
        print(f"  [WARN] multica {' '.join(args[:2])} timed out", file=sys.stderr)
        return None
    except json.JSONDecodeError as e:
        print(f"  [WARN] multica {' '.join(args[:2])} invalid JSON: {e}", file=sys.stderr)
        return None
    except Exception as e:
        print(f"  [WARN] multica {' '.join(args[:2])} error: {e}", file=sys.stderr)
        return None


def is_transient_error(error_msg):
    """检查错误信息是否匹配 transient 模式且不匹配 permanent 模式。"""
    if not error_msg:
        return False
    text = error_msg.lower()
    # 先检查 permanent（排除优先级更高）
    for pattern in PERMANENT_PATTERNS:
        if pattern in text:
            return False
    # 再检查 transient
    for pattern in TRANSIENT_PATTERNS:
        if pattern in text:
            return True
    return False


def is_autopilot_issue(issue):
    """判断 issue 是否为 autopilot 任务。
    
    采用双重检查：
    1. 标题匹配已知 autopilot 执行容器模式
    2. 描述中包含 autopilot 执行容器标记
    """
    title = (issue.get("title") or "").lower()
    desc = (issue.get("description") or "").lower()

    # 标题模式匹配
    for pattern in AUTOPILOT_TITLE_PATTERNS:
        if pattern.lower() in title:
            return True

    # 描述标记匹配
    for marker in AUTOPILOT_DESC_MARKERS:
        if marker.lower() in desc:
            return True

    return False


def load_state():
    """加载幂等状态文件。"""
    if os.path.exists(STATE_FILE):
        try:
            with open(STATE_FILE) as f:
                return json.load(f)
        except (json.JSONDecodeError, IOError):
            return {}
    return {}


def save_state(state):
    """保存幂等状态文件。"""
    with open(STATE_FILE, "w") as f:
        json.dump(state, f, indent=2)


def clean_expired_state(state, ttl=COOLDOWN_SECONDS * 2):
    """清理过期的状态记录（超过 2 倍冷却期）。"""
    now = time.time()
    expired = [k for k, v in state.items() if now - v["timestamp"] > ttl]
    for k in expired:
        del state[k]
    return state


def within_cooldown(state, issue_id, cooldown_seconds=COOLDOWN_SECONDS):
    """检查 issue 是否在冷却期内。"""
    record = state.get(issue_id)
    if not record:
        return False
    elapsed = time.time() - record["timestamp"]
    return elapsed < cooldown_seconds


def record_rerun(state, issue_id, task_id, success, reason=""):
    """记录 rerun 尝试。"""
    state[issue_id] = {
        "task_id": task_id,
        "timestamp": time.time(),
        "success": success,
        "reason": reason,
    }


# ---- Core Logic -------------------------------------------------------------

def fetch_all_issues(exclude_statuses=None):
    """分页获取所有 issue。"""
    if exclude_statuses is None:
        exclude_statuses = {"done", "cancelled"}

    all_issues = []
    offset = 0
    limit = 50
    while True:
        result = multica("issue", "list", "--limit", str(limit), "--offset", str(offset), "--output", "json")
        if result is None:
            break
        issues = result.get("issues", [])
        for issue in issues:
            if issue.get("status") not in exclude_statuses:
                all_issues.append(issue)

        if not result.get("has_more", False):
            break
        offset += limit

    return all_issues


def fetch_latest_run(issue_id):
    """获取 issue 的最新 run。"""
    runs = multica("issue", "runs", issue_id, "--output", "json")
    if not runs or not isinstance(runs, list) or len(runs) == 0:
        return None
    # runs 按时间倒序排列（最新在前）
    return runs[0]


def rerun_issue(issue_id):
    """触发 issue 的 rerun。"""
    result = multica("issue", "rerun", issue_id, "--output", "json")
    return result


def scan_and_compensate(dry_run=True, cooldown_seconds=COOLDOWN_SECONDS, max_issues=0):
    """主扫描与补偿逻辑。"""
    print(f"{'='*60}")
    print(f"补偿扫描启动 — {datetime.now(timezone.utc).isoformat()}")
    print(f"模式: {'DRY-RUN (不实际执行 rerun)' if dry_run else 'APPLY (实际执行 rerun)'}")
    print(f"冷却期: {cooldown_seconds}s")
    print(f"{'='*60}")

    # 加载幂等状态
    state = load_state()
    state = clean_expired_state(state, ttl=cooldown_seconds * 2)

    # 获取所有活跃 issue
    print("\n[1/4] 获取 issue 列表...")
    issues = fetch_all_issues()
    if max_issues > 0:
        issues = issues[:max_issues]
    print(f"  活跃 issue 总数: {len(issues)}")

    # 收集候选
    print("\n[2/4] 扫描 latest run...")
    candidates = []
    skipped_total = 0
    autopilot_count = 0
    cooldown_count = 0

    for i, issue in enumerate(issues):
        issue_id = issue["id"]
        identifier = issue.get("identifier", issue_id[:8])

        if (i + 1) % 50 == 0:
            print(f"  进度: {i+1}/{len(issues)}")

        # 排除 autopilot
        if is_autopilot_issue(issue):
            autopilot_count += 1
            continue

        # 检查冷却期
        if within_cooldown(state, issue_id, cooldown_seconds):
            cooldown_count += 1
            continue

        # 获取 latest run
        latest = fetch_latest_run(issue_id)
        if latest is None:
            skipped_total += 1
            continue

        # 过滤：必须 failed
        if latest.get("status") != "failed":
            skipped_total += 1
            continue

        # 过滤：attempt < max_attempts
        attempt = latest.get("attempt", 0)
        max_attempts = latest.get("max_attempts", 1)
        if attempt >= max_attempts:
            skipped_total += 1
            continue

        # 过滤：必须是 transient error
        error_msg = latest.get("error") or ""
        if not is_transient_error(error_msg):
            skipped_total += 1
            continue

        candidates.append((issue, latest))

    print(f"  候选数: {len(candidates)}")
    print(f"  排除: autopilot={autopilot_count}, 冷却期={cooldown_count}, 其他不符合={skipped_total}")

    # 执行 rerun
    print(f"\n[3/4] 执行补偿...")
    results = []
    for issue, run in candidates:
        issue_id = issue["id"]
        identifier = issue.get("identifier", issue_id[:8])
        task_id = run["id"]
        error_msg = run.get("error", "")
        attempt = run.get("attempt", 0)
        max_attempts = run.get("max_attempts", 1)

        print(f"\n  Issue: {identifier} ({issue_id})")
        print(f"    task_id: {task_id}")
        print(f"    error: {error_msg}")
        print(f"    attempt: {attempt}/{max_attempts}")

        if dry_run:
            print(f"    [DRY-RUN] 将触发 rerun")
            record_rerun(state, issue_id, task_id, True, "dry-run: candidate identified")
            results.append({
                "issue_id": issue_id,
                "identifier": identifier,
                "task_id": task_id,
                "error": error_msg,
                "attempt": attempt,
                "max_attempts": max_attempts,
                "rerun_triggered": False,
                "reason": "dry-run",
            })
        else:
            result = rerun_issue(issue_id)
            if result and not result.get("error"):
                print(f"    [OK] rerun 已触发")
                record_rerun(state, issue_id, task_id, True, "rerun triggered")
                results.append({
                    "issue_id": issue_id,
                    "identifier": identifier,
                    "task_id": task_id,
                    "error": error_msg,
                    "attempt": attempt,
                    "max_attempts": max_attempts,
                    "rerun_triggered": True,
                    "reason": "success",
                })
            else:
                err = result.get("error", "unknown") if result else "no response"
                print(f"    [FAIL] rerun 失败: {err}")
                record_rerun(state, issue_id, task_id, False, str(err))
                results.append({
                    "issue_id": issue_id,
                    "identifier": identifier,
                    "task_id": task_id,
                    "error": error_msg,
                    "attempt": attempt,
                    "max_attempts": max_attempts,
                    "rerun_triggered": False,
                    "reason": f"rerun failed: {err}",
                })

    # 保存状态
    save_state(state)
    print(f"\n  已更新幂等状态文件: {STATE_FILE}")

    # 汇总
    print(f"\n[4/4] 扫描汇总")
    print(f"{'='*60}")
    print(f"扫描 issue 总数: {len(issues)}")
    print(f"候选数: {len(candidates)}")
    triggered = sum(1 for r in results if r["rerun_triggered"])
    failed_count = sum(1 for r in results if not r["rerun_triggered"])
    print(f"rerun 触发: {triggered}")
    print(f"rerun 失败/跳过: {failed_count}")
    print(f"排除统计: autopilot={autopilot_count}, 冷却期={cooldown_count}, 其他不符合={skipped_total}")
    print(f"{'='*60}")

    # 详细结果
    if results:
        print(f"\n详细结果:")
        for r in results:
            print(f"  [{r['identifier']}] task={r['task_id'][:8]} "
                  f"error=\"{r['error'][:60]}\" "
                  f"attempt={r['attempt']}/{r['max_attempts']} "
                  f"rerun={'OK' if r['rerun_triggered'] else 'SKIP/FAIL'} "
                  f"({r['reason']})")

    return results


# ---- CLI -------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(
        description="外部补偿脚本：扫描并自动 rerun failed + transient error + attempt < max_attempts 的 issue task"
    )
    parser.add_argument(
        "--apply", action="store_true",
        help="实际执行 rerun（默认 dry-run 模式）"
    )
    parser.add_argument(
        "--cooldown", type=int, default=COOLDOWN_SECONDS,
        help=f"冷却期秒数（默认 {COOLDOWN_SECONDS}s = 5min）"
    )
    parser.add_argument(
        "--state-file", default=STATE_FILE,
        help=f"幂等状态文件路径（默认 {STATE_FILE}）"
    )
    parser.add_argument(
        "--max-issues", type=int, default=0,
        help="最多处理多少 issue（0=全部，调试用）"
    )
    args = parser.parse_args()

    results = scan_and_compensate(
        dry_run=not args.apply,
        cooldown_seconds=args.cooldown,
        max_issues=args.max_issues,
    )

    # 输出 JSON 摘要到 stdout 以便后续处理
    print("\n--- JSON SUMMARY ---")
    print(json.dumps(results, indent=2))

    return 0 if results else 0


if __name__ == "__main__":
    sys.exit(main())
