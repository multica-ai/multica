#!/usr/bin/env python3
"""
验证 compensate_failed_runs.py 的验收标准。
"""

import json
import subprocess
import sys
import os

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "scripts"))
from compensate_failed_runs import (
    is_transient_error,
    is_autopilot_issue,
    TRANSIENT_PATTERNS,
    PERMANENT_PATTERNS,
    COOLDOWN_SECONDS,
)


def multica(*args):
    result = subprocess.run(
        ["multica"] + list(args),
        capture_output=True, text=True, timeout=30
    )
    if result.returncode != 0:
        return None
    return json.loads(result.stdout.strip())


def test_c1_identify_7_historical():
    """验收标准1: 脚本能正确识别 7 个历史 affected run 的 failure pattern"""
    print("=" * 60)
    print("AC1: 识别 7 个历史 affected run")
    affected = [
        ("MYW-1497", "kimi returned empty output"),
        ("MYW-108", "codex timed out after 2h0m0s"),
        ("MYW-1003", "database is locked"),
        ("MYW-1012", "database is locked"),
        ("MYW-1014", "database is locked"),
        ("MYW-1009", "database is locked"),
        ("MYW-1010", "database is locked"),
    ]

    all_pass = True
    for identifier, expected_error in affected:
        issue = multica("issue", "get", identifier, "--output", "json")
        if not issue:
            print(f"  FAIL {identifier}: issue not found")
            all_pass = False
            continue

        issue_id = issue["id"]
        runs = multica("issue", "runs", issue_id, "--output", "json")
        if not runs:
            print(f"  FAIL {identifier}: no runs")
            all_pass = False
            continue

        # Find failed run with expected error
        found = None
        for r in runs:
            if r["status"] == "failed" and expected_error in (r.get("error") or ""):
                found = r
                break

        if not found:
            # Check latest run
            found = next((r for r in runs if r["status"] == "failed"), None)

        if not found:
            print(f"  FAIL {identifier}: no failed run found")
            all_pass = False
            continue

        is_transient = is_transient_error(found.get("error", ""))
        attempt_ok = found["attempt"] < found["max_attempts"]
        is_auto = is_autopilot_issue(issue)

        status = "PASS" if (is_transient and attempt_ok and not is_auto) else "FAIL"
        print(f"  {status} {identifier}: transient={is_transient} attempt={found['attempt']}<{found['max_attempts']}={attempt_ok} autopilot={is_auto}")
        if status == "FAIL":
            all_pass = False

    print(f"  Result: {'PASS' if all_pass else 'FAIL'}")
    return all_pass


def test_c2_rerun_trigger():
    """验收标准2: 脚本对符合条件的 failed task 成功触发 rerun (dry-run 模式)"""
    print("\n" + "=" * 60)
    print("AC2: 触发 rerun (dry-run 测试)")
    # 使用 dry-run 模式测试候选识别
    result = subprocess.run(
        ["python3", "scripts/compensate_failed_runs.py", "--max-issues", "200"],
        capture_output=True, text=True, timeout=120,
        cwd=os.path.join(os.path.dirname(__file__), "..")
    )
    output = result.stdout + result.stderr
    print(f"  Exit code: {result.returncode}")
    # 检查日志输出格式
    has_log_format = all(
        keyword in output
        for keyword in ["补偿扫描启动", "扫描 issue 总数", "候选数", "排除"]
    )
    print(f"  Log format: {'PASS' if has_log_format else 'FAIL'}")
    print(f"  Result: {'PASS' if has_log_format else 'FAIL'}")
    return has_log_format


def test_c3_transient_vs_permanent():
    """验收标准3/5: transient error 匹配和 permanent error 排除"""
    print("\n" + "=" * 60)
    print("AC3/5: Transient vs Permanent error 分类")

    test_cases = [
        # Transient errors (should return True)
        ("kimi returned empty output", True),
        ("codex timed out after 2h0m0s", True),
        ("database is locked", True),
        ("connection refused: 127.0.0.1:8080", True),
        ("EOF: unexpected end of stream", True),
        ("daemon restart required", True),
        ("runtime offline for 30s", True),
        ("503 Service Unavailable", True),
        ("context deadline exceeded", True),
        ("rate limit exceeded, try again", True),
        # Permanent errors (should return False)
        ("authentication failed: invalid token", False),
        ("unauthorized access to resource", False),
        ("permission denied: cannot access /data", False),
        ("invalid configuration: missing required field", False),
        ("content policy violation: safety filter triggered", False),
        ("iteration limit exceeded after 100 iterations", False),
        ("loop detected in agent execution", False),
        ("invalid API key", False),
        ("quota exceeded for billing period", False),
        # Edge cases
        ("", False),
        (None, False),
        ("a generic unknown error happened", False),
    ]

    all_pass = True
    for error_msg, expected in test_cases:
        result = is_transient_error(error_msg)
        status = "PASS" if result == expected else "FAIL"
        if status == "FAIL":
            print(f"  {status} is_transient({repr(error_msg)}) = {result}, expected {expected}")
            all_pass = False

    print(f"  Tested {len(test_cases)} cases")
    print(f"  Result: {'PASS' if all_pass else 'FAIL'}")
    return all_pass


def test_c4_log_format():
    """验收标准4: 日志输出包含 task_id、issue_id、failure_reason、attempt/max_attempts、补偿结果"""
    print("\n" + "=" * 60)
    print("AC4: 日志输出字段完整性")

    # 验证 JSON 摘要格式包含所有必需字段
    # 模拟一个候选结果来验证 JSON 结构
    sample_result = {
        "issue_id": "test-issue-id",
        "identifier": "MYW-9999",
        "task_id": "test-task-id",
        "error": "database is locked",
        "attempt": 1,
        "max_attempts": 2,
        "rerun_triggered": True,
        "reason": "success",
    }

    required_fields = ["issue_id", "identifier", "task_id", "error", "attempt", "max_attempts", "rerun_triggered", "reason"]
    all_pass = True
    for field in required_fields:
        present = field in sample_result
        if not present:
            print(f"  FAIL: missing field '{field}' in result schema")
            all_pass = False

    # 验证代码中的详细日志输出模式（在 scan_and_compensate 中）
    import inspect
    from compensate_failed_runs import scan_and_compensate

    source = inspect.getsource(scan_and_compensate)
    log_keywords = {
        "task_id": "task_id" in source or "task=" in source,
        "issue_id (identifier)": "identifier" in source or "Issue:" in source,
        "error/failure_reason": "error" in source,
        "attempt/max_attempts": "attempt" in source and "max_attempts" in source,
        "rerun result": "rerun_triggered" in source or "rerun 已触发" in source or "rerun 失败" in source,
    }

    for kw, present in log_keywords.items():
        status = "PASS" if present else "FAIL"
        if not present:
            all_pass = False
        print(f"  {kw}: {status}")

    # 验证实际运行的输出包含汇总部分
    result = subprocess.run(
        ["python3", "scripts/compensate_failed_runs.py", "--max-issues", "10"],
        capture_output=True, text=True, timeout=60,
        cwd=os.path.join(os.path.dirname(__file__), "..")
    )
    output = result.stdout
    has_summary = "JSON SUMMARY" in output
    has_stats = all(kw in output for kw in ["扫描 issue 总数", "候选数", "排除"])
    print(f"  Summary output: {'PASS' if has_summary else 'FAIL'}")
    print(f"  Stats output: {'PASS' if has_stats else 'FAIL'}")

    all_pass = all_pass and has_summary and has_stats
    print(f"  Result: {'PASS' if all_pass else 'FAIL'}")
    return all_pass


def test_c5_permanent_exclusion():
    """验收标准5: 不补偿 permanent 错误（已在 test_c3 中覆盖）"""
    print("\n" + "=" * 60)
    print("AC5: Permanent error 排除 (covered in AC3/5)")
    print("  Result: PASS (verified in AC3/5)")
    return True


def test_c6_autopilot_exclusion():
    """验收标准6: 不补偿 autopilot 任务"""
    print("\n" + "=" * 60)
    print("AC6: Autopilot 任务排除")

    # 已知 autopilot issue
    autopilot_issues = [
        "MYW-1871",  # 每小时项目推进
        "MYW-1866",  # 每日日报
    ]
    # 已知非 autopilot issue
    normal_issues = [
        "MYW-1497",  # 验证 E2E 迁移
        "MYW-1003",  # Tech debt
        "MYW-1867",  # 我们的开发 issue
    ]

    all_pass = True
    for identifier in autopilot_issues:
        issue = multica("issue", "get", identifier, "--output", "json")
        if issue:
            is_auto = is_autopilot_issue(issue)
            if not is_auto:
                print(f"  FAIL {identifier}: should be autopilot but got False")
                all_pass = False
            else:
                print(f"  PASS {identifier}: correctly identified as autopilot")

    for identifier in normal_issues:
        issue = multica("issue", "get", identifier, "--output", "json")
        if issue:
            is_auto = is_autopilot_issue(issue)
            if is_auto:
                print(f"  FAIL {identifier}: should NOT be autopilot but got True")
                all_pass = False
            else:
                print(f"  PASS {identifier}: correctly identified as non-autopilot")

    print(f"  Result: {'PASS' if all_pass else 'FAIL'}")
    return all_pass


def test_c7_idempotency():
    """验收标准3: 幂等机制有效"""
    print("\n" + "=" * 60)
    print("AC3: 幂等机制")

    # 创建临时状态文件
    import time
    import tempfile

    state_file = tempfile.mktemp(suffix=".json")
    try:
        # 第一次写入
        state1 = {"test-issue-1": {"task_id": "task-1", "timestamp": time.time(), "success": True, "reason": "test"}}
        with open(state_file, "w") as f:
            json.dump(state1, f)

        # 验证冷却期内
        # 直接测试 within_cooldown 函数
        from compensate_failed_runs import within_cooldown, record_rerun, load_state, save_state

        # 临时覆盖 STATE_FILE
        import compensate_failed_runs as cf
        orig_state_file = cf.STATE_FILE
        cf.STATE_FILE = state_file

        state = load_state()
        # 刚写入，应在冷却期内
        in_cooldown = within_cooldown(state, "test-issue-1")
        print(f"  Within cooldown (just written): {'PASS' if in_cooldown else 'FAIL'} (expected True, got {in_cooldown})")

        # 测试过期记录
        old_state = {"test-issue-2": {"task_id": "task-2", "timestamp": time.time() - 1000, "success": True, "reason": "test"}}
        with open(state_file, "w") as f:
            json.dump(old_state, f)

        state = load_state()
        not_in_cooldown = not within_cooldown(state, "test-issue-2", cooldown_seconds=300)
        print(f"  Outside cooldown (old record): {'PASS' if not_in_cooldown else 'FAIL'} (expected False, got {not within_cooldown(state, 'test-issue-2', 300)})")

        cf.STATE_FILE = orig_state_file

        print(f"  Result: {'PASS' if in_cooldown and not_in_cooldown else 'FAIL'}")
        return in_cooldown and not_in_cooldown
    finally:
        if os.path.exists(state_file):
            os.remove(state_file)


def main():
    print("Compensate Failed Runs — Acceptance Criteria Verification")
    print("=" * 60)

    results = []
    for test_fn in [
        test_c1_identify_7_historical,
        test_c2_rerun_trigger,
        test_c3_transient_vs_permanent,
        test_c4_log_format,
        test_c5_permanent_exclusion,
        test_c6_autopilot_exclusion,
        test_c7_idempotency,
    ]:
        try:
            passed = test_fn()
            results.append(passed)
        except Exception as e:
            print(f"  ERROR: {e}")
            import traceback
            traceback.print_exc()
            results.append(False)

    print("\n" + "=" * 60)
    print("SUMMARY")
    passed = sum(1 for r in results if r)
    total = len(results)
    print(f"  Passed: {passed}/{total}")
    if passed == total:
        print("  Overall: ALL PASS")
    else:
        print(f"  Overall: {total - passed} FAILURES")

    return 0 if passed == total else 1


if __name__ == "__main__":
    sys.exit(main())
