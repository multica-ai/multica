"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, ChevronRight, Plus, Search } from "@multica/ui/icons";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useIssueStatuses } from "@multica/core/issue-statuses";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { StatusIcon } from "../../issues/components/status-icon";
import { DONE_LIMIT, WORK_GROUPS, groupAgentWork, type WorkGroup } from "../lib/agent-work";

/**
 * The agent's work, sorted for the question an admin asks: what is it doing,
 * what is queued, what could it be given, and what has it finished. A click on a
 * row opens that issue's thread; an issue nobody has started can be handed over
 * from its row.
 */
export function CompanyAgentWork({
  agentId,
  issues,
  selectedIssueId,
  onSelect,
  onNew,
}: {
  agentId: string;
  issues: readonly Issue[];
  selectedIssueId: string | null;
  onSelect: (issueId: string) => void;
  onNew: () => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const update = useUpdateIssue();
  const [query, setQuery] = useState("");
  const [doneOpen, setDoneOpen] = useState(false);

  const groups = useMemo(
    () => groupAgentWork(issues, agentId, catalog.categoryOf, query),
    [issues, agentId, catalog.categoryOf, query],
  );
  const name = agents.find((agent) => agent.id === agentId)?.name ?? "";

  const assign = (issue: Issue) =>
    update.mutate(
      { id: issue.id, assignee_type: "agent", assignee_id: agentId },
      {
        onSuccess: () => toast.success(t(($) => $.company.assign_done, { identifier: issue.identifier, name })),
        onError: () => toast.error(t(($) => $.company.assign_failed)),
      },
    );

  const groupLabels: Record<WorkGroup, string> = {
    active: t(($) => $.company.group_active),
    next: t(($) => $.company.group_next),
    assignable: t(($) => $.company.group_assignable),
    done: t(($) => $.company.group_done),
  };
  const empty = WORK_GROUPS.every((group) => groups[group].length === 0);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b p-2">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t(($) => $.company.work_search)}
            aria-label={t(($) => $.company.work_search)}
            className="h-8 pl-7"
          />
        </div>
        <Button type="button" size="sm" onClick={onNew}>
          <Plus className="size-4" />
          {t(($) => $.company.new_issue)}
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-1">
        {empty ? <p className="px-2 py-3 text-caption text-muted-foreground">{t(($) => $.company.work_empty)}</p> : null}
        {WORK_GROUPS.map((group) => {
          const all = groups[group];
          if (all.length === 0) return null;
          const collapsed = group === "done" && !doneOpen && !query;
          const shown = group === "done" ? all.slice(0, DONE_LIMIT) : all;
          return (
            <section key={group} aria-label={groupLabels[group]} className="mb-1">
              <h3 className="m-0">
                <button
                  type="button"
                  aria-expanded={!collapsed}
                  disabled={group !== "done"}
                  onClick={() => setDoneOpen((open) => !open)}
                  className="flex w-full items-center gap-1 px-2 py-1 text-left text-caption font-semibold text-muted-foreground disabled:cursor-default"
                >
                  {group === "done" ? collapsed ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" /> : null}
                  {groupLabels[group]}
                  <span className="font-normal">{all.length}</span>
                </button>
              </h3>
              {collapsed ? null : (
                <ul>
                  {shown.map((issue) => (
                    <li key={issue.id}>
                      <WorkRow
                        issue={issue}
                        selected={issue.id === selectedIssueId}
                        assignable={group === "assignable"}
                        pending={update.isPending}
                        assigneeName={
                          issue.assignee_type === "agent"
                            ? agents.find((agent) => agent.id === issue.assignee_id)?.name
                            : undefined
                        }
                        onSelect={() => onSelect(issue.id)}
                        onAssign={() => assign(issue)}
                      />
                    </li>
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}

function WorkRow({
  issue,
  selected,
  assignable,
  pending,
  assigneeName,
  onSelect,
  onAssign,
}: {
  issue: Issue;
  selected: boolean;
  assignable: boolean;
  pending: boolean;
  /** Who holds the issue now, when it is listed as work to hand over. */
  assigneeName?: string;
  onSelect: () => void;
  onAssign: () => void;
}) {
  const { t } = useT("issues");
  return (
    <div className={cn("group flex items-center gap-1 rounded-md pr-1 hover:bg-accent", selected && "bg-accent")}>
      <button
        type="button"
        aria-current={selected || undefined}
        onClick={onSelect}
        className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left"
      >
        <StatusIcon status={issue.status} className="size-4 shrink-0" />
        <PriorityIcon priority={issue.priority} className="shrink-0" />
        <span className="shrink-0 font-mono text-micro text-muted-foreground">{issue.identifier}</span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-body">{issue.title}</span>
          {assignable && assigneeName ? (
            <span className="block truncate text-micro text-muted-foreground">
              {t(($) => $.company.held_by, { name: assigneeName })}
            </span>
          ) : null}
        </span>
      </button>
      {assignable ? (
        <Button type="button" variant="outline" size="xs" disabled={pending} onClick={onAssign}>
          {t(($) => $.company.assign_action)}
        </Button>
      ) : null}
    </div>
  );
}
