"""A/B quality validation framework.

Compares full context vs optimized context across test categories
to ensure no quality degradation.
"""

import json
import os
from dataclasses import dataclass, field
from typing import Optional
from .profiler import estimate_tokens


@dataclass
class TestCase:
    """A single test case for A/B comparison."""
    category: str  # code_review, bug_fix, feature, research, devops
    task: str
    expected_keywords: list[str] = field(default_factory=list)
    quality_score_full: float = 0.0
    quality_score_optimized: float = 0.0


# Default test suite covering common agent task types
DEFAULT_TEST_SUITE = [
    TestCase(
        category="code_review",
        task="Review a PR that changes the authentication middleware to support OAuth2",
        expected_keywords=["oauth", "auth", "middleware", "token", "security"],
    ),
    TestCase(
        category="bug_fix",
        task="Fix a bug where the dashboard crashes when the database returns null values",
        expected_keywords=["null", "dashboard", "database", "error", "fix"],
    ),
    TestCase(
        category="feature",
        task="Implement a new user notification system with email and SMS support",
        expected_keywords=["notification", "email", "sms", "user", "implement"],
    ),
    TestCase(
        category="research",
        task="Research the best approach for implementing real-time data sync",
        expected_keywords=["real-time", "sync", "data", "approach", "research"],
    ),
    TestCase(
        category="devops",
        task="Set up CI/CD pipeline with automated testing and deployment to staging",
        expected_keywords=["ci", "cd", "pipeline", "test", "deploy", "staging"],
    ),
]


def score_response(response: str, expected_keywords: list[str]) -> float:
    """Score a response 0-100 based on keyword coverage and structure."""
    if not response:
        return 0.0

    response_lower = response.lower()
    keyword_hits = sum(1 for kw in expected_keywords if kw.lower() in response_lower)
    keyword_score = (keyword_hits / len(expected_keywords) * 100) if expected_keywords else 50

    # Structure bonus: check for lists, headers, code blocks
    structure_score = 0
    if "\n-" in response or "\n*" in response:
        structure_score += 10
    if "##" in response or "**" in response:
        structure_score += 5
    if "```" in response or "`" in response:
        structure_score += 5

    # Length penalty: too short = probably low quality
    length_score = min(20, len(response) / 50)

    return min(100, keyword_score * 0.6 + structure_score + length_score)


def run_ab_test(full_context: str, optimized_context: str,
                test_suite: list[TestCase] = None,
                score_fn=None) -> dict:
    """Run A/B test comparing full vs optimized context.

    Note: This framework scores context QUALITY (completeness of information),
    not generation quality. It checks whether the optimized context retains
    the key information needed for each task type.
    """
    if test_suite is None:
        test_suite = DEFAULT_TEST_SUITE
    if score_fn is None:
        score_fn = score_response

    results = []
    full_scores = []
    opt_scores = []

    for tc in test_suite:
        # Score how well context covers each task's keywords
        full_score = score_fn(full_context, tc.expected_keywords)
        opt_score = score_fn(optimized_context, tc.expected_keywords)

        tc.quality_score_full = full_score
        tc.quality_score_optimized = opt_score

        results.append({
            "category": tc.category,
            "task": tc.task[:80],
            "full_score": round(full_score, 1),
            "optimized_score": round(opt_score, 1),
            "delta": round(opt_score - full_score, 1),
        })
        full_scores.append(full_score)
        opt_scores.append(opt_score)

    avg_full = sum(full_scores) / len(full_scores) if full_scores else 0
    avg_opt = sum(opt_scores) / len(opt_scores) if opt_scores else 0
    quality_ratio = (avg_opt / avg_full * 100) if avg_full else 100

    full_tokens = estimate_tokens(full_context)
    opt_tokens = estimate_tokens(optimized_context)
    token_reduction = ((full_tokens - opt_tokens) / full_tokens * 100) if full_tokens else 0

    # PASS if quality ratio >= 85% (allows minor degradation)
    verdict = "PASS" if quality_ratio >= 85 else "FAIL"

    return {
        "verdict": verdict,
        "quality_full_avg": round(avg_full, 1),
        "quality_optimized_avg": round(avg_opt, 1),
        "quality_ratio": round(quality_ratio, 1),
        "full_tokens": full_tokens,
        "optimized_tokens": opt_tokens,
        "token_reduction_pct": round(token_reduction, 1),
        "test_results": results,
    }


def format_ab_report(result: dict) -> str:
    """Format A/B test results as readable report."""
    lines = [
        f"# A/B Quality Test: {result['verdict']}",
        "",
        f"Full context: {result['quality_full_avg']}% quality, {result['full_tokens']} tokens",
        f"Optimized:    {result['quality_optimized_avg']}% quality, {result['optimized_tokens']} tokens",
        f"Quality ratio: {result['quality_ratio']}% | Token reduction: {result['token_reduction_pct']}%",
        "",
        f"{'Category':<15} {'Full':>8} {'Opt':>8} {'Delta':>8}",
        "-" * 43,
    ]
    for r in result["test_results"]:
        lines.append(f"{r['category']:<15} {r['full_score']:>7.1f}% {r['optimized_score']:>7.1f}% {r['delta']:>+7.1f}%")
    return "\n".join(lines)
