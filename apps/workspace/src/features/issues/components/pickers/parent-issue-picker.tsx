"use client";

import { useMemo, useState } from "react";
import { GitBranchPlus, GitCommitHorizontal, GitMerge } from "lucide-react";
import type { IssueReference, UpdateIssueRequest } from "@/shared/types";
import { useIssueStore } from "@/features/issues";
import { useIssuesListQuery } from "@/features/issues/queries";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "./property-picker";

function collectDescendantIssueIds(issueId: string, issues: IssueReference[]): Set<string> {
  const descendants = new Set<string>();
  const queue = [issueId];

  while (queue.length > 0) {
    const currentIssueId = queue.shift();
    if (!currentIssueId) continue;

    issues.forEach((issue) => {
      if (issue.parent_issue_id === currentIssueId && !descendants.has(issue.id)) {
        descendants.add(issue.id);
        queue.push(issue.id);
      }
    });
  }

  return descendants;
}

export function ParentIssuePicker({
  issueId,
  parentIssueId,
  parentIssue,
  onUpdate,
  align,
}: {
  issueId?: string;
  parentIssueId: string | null;
  parentIssue?: IssueReference | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  align?: "start" | "center" | "end";
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const storeIssues = useIssueStore((state) => state.issues);
  const { data } = useIssuesListQuery();
  const issues = (data?.issues ?? storeIssues) as IssueReference[];

  const excludedIds = useMemo(() => {
    if (!issueId) return new Set<string>();
    return new Set<string>([issueId, ...collectDescendantIssueIds(issueId, issues)]);
  }, [issueId, issues]);

  const filteredIssues = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return issues.filter((issue) => {
      if (excludedIds.has(issue.id)) return false;
      if (!query) return true;
      return issue.title.toLowerCase().includes(query) || issue.identifier.toLowerCase().includes(query);
    });
  }, [excludedIds, filter, issues]);

  const selectedParent = issues.find((issue) => issue.id === parentIssueId) ?? parentIssue ?? null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setFilter("");
      }}
      width="w-72"
      align={align ?? "end"}
      searchable
      searchPlaceholder="Search parent issue..."
      onSearchChange={setFilter}
      trigger={selectedParent ? (
        <>
          <GitMerge className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{selectedParent.identifier} · {selectedParent.title}</span>
        </>
      ) : (
        <span className="text-muted-foreground">No parent</span>
      )}
    >
      <PickerItem
        selected={!parentIssueId}
        onClick={() => {
          onUpdate({ parent_issue_id: null });
          setOpen(false);
        }}
      >
        <GitBranchPlus className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">No parent</span>
      </PickerItem>

      {filteredIssues.map((issue) => (
        <PickerItem
          key={issue.id}
          selected={issue.id === parentIssueId}
          onClick={() => {
            onUpdate({ parent_issue_id: issue.id });
            setOpen(false);
          }}
        >
          <GitCommitHorizontal className="h-3.5 w-3.5 text-muted-foreground" />
          <span className="flex min-w-0 flex-1 flex-col items-start">
            <span className="truncate">{issue.title}</span>
            <span className="truncate text-[11px] text-muted-foreground">{issue.identifier}</span>
          </span>
        </PickerItem>
      ))}

      {filteredIssues.length === 0 ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}
