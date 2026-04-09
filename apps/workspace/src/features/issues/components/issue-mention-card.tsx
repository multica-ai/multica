"use client";

import { useIssueStore } from "@/features/issues/store";
import { Link } from "@/shared/router";
import { StatusIcon } from "./status-icon";

interface IssueMentionCardProps {
  issueId: string;
  /** Fallback text when issue is not in store (e.g. "MUL-7") */
  fallbackLabel?: string;
}

export function IssueMentionCard({ issueId, fallbackLabel }: IssueMentionCardProps) {
  const issue = useIssueStore((s) => s.issues.find((i) => i.id === issueId));

  if (!issue) {
    return (
      <Link
        href={`/issues/${issueId}`}
        className="text-primary font-medium cursor-pointer hover:underline"
      >
        {fallbackLabel ?? issueId.slice(0, 8)}
      </Link>
    );
  }

  return (
    <Link
      href={`/issues/${issueId}`}
      className="inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-sm hover:bg-accent transition-colors cursor-pointer no-underline"
    >
      <StatusIcon status={issue.status} className="h-3.5 w-3.5" />
      <span className="font-medium text-muted-foreground">{issue.identifier}</span>
      <span className="text-foreground">{issue.title}</span>
    </Link>
  );
}
