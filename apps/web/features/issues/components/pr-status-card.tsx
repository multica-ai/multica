"use client";

import { useState, useEffect, useCallback } from "react";
import { GitPullRequest, ExternalLink } from "lucide-react";
import type { PullRequest, PullRequestStatus } from "@/shared/types";
import { api } from "@/shared/api";
import { useWSEvent, useWSReconnect } from "@/features/realtime";

const STATUS_CONFIG: Record<PullRequestStatus, { label: string; color: string; bg: string }> = {
  open: { label: "Open", color: "text-green-600", bg: "bg-green-100 dark:bg-green-900/30" },
  draft: { label: "Draft", color: "text-muted-foreground", bg: "bg-muted" },
  merged: { label: "Merged", color: "text-purple-600", bg: "bg-purple-100 dark:bg-purple-900/30" },
  closed: { label: "Closed", color: "text-red-600", bg: "bg-red-100 dark:bg-red-900/30" },
};

function PRStatusBadge({ status }: { status: PullRequestStatus }) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.open;
  return (
    <span className={`inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium ${cfg.bg} ${cfg.color}`}>
      {cfg.label}
    </span>
  );
}

interface PRStatusCardProps {
  issueId: string;
}

export function PRStatusCard({ issueId }: PRStatusCardProps) {
  const [prs, setPrs] = useState<PullRequest[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchPRs = useCallback(async () => {
    try {
      const data = await api.listIssuePullRequests(issueId);
      setPrs(data);
    } catch {
      // Silently fail — PRs are optional metadata
    } finally {
      setLoading(false);
    }
  }, [issueId]);

  useEffect(() => {
    fetchPRs();
  }, [fetchPRs]);

  // Refetch on real-time PR events
  const handlePREvent = useCallback(
    (payload: unknown) => {
      const data = payload as { issue_id?: string };
      if (data?.issue_id === issueId) {
        fetchPRs();
      }
    },
    [issueId, fetchPRs],
  );

  useWSEvent("pull_request:linked", handlePREvent);
  useWSEvent("pull_request:updated", handlePREvent);
  useWSReconnect(fetchPRs);

  if (loading || prs.length === 0) return null;

  return (
    <div className="space-y-1.5">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground px-2 -mx-2">
        <GitPullRequest className="h-3.5 w-3.5" />
        <span>Pull Requests</span>
      </div>
      {prs.map((pr) => (
        <a
          key={pr.id}
          href={pr.url}
          target="_blank"
          rel="noopener noreferrer"
          className="group flex items-start gap-2 rounded-md px-2 py-1.5 -mx-2 hover:bg-accent/50 transition-colors"
        >
          <PRStatusBadge status={pr.status} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1">
              <span className="text-xs truncate font-medium">{pr.title}</span>
              <ExternalLink className="h-3 w-3 shrink-0 opacity-0 group-hover:opacity-60 transition-opacity" />
            </div>
            <span className="text-[10px] text-muted-foreground">
              {pr.repo_owner}/{pr.repo_name}#{pr.pr_number}
            </span>
          </div>
        </a>
      ))}
    </div>
  );
}
