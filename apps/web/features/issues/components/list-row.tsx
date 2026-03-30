"use client";

import Link from "next/link";
import type { Issue } from "@/shared/types";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { PriorityIcon } from "./priority-icon";

function formatDate(date: string): string {
  return new Date(date).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
}

export function ListRow({ issue }: { issue: Issue }) {
  const selected = useIssueSelectionStore((s) => s.selectedIds.has(issue.id));
  const toggle = useIssueSelectionStore((s) => s.toggle);

  return (
    <div
      className={`group/row flex h-9 items-center gap-2 px-4 text-sm transition-colors hover:bg-accent/50 ${
        selected ? "bg-accent/30" : ""
      }`}
    >
      <div className="relative flex shrink-0 items-center justify-center w-4 h-4">
        <PriorityIcon
          priority={issue.priority}
          className={selected ? "hidden" : "group-hover/row:hidden"}
        />
        <input
          type="checkbox"
          checked={selected}
          onChange={() => toggle(issue.id)}
          className={`absolute inset-0 cursor-pointer accent-primary ${
            selected ? "" : "hidden group-hover/row:block"
          }`}
        />
      </div>
      <Link
        href={`/issues/${issue.id}`}
        className="flex flex-1 items-center gap-2 min-w-0"
      >
        <span className="w-16 shrink-0 text-xs text-muted-foreground">
          {issue.identifier}
        </span>
        <span className="min-w-0 flex-1 truncate">{issue.title}</span>
        {issue.due_date && (
          <span className="shrink-0 text-xs text-muted-foreground">
            {formatDate(issue.due_date)}
          </span>
        )}
        {issue.assignee_type && issue.assignee_id && (
          <ActorAvatar
            actorType={issue.assignee_type}
            actorId={issue.assignee_id}
            size={20}
          />
        )}
      </Link>
    </div>
  );
}
