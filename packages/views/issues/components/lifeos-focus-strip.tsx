"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import type { Issue } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

export const LIFEOS_FOCUS_VALUES = [
  "all",
  "decision",
  "chairman_action",
  "review",
  "ai_working",
  "waiting",
] as const;

export type LifeOSFocus = (typeof LIFEOS_FOCUS_VALUES)[number];

const LIFEOS_FOCUS_SET = new Set<string>(LIFEOS_FOCUS_VALUES);

export function lifeOSFocusForIssue(
  issue: Issue,
): Exclude<LifeOSFocus, "all"> | "closed" {
  const explicit = issue.metadata.lifeos_attention_type;
  if (
    explicit === "decision" ||
    explicit === "chairman_action" ||
    explicit === "review" ||
    explicit === "ai_working" ||
    explicit === "waiting"
  ) {
    return explicit;
  }
  if (issue.status === "in_review") return "review";
  if (issue.status === "blocked") {
    return issue.assignee_type === "member" ? "chairman_action" : "waiting";
  }
  if (issue.status === "todo" || issue.status === "in_progress") {
    if (issue.assignee_type === "agent") return "ai_working";
    if (issue.assignee_type === "member") return "chairman_action";
    return "waiting";
  }
  if (issue.status === "backlog") return "waiting";
  return "closed";
}

function focusFromLocation(): LifeOSFocus {
  if (typeof window === "undefined") return "all";
  const value = new URLSearchParams(window.location.search).get("focus");
  return value && LIFEOS_FOCUS_SET.has(value) ? (value as LifeOSFocus) : "all";
}

export function useLifeOSFocus() {
  const [focus, setFocusState] = useState<LifeOSFocus>("all");

  useEffect(() => {
    const handlePopState = () => setFocusState(focusFromLocation());
    handlePopState();
    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, []);

  const setFocus = useCallback((next: LifeOSFocus) => {
    const url = new URL(window.location.href);
    if (next === "all") url.searchParams.delete("focus");
    else url.searchParams.set("focus", next);
    window.history.replaceState(window.history.state, "", url);
    setFocusState(next);
  }, []);

  return [focus, setFocus] as const;
}

export function LifeOSFocusStrip({
  issues,
  value,
  onChange,
}: {
  issues: Issue[];
  value: LifeOSFocus;
  onChange: (value: LifeOSFocus) => void;
}) {
  const { t } = useT("issues");
  const counts = useMemo(() => {
    const next = new Map<LifeOSFocus, number>();
    next.set("all", issues.length);
    for (const issue of issues) {
      const focus = lifeOSFocusForIssue(issue);
      if (focus !== "closed") next.set(focus, (next.get(focus) ?? 0) + 1);
    }
    return next;
  }, [issues]);
  const labels: Record<LifeOSFocus, string> = {
    all: t(($) => $.focus.all),
    decision: t(($) => $.focus.decision),
    chairman_action: t(($) => $.focus.chairman_action),
    review: t(($) => $.focus.review),
    ai_working: t(($) => $.focus.ai_working),
    waiting: t(($) => $.focus.waiting),
  };

  return (
    <nav
      aria-label={t(($) => $.focus.label)}
      className="flex shrink-0 items-center gap-1 overflow-x-auto border-b bg-background px-3 py-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
    >
      {LIFEOS_FOCUS_VALUES.map((focus) => {
        const selected = value === focus;
        return (
          <button
            key={focus}
            type="button"
            aria-pressed={selected}
            onClick={() => onChange(focus)}
            className={cn(
              "inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              selected
                ? "bg-foreground text-background"
                : "text-muted-foreground hover:bg-muted hover:text-foreground",
            )}
          >
            <span>{labels[focus]}</span>
            <span className="min-w-4 text-center tabular-nums opacity-70">
              {counts.get(focus) ?? 0}
            </span>
          </button>
        );
      })}
    </nav>
  );
}
