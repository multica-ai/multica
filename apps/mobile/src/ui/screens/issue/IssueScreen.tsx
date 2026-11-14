import { useEffect, useMemo } from "react";
import { Alert, Linking, Platform, Share } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { useTaskMessages } from "@multica/core/chat/queries";
import { issuePullRequestsOptions } from "@multica/core/github/queries";
import { useMarkIssueInboxRead } from "@multica/core/inbox/mutations";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { PRIORITY_ORDER } from "@multica/core/issues/config";
import { useCancelIssueRun, useCreateComment, useUpdateIssue } from "@multica/core/issues/mutations";
import { issueDetailOptions, issueTasksOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import { paths } from "@multica/core/paths";
import type { AgentTask, IssuePriority, TimelineEntry } from "@multica/core/types";
import { projectListOptions } from "@multica/core/projects/queries";
import { endpoints } from "../../../platform/env";
import { pick } from "../../components/action-sheet";
import { useActorResolver, type ActorResolver } from "../../model/people";
import { useStatusPresentation } from "../../model/status";
import { runSteps } from "../../model/steps";
import { isActiveTask, runPhase } from "../../model/tasks";
import { useWorkspace } from "../../model/workspace-context";
import type { RootStackParamList } from "../../navigation/types";
import { useDetail } from "../../shell/detail";
import { relativeTime, useStrings, type Strings } from "../../strings";
import { IssueView, type AgentRunModel, type PullRequestModel, type TimelineModel } from "./IssueView";

const VISIBLE_STEPS = 4;
const TIMELINE_LIMIT = 60;

function latestRun(tasks: readonly AgentTask[]): AgentTask | null {
  const active = tasks.find(isActiveTask);
  if (active) return active;
  return [...tasks].sort((a, b) => b.created_at.localeCompare(a.created_at))[0] ?? null;
}

function activityText(entry: TimelineEntry, strings: Strings, statusLabel: (key: string) => string): string {
  const to = typeof entry.details?.to === "string" ? entry.details.to : null;
  switch (entry.action) {
    case "created":
      return strings.issue.created;
    case "status_changed":
      return to ? strings.issue.changedStatus(statusLabel(to)) : strings.issue.updated;
    case "assignee_changed":
      return strings.issue.assigned;
    case "priority_changed":
      return strings.issue.changedPriority;
    case "title_changed":
      return strings.issue.renamed;
    default:
      return strings.issue.updated;
  }
}

function timelineModels(entries: readonly TimelineEntry[], resolveActor: ActorResolver, statusLabel: (key: string) => string, strings: Strings): TimelineModel[] {
  return entries.slice(-TIMELINE_LIMIT).map((entry) => {
    const actor = resolveActor(entry.actor_type, entry.actor_id, entry.actor_name);
    const time = relativeTime(entry.created_at, strings);
    return entry.type === "comment"
      ? { kind: "comment", id: entry.id, actor, body: entry.content ?? "", time, deleted: Boolean(entry.deleted_at) }
      : { kind: "activity", id: entry.id, actor, text: activityText(entry, strings, statusLabel), time };
  });
}

export function IssueScreen() {
  const { issueId } = useRoute<RouteProp<RootStackParamList, "Issue">>().params;
  return <IssueDetail issueId={issueId} />;
}

/** An issue by id: a pushed screen of its own, or the detail column beside a list. */
export function IssueDetail({ issueId }: { issueId: string }) {
  const navigation = useNavigation();
  const detailNav = useDetail();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const resolveActor = useActorResolver();
  const status = useStatusPresentation(workspace.id);
  const catalog = useIssueStatuses(workspace.id);

  const detail = useQuery(issueDetailOptions(workspace.id, issueId));
  const issue = detail.data;
  const id = issue?.id ?? "";
  const timeline = useQuery({ ...issueTimelineOptions(id), enabled: Boolean(id) });
  const tasks = useQuery({ ...issueTasksOptions(id), enabled: Boolean(id) });
  const pullRequests = useQuery({ ...issuePullRequestsOptions(id), enabled: Boolean(id) });
  const projects = useQuery({ ...projectListOptions(workspace.id), enabled: Boolean(issue?.project_id) });

  const task = latestRun(tasks.data ?? []);
  const live = task ? isActiveTask(task) : false;
  const messages = useTaskMessages(task?.id ?? "", live, Boolean(task));

  const updateIssue = useUpdateIssue();
  const createComment = useCreateComment(id);
  const cancelRun = useCancelIssueRun(id);
  const markIssueRead = useMarkIssueInboxRead(workspace.id);
  const markRead = markIssueRead.mutate;
  useEffect(() => {
    if (id) markRead(id);
  }, [id, markRead]);

  const run = useMemo<AgentRunModel | null>(() => {
    if (!task) return null;
    const steps = runSteps(messages.data ?? [], live);
    const since = live ? (task.started_at ?? task.created_at) : (task.completed_at ?? task.created_at);
    return {
      id: task.id,
      agent: resolveActor("agent", task.agent_id),
      phase: runPhase(task.status),
      time: relativeTime(since, strings),
      steps: steps.slice(-VISIBLE_STEPS),
      hiddenSteps: Math.max(0, steps.length - VISIBLE_STEPS),
    };
  }, [task, messages.data, live, resolveActor, strings]);

  const prs = useMemo<PullRequestModel[]>(
    () =>
      (pullRequests.data?.pull_requests ?? []).map((pr) => ({
        id: pr.id,
        title: pr.title,
        state: pr.state,
        additions: pr.additions ?? 0,
        deletions: pr.deletions ?? 0,
        files: pr.changed_files ?? 0,
        url: pr.html_url,
      })),
    [pullRequests.data],
  );

  const items = useMemo(() => timelineModels(timeline.data ?? [], resolveActor, (key) => status(key).label, strings), [timeline.data, resolveActor, status, strings]);

  const missing = detail.error instanceof ApiError && detail.error.status === 404;
  const project = issue?.project_id ? projects.data?.find((p) => p.id === issue.project_id)?.title ?? null : null;
  const webUrl = issue ? `${endpoints.webUrl}${paths.workspace(workspace.slug).issueDetail(issue.identifier)}` : null;

  return (
    <IssueView
      state={issue ? "ready" : missing ? "missing" : detail.isError ? "error" : "loading"}
      issue={
        issue
          ? {
              identifier: issue.identifier,
              title: issue.title,
              description: issue.description ?? "",
              status: status(issue.status),
              priority: issue.priority,
              assignee: issue.assignee_type && issue.assignee_id ? resolveActor(issue.assignee_type, issue.assignee_id) : null,
              project,
            }
          : null
      }
      run={run}
      pullRequests={prs}
      timeline={items}
      onBack={() => navigation.goBack()}
      onRetry={() => void detail.refetch()}
      onShare={webUrl ? () => void Share.share({ url: webUrl, message: Platform.OS === "ios" ? undefined : webUrl }) : undefined}
      onStatusPress={
        issue
          ? async () => {
              const value = await pick(
                strings.issue.statusTitle,
                catalog.activeStatuses.map((entry) => ({ value: entry.key, label: status(entry.key).label })),
                strings.common.cancel,
              );
              if (value) updateIssue.mutate({ id: issue.id, status: value });
            }
          : undefined
      }
      onPriorityPress={
        issue
          ? async () => {
              const value = await pick<IssuePriority>(
                strings.issue.priorityTitle,
                PRIORITY_ORDER.map((value) => ({ value, label: strings.priority[value] })),
                strings.common.cancel,
              );
              if (value) updateIssue.mutate({ id: issue.id, priority: value });
            }
          : undefined
      }
      onStop={task && live ? () => cancelRun.mutate(task.id) : undefined}
      onOpenUrl={(url) => void Linking.openURL(url)}
      onOpenIssue={(target) => detailNav.open({ kind: "issue", issueId: target })}
      onComment={async (text) => {
        try {
          await createComment.mutateAsync({ content: text });
        } catch (error) {
          Alert.alert(strings.issue.commentFailed);
          throw error;
        }
      }}
    />
  );
}
