"""Configurable per-agent token budgets with priority-based trimming."""

from dataclasses import dataclass, field
from typing import Optional
from .profiler import estimate_tokens


# Priority levels: P1=highest (never trim), P5=lowest (trim first)
PRIORITY_ORDER = ["P1", "P2", "P3", "P4", "P5"]


@dataclass
class SectionBudget:
    """Budget allocation for a single context section."""
    name: str
    tokens: int
    priority: str = "P3"
    strategy: str = "full"  # full, truncate, summarize, lazy, omit


@dataclass
class AgentBudget:
    """Token budget configuration for an agent."""
    agent_id: str
    max_tokens: int = 30000
    sections: list[SectionBudget] = field(default_factory=list)
    warn_threshold: float = 0.8  # warn at 80% usage

    @property
    def total_allocated(self) -> int:
        return sum(s.tokens for s in self.sections)

    @property
    def utilization(self) -> float:
        return self.total_allocated / self.max_tokens if self.max_tokens else 0

    @property
    def is_over_budget(self) -> bool:
        return self.total_allocated > self.max_tokens

    @property
    def remaining(self) -> int:
        return self.max_tokens - self.total_allocated


def create_budget_from_profile(agent_id: str, profile: dict,
                                max_tokens: int = 30000) -> AgentBudget:
    """Create budget from profiler output."""
    sections = []
    for s in profile.get("sections", []):
        priority = _assign_priority(s["pct"])
        strategy = _assign_strategy(priority, s["pct"])
        sections.append(SectionBudget(
            name=s["section"],
            tokens=s["tokens"],
            priority=priority,
            strategy=strategy,
        ))
    return AgentBudget(agent_id=agent_id, max_tokens=max_tokens, sections=sections)


def _assign_priority(pct: float) -> str:
    """Assign priority based on section size."""
    if pct < 5:
        return "P5"
    elif pct < 10:
        return "P4"
    elif pct < 20:
        return "P3"
    elif pct < 30:
        return "P2"
    return "P1"


def _assign_strategy(priority: str, pct: float) -> str:
    """Assign trimming strategy based on priority and size."""
    if priority == "P1":
        return "full"
    elif priority == "P2":
        return "truncate" if pct > 25 else "full"
    elif priority == "P3":
        return "summarize" if pct > 15 else "truncate"
    elif priority == "P4":
        return "lazy"
    return "omit"


def trim_to_budget(budget: AgentBudget) -> dict:
    """Trim sections to fit within budget. Returns trimming plan."""
    if not budget.is_over_budget:
        return {
            "action": "none",
            "total": budget.total_allocated,
            "max": budget.max_tokens,
            "remaining": budget.remaining,
        }

    excess = budget.total_allocated - budget.max_tokens
    trimmed = []
    remaining_excess = excess

    # Trim lowest priority first
    for priority in reversed(PRIORITY_ORDER):
        if remaining_excess <= 0:
            break
        for section in budget.sections:
            if section.priority == priority and remaining_excess > 0:
                if section.strategy == "omit":
                    trimmed.append({"section": section.name, "action": "omit",
                                    "saved": section.tokens})
                    remaining_excess -= section.tokens
                elif section.strategy == "lazy":
                    saved = int(section.tokens * 0.8)
                    trimmed.append({"section": section.name, "action": "lazy",
                                    "saved": saved})
                    remaining_excess -= saved
                elif section.strategy == "summarize":
                    saved = int(section.tokens * 0.5)
                    trimmed.append({"section": section.name, "action": "summarize",
                                    "saved": saved})
                    remaining_excess -= saved
                elif section.strategy == "truncate":
                    saved = int(section.tokens * 0.3)
                    trimmed.append({"section": section.name, "action": "truncate",
                                    "saved": saved})
                    remaining_excess -= saved

    total_saved = sum(t["saved"] for t in trimmed)
    return {
        "action": "trimmed",
        "excess": excess,
        "total_saved": total_saved,
        "sections_trimmed": trimmed,
        "new_total": budget.total_allocated - total_saved,
    }


def format_budget(budget: AgentBudget) -> str:
    """Format budget as readable report."""
    lines = [
        f"# Token Budget: {budget.agent_id}",
        f"Max: {budget.max_tokens} | Allocated: {budget.total_allocated} | "
        f"Remaining: {budget.remaining} | Utilization: {budget.utilization:.0%}",
        "",
        f"{'Section':<45} {'Tokens':>8} {'Priority':>8} {'Strategy':>10}",
        "-" * 75,
    ]
    for s in budget.sections:
        lines.append(f"{s.name[:45]:<45} {s.tokens:>8} {s.priority:>8} {s.strategy:>10}")
    return "\n".join(lines)
