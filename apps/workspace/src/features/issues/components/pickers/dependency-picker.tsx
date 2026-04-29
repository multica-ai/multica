"use client";

import { useMemo, useState } from "react";
import { Link2 } from "lucide-react";
import type { IssueDependencyGroups, IssueReference, IssueDependencyType } from "@/shared/types";
import { useIssueStore } from "@/features/issues";
import { useIssuesListQuery } from "@/features/issues/queries";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "./property-picker";

const RELATION_LABEL: Record<IssueDependencyType, string> = {
  blocks: "Blocks",
  blocked_by: "Blocked by",
  related: "Related",
};

export function DependencyPicker({
  issueId,
  dependencies,
  type,
  onAdd,
}: {
  issueId: string;
  dependencies: IssueDependencyGroups | null | undefined;
  type: IssueDependencyType;
  onAdd: (dependencyIssueId: string, dependencyType: IssueDependencyType) => Promise<unknown>;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const storeIssues = useIssueStore((state) => state.issues);
  const { data } = useIssuesListQuery();
  const issues = (data?.issues ?? storeIssues) as IssueReference[];

  const relatedIssueIds = useMemo(() => {
    const ids = new Set<string>([issueId]);
    (dependencies?.blocks ?? []).forEach((entry) => ids.add(entry.issue.id));
    (dependencies?.blocked_by ?? []).forEach((entry) => ids.add(entry.issue.id));
    (dependencies?.related ?? []).forEach((entry) => ids.add(entry.issue.id));
    return ids;
  }, [dependencies, issueId]);

  const filteredIssues = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return issues.filter((issue) => {
      if (relatedIssueIds.has(issue.id)) return false;
      if (!query) return true;
      return issue.title.toLowerCase().includes(query) || issue.identifier.toLowerCase().includes(query);
    });
  }, [filter, issues, relatedIssueIds]);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setFilter("");
      }}
      width="w-72"
      align="start"
      searchable
      searchPlaceholder={`Add ${RELATION_LABEL[type].toLowerCase()} issue...`}
      onSearchChange={setFilter}
      trigger={(
        <span className="inline-flex items-center gap-1 rounded border px-2 py-1 text-[11px] text-muted-foreground">
          <Link2 className="h-3 w-3" />
          {RELATION_LABEL[type]}
        </span>
      )}
    >
      {filteredIssues.map((issue) => (
        <PickerItem
          key={issue.id}
          selected={false}
          onClick={async () => {
            await onAdd(issue.id, type);
            setOpen(false);
          }}
        >
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
