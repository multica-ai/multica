#!/usr/bin/env python3
"""Token optimizer CLI.

Usage:
    python -m token_optimizer.cli profile <file>
    python -m token_optimizer.cli compress <file> [--output <path>]
    python -m token_optimizer.cli compress-v2 <file> [--output <path>]
    python -m token_optimizer.cli selective <task-text>
    python -m token_optimizer.cli budget <file> [--max-tokens N]
    python -m token_optimizer.cli dashboard <agent-id>
    python -m token_optimizer.cli ab-test <full-file> <opt-file>
    python -m token_optimizer.cli benchmark <file>
"""

import argparse
import json
import sys
import os

# Add parent to path for relative imports
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def cmd_profile(args):
    from token_optimizer.core.profiler import profile_file, format_profile
    result = profile_file(args.file)
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        print(format_profile(result))


def cmd_compress(args):
    from token_optimizer.core.compressor import compress_file
    result = compress_file(args.file, args.output)
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        print(f"Compression (v1): {result['original_tokens']} -> {result['compressed_tokens']} tokens "
              f"({result['reduction_pct']}% reduction)")
        for r in result["rules_applied"]:
            print(f"  - {r['rule']}: saved {r['chars_saved']} chars")


def cmd_compress_v2(args):
    from token_optimizer.core.compressor_v2 import compress_agents_md
    result = compress_agents_md(args.file, args.output)
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        print(f"Compression (v2): {result['original_tokens']} -> {result['compressed_tokens']} tokens "
              f"({result['reduction_pct']}% reduction)")
        for s in result["sections_compressed"]:
            print(f"  - {s['section']}: {s['original_tokens']} -> {s['compressed_tokens']} tok (saved {s['saved']})")
        print(f"  Sections kept as-is: {', '.join(result['sections_kept'])}")


def cmd_selective(args):
    from token_optimizer.core.selective_context import build_selective_context
    result = build_selective_context(args.task, skills_dir=args.skills_dir)
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        print(f"Task classification: {result['categories_used']}")
        print(f"Skills: {result['inject_count']} injected, {result['lazy_count']} lazy-loaded")
        print(f"Tokens: {result['inject_tokens']} inject, {result['lazy_tokens_saved']} saved "
              f"({result['reduction_pct']}% reduction)")


def cmd_budget(args):
    from token_optimizer.core.profiler import profile_file
    from token_optimizer.core.budget import create_budget_from_profile, trim_to_budget, format_budget
    profile = profile_file(args.file)
    budget = create_budget_from_profile("agent", profile, max_tokens=args.max_tokens)
    trim_plan = trim_to_budget(budget)
    if args.json:
        print(json.dumps({"budget": {
            "agent_id": budget.agent_id,
            "max_tokens": budget.max_tokens,
            "allocated": budget.total_allocated,
            "utilization": budget.utilization,
        }, "trim_plan": trim_plan}, indent=2))
    else:
        print(format_budget(budget))
        if trim_plan["action"] == "trimmed":
            print(f"\nOver budget! Trimmed {trim_plan['total_saved']} tokens.")
            for t in trim_plan["sections_trimmed"]:
                print(f"  - {t['section']}: {t['action']} (saved {t['saved']})")


def cmd_dashboard(args):
    from token_optimizer.core.dashboard import format_dashboard, compute_stats
    if args.json:
        stats = compute_stats(args.agent_id, log_dir=args.log_dir)
        print(json.dumps(stats, indent=2))
    else:
        print(format_dashboard(args.agent_id, log_dir=args.log_dir))


def cmd_ab_test(args):
    from token_optimizer.core.ab_test import run_ab_test, format_ab_report
    with open(args.full_file) as f:
        full = f.read()
    with open(args.opt_file) as f:
        opt = f.read()
    result = run_ab_test(full, opt)
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        print(format_ab_report(result))


def cmd_benchmark(args):
    """Run full benchmark pipeline: profile -> compress -> selective -> A/B."""
    from token_optimizer.core.profiler import profile_file, format_profile, estimate_tokens
    from token_optimizer.core.compressor_v2 import compress_agents_md
    from token_optimizer.core.selective_context import build_selective_context
    from token_optimizer.core.ab_test import run_ab_test, format_ab_report

    filepath = args.file
    output_dir = args.output_dir or os.path.dirname(filepath)

    # Stage 0: Baseline
    profile = profile_file(filepath)
    baseline_tokens = profile["total_tokens"]
    print(f"Stage 0 - Baseline: {baseline_tokens} tokens")
    print(format_profile(profile))

    # Stage 1: Compress AGENTS.md
    optimized_path = os.path.join(output_dir, "AGENTS.optimized.md")
    compress_result = compress_agents_md(filepath, optimized_path)
    stage1_tokens = compress_result["compressed_tokens"]
    reduction1 = compress_result["reduction_pct"]
    print(f"\nStage 1 - Compressed AGENTS.md: {stage1_tokens} tokens ({reduction1}% reduction)")

    # Stage 2: Selective context analysis
    with open(filepath) as f:
        task_text = f.read()[:500]
    selective = build_selective_context(task_text)
    print(f"Stage 2 - Selective skills: {selective['inject_count']}/{selective['total_skills']} injected, "
          f"{selective['lazy_count']} lazy-loaded ({selective['reduction_pct']}% skill reduction)")

    # A/B test: compare full vs compressed AGENTS.md
    with open(filepath) as f:
        full_ctx = f.read()
    with open(optimized_path) as f:
        opt_ctx = f.read()
    ab_result = run_ab_test(full_ctx, opt_ctx)

    total_reduction = reduction1  # AGENTS.md reduction
    skill_inject_tokens = selective["inject_tokens"]

    print(f"\n{'='*60}")
    print(f"BENCHMARK SUMMARY")
    print(f"{'='*60}")
    print(f"AGENTS.md baseline:  {baseline_tokens} tokens")
    print(f"AGENTS.md compressed: {stage1_tokens} tokens ({reduction1}% reduction)")
    print(f"Skills: {selective['inject_count']} injected ({skill_inject_tokens} tok), "
          f"{selective['lazy_count']} lazy ({selective['lazy_tokens']} tok saved)")
    print(f"A/B Quality:         {ab_result['quality_ratio']}% retention -> {ab_result['verdict']}")
    print(f"Optimized file:      {optimized_path}")

    # Log to dashboard
    from token_optimizer.core.dashboard import log_run
    log_run("benchmark", stage1_tokens, {
        "baseline": baseline_tokens,
        "compressed": stage1_tokens,
        "skills_injected": selective["inject_count"],
        "skills_lazy": selective["lazy_count"],
    }, baseline_tokens=baseline_tokens)


def main():
    parser = argparse.ArgumentParser(description="Token optimizer for agent context")
    parser.add_argument("--json", action="store_true", help="Output as JSON")
    sub = parser.add_subparsers(dest="command")

    p_profile = sub.add_parser("profile", help="Profile token usage per section")
    p_profile.add_argument("file", help="File to profile")

    p_compress = sub.add_parser("compress", help="Pattern-based compression (v1)")
    p_compress.add_argument("file", help="File to compress")
    p_compress.add_argument("--output", "-o", help="Output path")

    p_v2 = sub.add_parser("compress-v2", help="Section-replacement compression (v2)")
    p_v2.add_argument("file", help="File to compress")
    p_v2.add_argument("--output", "-o", help="Output path")

    p_sel = sub.add_parser("selective", help="Selective context filtering")
    p_sel.add_argument("task", help="Task description for classification")
    p_sel.add_argument("--skills-dir", help="Skills directory path")

    p_budget = sub.add_parser("budget", help="Token budget analysis")
    p_budget.add_argument("file", help="File to budget")
    p_budget.add_argument("--max-tokens", type=int, default=30000, help="Max token budget")

    p_dash = sub.add_parser("dashboard", help="Token usage dashboard")
    p_dash.add_argument("agent_id", help="Agent ID")
    p_dash.add_argument("--log-dir", help="Dashboard log directory")

    p_ab = sub.add_parser("ab-test", help="A/B quality test")
    p_ab.add_argument("full_file", help="Full context file")
    p_ab.add_argument("opt_file", help="Optimized context file")

    p_bench = sub.add_parser("benchmark", help="Full benchmark pipeline")
    p_bench.add_argument("file", help="File to benchmark")
    p_bench.add_argument("--output-dir", "-d", help="Output directory")

    args = parser.parse_args()

    commands = {
        "profile": cmd_profile,
        "compress": cmd_compress,
        "compress-v2": cmd_compress_v2,
        "selective": cmd_selective,
        "budget": cmd_budget,
        "dashboard": cmd_dashboard,
        "ab-test": cmd_ab_test,
        "benchmark": cmd_benchmark,
    }

    if args.command in commands:
        commands[args.command](args)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
