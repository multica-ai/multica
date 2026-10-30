"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { CalendarClock, CalendarX, FolderMinus, Lock, Network, UserMinus, Zap } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { dispatchReasonCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import { addDaysDateOnly, todayDateOnly } from "@multica/core/issues/date";
import {
  sharedWorkflowStatusKeys,
  stepHandsOff,
  useIssueWorkflows,
  workflowStep,
} from "@multica/core/issue-workflows";
import { issueLabelsOptions, labelListOptions } from "@multica/core/labels/queries";
import { useAttachLabelToIssue, useDetachLabel } from "@multica/core/labels/mutations";
import { useModalStore } from "@multica/core/modals";
import { projectListOptions } from "@multica/core/projects/queries";
import { quickActionListOptions, useRunQuickAction } from "@multica/core/quick-actions";
import type { Issue, IssuePriority, Label } from "@multica/core/types";
import { StatusIcon } from "../../issues/components/status-icon";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { outcomeMessage } from "../../issues/components/quick-actions-section";
import { useIssueStatusOptions } from "../../issues/utils/status-options";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { HighlightText } from "../highlight-text";
import { useIssueTargetWriter, targetIssues } from "../issue-target";
import { scoreRow, scoreText } from "../matching";
import { usePalette } from "../palette-context";
import { PaletteGroup, ROW_CLASS, ROW_ICON_CLASS, RowCheck, RowHint } from "../rows";
import { AgentPresenceText, ActorRow, IssueResultRow } from "../result-rows";
import { useActorDirectory } from "../use-actor-directory";
import { useServerSearch } from "../use-server-search";
import type { IssueTarget } from "../types";

/** The value every target issue shares, or undefined when they differ. */
function common<T>(issues: Issue[], pick: (issue: Issue) => T): T | undefined {
  const first = issues[0];
  if (!first) return undefined;
  const value = pick(first);
  return issues.every((issue) => pick(issue) === value) ? value : undefined;
}

function sharedProjectIdOf(issues: Issue[]): string | null | undefined {
  const first = issues[0]?.project_id ?? null;
  return first && issues.every((i) => i.project_id === first) ? first : undefined;
}

function filterByLabel<T>(items: T[], query: string, label: (item: T) => string, keywords?: (item: T) => string[]) {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items
    .map((item) => ({ item, score: scoreRow(label(item), keywords?.(item) ?? [], q) }))
    .filter((entry) => entry.score > 0)
    .sort((a, b) => b.score - a.score)
    .map((entry) => entry.item);
}

export function IssueStatusPage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const openModal = useModalStore((s) => s.open);
  const writer = useIssueTargetWriter(target);
  const issues = writer.issues;
  const single = target.kind === "single" ? target.issue : null;
  const sharedProjectId = single ? single.project_id : sharedProjectIdOf(issues);
  const { workflows } = useIssueWorkflows(wsId);
  const { data: projects } = useQuery({
    ...projectListOptions(wsId),
    enabled: !single && sharedProjectId === undefined,
  });
  // A selection inside one project offers that project's workflow statuses;
  // one spanning projects offers the statuses every selected issue's
  // workflow allows — the batch toolbar's rule. (MUL-7420)
  const { options, workflow } = useIssueStatusOptions(
    wsId,
    single ? single.project_id : (sharedProjectId ?? undefined),
    single?.status,
  );
  const allowed = useMemo(
    () => (single || sharedProjectId ? null : sharedWorkflowStatusKeys(workflows, projects, issues)),
    [issues, projects, sharedProjectId, single, workflows],
  );
  const current = common(issues, (issue) => issue.status);
  const visible = filterByLabel(
    allowed ? options.filter((o) => allowed.has(o.key)) : options,
    query,
    (o) => o.label,
    (o) => [o.key.replace(/_/g, " ")],
  );

  const select = (status: string) => {
    palette.close();
    if (
      single &&
      workflow &&
      status !== single.status &&
      stepHandsOff(workflowStep(workflow, status))
    ) {
      openModal("issue-workflow-handoff", {
        issueId: single.id,
        identifier: single.identifier,
        status,
      });
      return;
    }
    writer.apply({ status });
  };

  return (
    <PaletteGroup heading={t(($) => $.pages.status)}>
      {visible.map((option) => (
        <CommandPrimitive.Item
          key={option.key}
          value={`status:${option.key}`}
          onSelect={() => select(option.key)}
          className={ROW_CLASS}
        >
          <StatusIcon
            status={option.key}
            category={option.category}
            color={option.color}
            icon={option.icon}
            className="size-4 shrink-0"
          />
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={option.label} query={query} />
          </span>
          {current === option.key && <RowCheck label={t(($) => $.pages.current)} />}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

export function IssuePriorityPage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const palette = usePalette();
  const writer = useIssueTargetWriter(target);
  const current = common(writer.issues, (issue) => issue.priority);
  const options = filterByLabel(
    PRIORITY_DISPLAY_ORDER.map((priority) => ({ priority, label: tIssues(($) => $.priority[priority]) })),
    query,
    (o) => o.label,
    (o) => [o.priority],
  );
  return (
    <PaletteGroup heading={t(($) => $.pages.priority)}>
      {options.map(({ priority, label }) => (
        <CommandPrimitive.Item
          key={priority}
          value={`priority:${priority}`}
          onSelect={() => {
            palette.close();
            writer.apply({ priority: priority as IssuePriority });
          }}
          className={ROW_CLASS}
        >
          <PriorityIcon priority={priority} className="size-4 shrink-0" />
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={label} query={query} />
          </span>
          {current === priority && <RowCheck label={t(($) => $.pages.current)} />}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

export function IssueAssigneePage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const palette = usePalette();
  const writer = useIssueTargetWriter(target);
  const { agents, squads, members } = useActorDirectory(true);
  const currentType = common(writer.issues, (issue) => issue.assignee_type);
  const currentId = common(writer.issues, (issue) => issue.assignee_id);
  const isCurrent = (type: string, id: string) => currentType === type && currentId === id;
  const q = query.trim().toLowerCase();

  // Agents ordered: assignable before not, online before offline. Search
  // keeps that order inside each match tier.
  const availabilityRank: Record<string, number> = { online: 0, unstable: 1, offline: 2 };
  const sortedAgents = [...agents].sort((a, b) => {
    if (a.assignable !== b.assignable) return a.assignable ? -1 : 1;
    const ra = a.availability ? (availabilityRank[a.availability] ?? 3) : 3;
    const rb = b.availability ? (availabilityRank[b.availability] ?? 3) : 3;
    return ra - rb;
  });
  const visibleAgents = filterByLabel(sortedAgents, q, (e) => e.agent.name);
  const visibleSquads = filterByLabel(squads, q, (e) => e.squad.name);
  const visibleMembers = filterByLabel(members, q, (m) => m.name, (m) => [m.email.toLowerCase()]);
  const unassignedLabel = tIssues(($) => $.pickers.assignee.trigger_unassigned);
  const showUnassigned = !q || scoreText(unassignedLabel, q) > 0;

  const select = (type: "member" | "agent" | "squad" | null, id: string | null) => {
    palette.close();
    writer.assign(type, id);
  };

  return (
    <>
      {showUnassigned && (
        <PaletteGroup heading={t(($) => $.pages.assignee)}>
          <CommandPrimitive.Item
            value="assignee:none"
            onSelect={() => select(null, null)}
            className={ROW_CLASS}
          >
            <UserMinus className={ROW_ICON_CLASS} />
            <span className="min-w-0 flex-1 truncate text-muted-foreground">{unassignedLabel}</span>
            {currentType === null && currentId === null && <RowCheck label={t(($) => $.pages.current)} />}
          </CommandPrimitive.Item>
        </PaletteGroup>
      )}
      {visibleAgents.length > 0 && (
        <PaletteGroup heading={tIssues(($) => $.pickers.assignee.agents_group)}>
          {visibleAgents.map((entry) => (
            <ActorRow
              key={entry.agent.id}
              value={`assignee:agent:${entry.agent.id}`}
              actorType="agent"
              actorId={entry.agent.id}
              name={entry.agent.name}
              query={query}
              showStatusDot
              disabled={!entry.assignable}
              onSelect={() => select("agent", entry.agent.id)}
              detail={
                entry.assignable ? (
                  <AgentPresenceText availability={entry.availability} runtimeLabel={entry.runtimeLabel} />
                ) : (
                  <span className="text-caption text-muted-foreground">
                    {entry.deniedReason ?? tIssues(($) => $.pickers.assignee.agent_runtime_required)}
                  </span>
                )
              }
              trailing={
                isCurrent("agent", entry.agent.id) ? (
                  <RowCheck label={t(($) => $.pages.current)} />
                ) : entry.agent.visibility === "private" ? (
                  <Lock className="ml-auto size-3 shrink-0 text-muted-foreground" />
                ) : null
              }
            />
          ))}
        </PaletteGroup>
      )}
      {visibleSquads.length > 0 && (
        <PaletteGroup heading={tIssues(($) => $.pickers.assignee.squads_group)}>
          {visibleSquads.map((entry) => (
            <ActorRow
              key={entry.squad.id}
              value={`assignee:squad:${entry.squad.id}`}
              actorType="squad"
              actorId={entry.squad.id}
              name={entry.squad.name}
              query={query}
              disabled={!entry.runnable}
              onSelect={() => select("squad", entry.squad.id)}
              detail={
                entry.runnable ? undefined : (
                  <span className="text-caption text-muted-foreground">
                    {tIssues(($) => $.pickers.assignee.squad_runtime_required)}
                  </span>
                )
              }
              trailing={isCurrent("squad", entry.squad.id) ? <RowCheck label={t(($) => $.pages.current)} /> : null}
            />
          ))}
        </PaletteGroup>
      )}
      {visibleMembers.length > 0 && (
        <PaletteGroup heading={tIssues(($) => $.pickers.assignee.members_group)}>
          {visibleMembers.map((member) => (
            <ActorRow
              key={member.user_id}
              value={`assignee:member:${member.user_id}`}
              actorType="member"
              actorId={member.user_id}
              name={member.name}
              query={query}
              onSelect={() => select("member", member.user_id)}
              trailing={
                isCurrent("member", member.user_id) ? <RowCheck label={t(($) => $.pages.current)} /> : null
              }
            />
          ))}
        </PaletteGroup>
      )}
    </>
  );
}

const EMPTY_LABELS: Label[] = [];

export function IssueLabelsPage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const issues = targetIssues(target);
  const single = target.kind === "single" ? target.issue : null;
  const { data: labels = EMPTY_LABELS } = useQuery(labelListOptions(wsId));
  const { data: singleLabels } = useQuery({
    ...issueLabelsOptions(wsId, single?.id ?? ""),
    enabled: !!single,
  });
  const attach = useAttachLabelToIssue();
  const detach = useDetachLabel(single?.id ?? "");
  const labelIdsOf = (issue: Issue) =>
    new Set((single && issue.id === single.id && singleLabels ? singleLabels : issue.labels ?? []).map((l) => l.id));
  const visible = filterByLabel(labels, query, (label) => label.name);

  const toggle = (label: Label) => {
    palette.close();
    const lacking = issues.filter((issue) => !labelIdsOf(issue).has(label.id));
    if (lacking.length === 0 && single) {
      // Every target already has it: on a single issue the row reads as a
      // checked toggle, so selecting it removes the label.
      detach.mutateAsync(label.id).then(
        () => toast.success(t(($) => $.toast.label_removed, { name: label.name })),
        (err: unknown) => toast.error(err instanceof Error ? err.message : String(err)),
      );
      return;
    }
    void Promise.allSettled(
      lacking.map((issue) => attach.mutateAsync({ issueId: issue.id, labelId: label.id })),
    ).then((results) => {
      const failed = results.filter((r) => r.status === "rejected").length;
      if (failed > 0) toast.error(t(($) => $.toast.label_failed, { name: label.name }));
      else toast.success(t(($) => $.toast.label_added, { name: label.name }));
    });
  };

  return (
    <PaletteGroup heading={t(($) => $.pages.labels)}>
      {visible.map((label) => {
        const onAll = issues.every((issue) => labelIdsOf(issue).has(label.id));
        return (
          <CommandPrimitive.Item
            key={label.id}
            value={`label:${label.id}`}
            onSelect={() => toggle(label)}
            className={ROW_CLASS}
          >
            <span
              className="mx-1 size-2.5 shrink-0 rounded-full"
              style={{ backgroundColor: label.color }}
            />
            <span className="min-w-0 flex-1 truncate">
              <HighlightText text={label.name} query={query} />
            </span>
            {onAll && <RowCheck label={t(($) => $.pages.current)} />}
          </CommandPrimitive.Item>
        );
      })}
      {visible.length === 0 && (
        <div className="px-2 py-3 text-caption text-muted-foreground">{t(($) => $.empty.no_labels)}</div>
      )}
    </PaletteGroup>
  );
}

export function IssueDueDatePage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const palette = usePalette();
  const writer = useIssueTargetWriter(target);
  const hasDueDate = writer.issues.some((issue) => !!issue.due_date);
  const today = todayDateOnly();
  const options = filterByLabel(
    [
      { id: "today", label: tIssues(($) => $.actions.due_today), value: today as string | null },
      { id: "tomorrow", label: tIssues(($) => $.actions.due_tomorrow), value: addDaysDateOnly(1) },
      { id: "next-week", label: tIssues(($) => $.actions.due_next_week), value: addDaysDateOnly(7) },
      ...(hasDueDate ? [{ id: "clear", label: tIssues(($) => $.actions.due_clear), value: null }] : []),
    ],
    query,
    (o) => o.label,
  );
  return (
    <PaletteGroup heading={t(($) => $.pages.due_date)}>
      {options.map((option) => (
        <CommandPrimitive.Item
          key={option.id}
          value={`due:${option.id}`}
          onSelect={() => {
            palette.close();
            writer.apply({ due_date: option.value });
          }}
          className={ROW_CLASS}
        >
          {option.value ? (
            <CalendarClock className={ROW_ICON_CLASS} />
          ) : (
            <CalendarX className={ROW_ICON_CLASS} />
          )}
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={option.label} query={query} />
          </span>
          {option.value && <RowHint>{option.value}</RowHint>}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

export function IssueProjectPage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const writer = useIssueTargetWriter(target);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = common(writer.issues, (issue) => issue.project_id);
  const live = projects.filter((project) => project.status !== "cancelled" || project.id === current);
  const visible = filterByLabel(live, query, (project) => project.title);
  const noProjectLabel = t(($) => $.pages.no_project);
  const q = query.trim().toLowerCase();
  return (
    <PaletteGroup heading={t(($) => $.pages.project)}>
      {(!q || scoreText(noProjectLabel, q) > 0) && (
        <CommandPrimitive.Item
          value="project:none"
          onSelect={() => {
            palette.close();
            writer.apply({ project_id: null });
          }}
          className={ROW_CLASS}
        >
          <FolderMinus className={ROW_ICON_CLASS} />
          <span className="min-w-0 flex-1 truncate text-muted-foreground">{noProjectLabel}</span>
          {current === null && <RowCheck label={t(($) => $.pages.current)} />}
        </CommandPrimitive.Item>
      )}
      {visible.map((project) => (
        <CommandPrimitive.Item
          key={project.id}
          value={`project:${project.id}`}
          onSelect={() => {
            palette.close();
            writer.apply({ project_id: project.id });
          }}
          className={ROW_CLASS}
        >
          <ProjectIcon project={project} size="md" />
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={project.title} query={query} />
          </span>
          {current === project.id && <RowCheck label={t(($) => $.pages.current)} />}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

/** Batch "Set parent issue": pick the parent by searching issues. */
export function IssueParentPage({ target, query }: { target: IssueTarget; query: string }) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const palette = usePalette();
  const writer = useIssueTargetWriter(target);
  const selectedIds = useMemo(() => new Set(writer.issues.map((issue) => issue.id)), [writer.issues]);
  const { results, isLoading, stale } = useServerSearch(query, { projects: false });
  const candidates = results.issues.filter((issue) => !selectedIds.has(issue.id));
  const hasParent = writer.issues.some((issue) => !!issue.parent_issue_id);

  return (
    <>
      {hasParent && !query.trim() && (
        <PaletteGroup heading={t(($) => $.pages.parent)}>
          <CommandPrimitive.Item
            value="parent:none"
            onSelect={() => {
              palette.close();
              writer.apply({ parent_issue_id: null });
            }}
            className={ROW_CLASS}
          >
            <Network className={ROW_ICON_CLASS} />
            <span className="min-w-0 flex-1 truncate">{tIssues(($) => $.actions.remove_parent_issue)}</span>
          </CommandPrimitive.Item>
        </PaletteGroup>
      )}
      {!query.trim() && (
        <div className="px-4 py-3 text-caption text-muted-foreground">{t(($) => $.empty.type_to_find_issue)}</div>
      )}
      {candidates.length > 0 && (
        <PaletteGroup heading={t(($) => $.groups.issues)}>
          {candidates.map((issue) => (
            <IssueResultRow
              key={issue.id}
              issue={issue}
              query={results.query}
              disabled={stale}
              onSelect={() => {
                palette.close();
                writer.apply({ parent_issue_id: issue.id });
              }}
            />
          ))}
        </PaletteGroup>
      )}
      {!isLoading && !stale && query.trim() && candidates.length === 0 && (
        <div className="py-8 text-center text-body text-muted-foreground">{t(($) => $.empty.no_results)}</div>
      )}
    </>
  );
}

export function IssueQuickActionPage({ issue, query }: { issue: Issue; query: string }) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const { data = [] } = useQuery(quickActionListOptions(wsId));
  const run = useRunQuickAction(issue.id);
  const actions = filterByLabel(
    data.filter((action) => action.status === "active"),
    query,
    (action) => action.name,
  );

  return (
    <PaletteGroup heading={t(($) => $.pages.quick_actions)}>
      {actions.map((action) => {
        const targetName = action.target_name || tIssues(($) => $.detail.quick_action_target_fallback);
        return (
          <CommandPrimitive.Item
            key={action.id}
            value={`quick-action:${action.id}`}
            onSelect={() => {
              palette.close();
              run.mutateAsync({ quickActionId: action.id }).then(
                (comment) => {
                  const { message, kind } = outcomeMessage(comment.trigger_outcomes?.[0], targetName, tIssues);
                  if (kind === "success") toast.success(message);
                  else if (kind === "error") toast.error(message);
                  else toast.info(message);
                },
                (error: unknown) => {
                  if (dispatchReasonCode(error) === "invocation_not_allowed") {
                    toast.error(tIssues(($) => $.detail.quick_action_blocked_body, { name: action.name }));
                    return;
                  }
                  toast.error(error instanceof Error ? error.message : String(error));
                },
              );
            }}
            className={ROW_CLASS}
          >
            <Zap className={ROW_ICON_CLASS} />
            <span className="min-w-0 flex-1 truncate">
              <HighlightText text={action.name} query={query} />
            </span>
            <RowHint>{tIssues(($) => $.detail.quick_action_runs_as, { name: targetName })}</RowHint>
          </CommandPrimitive.Item>
        );
      })}
    </PaletteGroup>
  );
}
