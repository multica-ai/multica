"use client";

// FIR-2742 / FIR-2748 — top-of-page alert on the Skills page. When the current
// user owns or approves skills that have pending change requests, this is the
// single entry point for reviewing them: it shows a personal count and carries
// the review controls (open the review sheet, or filter the list to the skills
// awaiting review), so the user handles their own queue one at a time without
// hunting skill by skill. It deliberately replaces the generic intro banner in
// that slot — see skills-page.tsx — because for an owner with pending reviews,
// their own queue is the more useful thing to surface at the top of the page.

import { GitPullRequestArrow } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

interface Props {
  /** Number of pending changes on skills the current user owns/approves. */
  count: number;
  /** Open the cross-skill review sheet (scoped to the user's own skills). */
  onReview: () => void;
  /** Whether the skills list is currently filtered to skills awaiting review. */
  pendingOnly: boolean;
  /** Toggle the list-level "skills awaiting review" filter. */
  onTogglePending: () => void;
}

export function SkillChangesAlert({
  count,
  onReview,
  pendingOnly,
  onTogglePending,
}: Props) {
  if (count <= 0) return null;
  return (
    <div
      role="status"
      className="flex shrink-0 flex-wrap items-center gap-x-2 gap-y-1.5 border-b bg-warning/10 px-4 py-2 text-xs text-foreground sm:px-6"
    >
      <GitPullRequestArrow className="h-3.5 w-3.5 shrink-0 text-warning" />
      <span className="min-w-0 flex-1">
        <span className="font-medium">
          {count} {count === 1 ? "change" : "changes"} to review
        </span>{" "}
        <span className="text-muted-foreground">on skills you own.</span>
      </span>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className={
          pendingOnly
            ? "h-7 shrink-0 bg-accent text-accent-foreground hover:bg-accent/80"
            : "h-7 shrink-0 text-muted-foreground"
        }
        onClick={onTogglePending}
      >
        Pending changes
      </Button>
      <Button
        type="button"
        size="sm"
        className="h-7 shrink-0"
        onClick={onReview}
      >
        Review
      </Button>
    </div>
  );
}
