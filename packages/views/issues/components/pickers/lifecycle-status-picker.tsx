"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Issue, IssueLifecyclePhase, IssueStatusCategory } from "@multica/core/types";
import { issueLifecycleOptions } from "@multica/core/issue-lifecycles";
import { useTransitionIssueStatusNode } from "@multica/core/issues/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { STATUS_CONFIG } from "@multica/core/issues/config";
import { PropertyPicker, PickerItem } from "./property-picker";
import { StatusIcon } from "../status-icon";
import { useT } from "../../../i18n";

const SEARCH_THRESHOLD = 9;

function categoryForPhase(phase: IssueLifecyclePhase | (string & {})): IssueStatusCategory {
  switch (phase) {
    case "backlog": return "backlog";
    case "unstarted": return "todo";
    case "completed": return "done";
    case "cancelled": return "cancelled";
    default: return "in_progress";
  }
}

/** Stable-node status picker for one concrete, lifecycle-pinned issue. */
export function LifecycleStatusPicker({ issue, align = "start" }: {
  issue: Issue;
  align?: "start" | "center" | "end";
}) {
  const wsId = useWorkspaceId();
  const lifecycleId = issue.lifecycle_id ?? "";
  const { data } = useQuery(issueLifecycleOptions(wsId, lifecycleId));
  const transition = useTransitionIssueStatusNode();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { t } = useT("issues");

  const activeStatuses = useMemo(
    () => (data?.statuses ?? []).filter((status) => !status.archived_at),
    [data?.statuses],
  );
  const current = data?.statuses.find((status) => status.id === issue.lifecycle_status_id);
  const options = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return normalized
      ? activeStatuses.filter((status) => status.name.toLowerCase().includes(normalized))
      : activeStatuses;
  }, [activeStatuses, query]);

  if (!lifecycleId || !data) return null;
  const currentCategory = categoryForPhase(current?.phase ?? "started");

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(next) => {
        if (!next) setQuery("");
        setOpen(next);
      }}
      width="w-56"
      align={align}
      searchable={activeStatuses.length > SEARCH_THRESHOLD}
      searchPlaceholder={t(($) => $.filters.search_status)}
      onSearchChange={setQuery}
      trigger={current ? (
        <>
          <StatusIcon
            status={current.legacy_status_key ?? issue.status}
            category={currentCategory}
            color={current.color}
            className="h-3.5 w-3.5 shrink-0"
          />
          <span className="truncate">{current.name}</span>
        </>
      ) : null}
    >
      {options.map((status) => {
        const category = categoryForPhase(status.phase);
        return (
          <PickerItem
            key={status.id}
            selected={status.id === issue.lifecycle_status_id}
            hoverClassName={STATUS_CONFIG[category].hoverBg}
            onClick={() => {
              if (status.id !== issue.lifecycle_status_id) {
                transition.mutate({
                  id: issue.id,
                  lifecycle_status_id: status.id,
                  expected_revision: issue.revision,
                  expected_transition_id: issue.transition_id ?? undefined,
                });
              }
              setOpen(false);
              setQuery("");
            }}
          >
            <StatusIcon
              status={status.legacy_status_key ?? issue.status}
              category={category}
              color={status.color}
              className="h-3.5 w-3.5"
            />
            <span className="truncate">{status.name}</span>
          </PickerItem>
        );
      })}
    </PropertyPicker>
  );
}
