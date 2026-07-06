"use client";

// FIR-2742 — top-of-page alert on the Skills page: when the current user owns
// or approves skills that have pending change requests, surface a single
// banner with a count and a one-click entry into the review sheet, so they can
// handle them one at a time instead of hunting skill by skill.

import { GitPullRequestArrow } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

interface Props {
  /** Number of pending changes on skills the user owns/approves. */
  count: number;
  onReview: () => void;
}

export function SkillChangesAlert({ count, onReview }: Props) {
  if (count <= 0) return null;
  return (
    <div
      role="status"
      className="flex shrink-0 items-center gap-2 border-b bg-warning/10 px-6 py-2 text-xs text-foreground"
    >
      <GitPullRequestArrow className="h-3.5 w-3.5 shrink-0 text-warning" />
      <span className="min-w-0 flex-1">
        <span className="font-medium">
          {count} pending {count === 1 ? "change" : "changes"}
        </span>{" "}
        <span className="text-muted-foreground">
          awaiting your review on skills you own.
        </span>
      </span>
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="h-7 shrink-0"
        onClick={onReview}
      >
        Review
      </Button>
    </div>
  );
}
