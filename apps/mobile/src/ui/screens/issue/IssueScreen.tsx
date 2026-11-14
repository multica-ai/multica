import { useEffect, useMemo, useState } from "react";
import { Alert, Linking, Platform, Share } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import * as Clipboard from "expo-clipboard";
import { api, ApiError } from "@multica/core/api";
import { useTaskMessages } from "@multica/core/chat/queries";
import { issuePullRequestsOptions } from "@multica/core/github/queries";
import { useMarkIssueInboxRead } from "@multica/core/inbox/mutations";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { isDeletedComment } from "@multica/core/issues/comment-deletion";
import { PRIORITY_ORDER } from "@multica/core/issues/config";
import {
  useCancelIssueRun,
  useCreateComment,
  useDeleteComment,
  useDeleteIssue,
  useResolveComment,
  useToggleCommentReaction,
  useToggleIssueSubscriber,
  useUpdateComment,
  useUpdateIssue,
} from "@multica/core/issues/mutations";
import { childIssuesOptions, issueDetailOptions, issueSubscribersOptions, issueTasksOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import { useIssueDeletion } from "@multica/core/issues/use-issue-deletion";
import { paths } from "@multica/core/paths";
import { useCreatePin, useDeletePin } from "@multica/core/pins/mutations";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { attachmentDownloadPath, contentReferencesAttachment } from "@multica/core/types";
import type { AgentTask, Issue, IssuePriority, SearchIssueResult, TimelineEntry } from "@multica/core/types";
import { endpoints } from "../../../platform/env";
import { confirmDestructive, pick, promptText } from "../../components/action-sheet";
import { formatDay, toDay } from "../../model/dates";
import type { MentionCandidate } from "../../model/mentions";
import { useActorResolver, type ActorResolver } from "../../model/people";
import { useAssigneeResolver, useAssignees } from "../../model/roster";
import { useStatusPresentation, type StatusPresentation } from "../../model/status";
import { runSteps } from "../../model/steps";
import { isActiveTask, runPhase } from "../../model/tasks";
import { buildTimeline, type TimelineNode } from "../../model/threads";
import { useWorkspace } from "../../model/workspace-context";
import type { IssueFieldKey, RootStackParamList } from "../../navigation/types";
import { useDetail } from "../../shell/detail";
import { relativeTime, useStrings, type Strings } from "../../strings";
import { IssueView, type AgentRunModel, type CommentModel, type IssueLinkModel, type PullRequestModel, type TimelineModel } from "./IssueView";

const VISIBLE_STEPS = 4;
const TIMELINE_LIMIT = 60;
const SEARCH_DELAY = 250;

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

function commentModel(entry: TimelineEntry, userId: string | null, actorOf: ActorResolver, strings: Strings): CommentModel {
  const body = entry.content ?? "";
  const reactions = new Map<string, { count: number; mine: boolean }>();
  for (const reaction of entry.reactions ?? []) {
    const current = reactions.get(reaction.emoji) ?? { count: 0, mine: false };
    current.count += 1;
    if (reaction.actor_type === "member" && reaction.actor_id === userId) current.mine = true;
    reactions.set(reaction.emoji, current);
  }
  return {
    id: entry.id,
    actor: actorOf(entry.actor_type, entry.actor_id, entry.actor_name),
    body,
    time: relativeTime(entry.created_at, strings),
    edited: Boolean(entry.updated_at && entry.updated_at > entry.created_at && !isDeletedComment(entry)),
    deleted: isDeletedComment(entry),
    mine: entry.actor_type === "member" && entry.actor_id === userId,
    reactions: [...reactions].map(([emoji, value]) => ({ emoji, ...value })),
    files: (entry.attachments ?? [])
      .filter((file) => !contentReferencesAttachment(body, file))
      .map((file) => ({
        id: file.id,
        name: file.filename,
        url: file.markdown_url || attachmentDownloadPath(file.id),
        image: file.content_type.startsWith("image/"),
      })),
  };
}

function runModel(task: AgentTask, actorOf: ActorResolver, strings: Strings, steps: AgentRunModel["steps"] = []): AgentRunModel {
  const live = isActiveTask(task);
  const since = live ? (task.started_at ?? task.created_at) : (task.completed_at ?? task.created_at);
  return {
    id: task.id,
    agent: actorOf("agent", task.agent_id),
    phase: runPhase(task.status),
    time: relativeTime(since, strings),
    steps: steps.slice(-VISIBLE_STEPS),
    hiddenSteps: Math.max(0, steps.length - VISIBLE_STEPS),
  };
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
  const resolveAssignee = useAssigneeResolver(workspace.id);
  const status = useStatusPresentation(workspace.id);
  const catalog = useIssueStatuses(workspace.id);
  const assignees = useAssignees(workspace.id);
  const userId = assignees.userId;
  const deletion = useIssueDeletion(workspace.id);

  const detail = useQuery(issueDetailOptions(workspace.id, issueId));
  const issue = detail.data;
  const id = issue?.id ?? "";
  const timeline = useQuery({ ...issueTimelineOptions(id), enabled: Boolean(id) });
  const tasks = useQuery({ ...issueTasksOptions(id), enabled: Boolean(id) });
  const pullRequests = useQuery({ ...issuePullRequestsOptions(id), enabled: Boolean(id) });
  const projects = useQuery({ ...projectListOptions(workspace.id), enabled: Boolean(issue?.project_id) });
  const children = useQuery({ ...childIssuesOptions(workspace.id, id), enabled: Boolean(id) });
  const parent = useQuery({ ...issueDetailOptions(workspace.id, issue?.parent_issue_id ?? ""), enabled: Boolean(issue?.parent_issue_id) });
  const subscribers = useQuery({ ...issueSubscribersOptions(id), enabled: Boolean(id) });
  const pins = useQuery({ ...pinListOptions(workspace.id, userId ?? ""), enabled: Boolean(userId) });

  const nodes = useMemo(() => buildTimeline(timeline.data ?? [], tasks.data ?? []).slice(-TIMELINE_LIMIT), [timeline.data, tasks.data]);
  const liveTask = useMemo(() => (tasks.data ?? []).find(isActiveTask) ?? null, [tasks.data]);
  const messages = useTaskMessages(liveTask?.id ?? "", true, Boolean(liveTask));

  const updateIssue = useUpdateIssue();
  const deleteIssue = useDeleteIssue();
  const createComment = useCreateComment(id);
  const updateComment = useUpdateComment(id);
  const deleteComment = useDeleteComment(id);
  const resolveComment = useResolveComment(id);
  const toggleReaction = useToggleCommentReaction(id);
  const toggleSubscriber = useToggleIssueSubscriber(id);
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const cancelRun = useCancelIssueRun(id);
  const markIssueRead = useMarkIssueInboxRead(workspace.id);
  const markRead = markIssueRead.mutate;
  useEffect(() => {
    if (id) markRead(id);
  }, [id, markRead]);

  const items = useMemo<TimelineModel[]>(() => {
    const liveSteps = liveTask ? runSteps(messages.data ?? [], true) : [];
    const run = (task: AgentTask) => runModel(task, resolveActor, strings, task.id === liveTask?.id ? liveSteps : []);
    return nodes.map((node: TimelineNode): TimelineModel => {
      switch (node.kind) {
        case "activity":
          return {
            kind: "activity",
            id: node.entry.id,
            actor: resolveActor(node.entry.actor_type, node.entry.actor_id, node.entry.actor_name),
            text: activityText(node.entry, strings, (key) => status(key).label),
            time: relativeTime(node.entry.created_at, strings),
          };
        case "thread":
          return {
            kind: "thread",
            id: node.root.id,
            root: commentModel(node.root, userId, resolveActor, strings),
            replies: node.replies.map((reply) => commentModel(reply, userId, resolveActor, strings)),
            run: node.run ? run(node.run) : null,
            resolved: node.resolved,
          };
        case "run":
          return { kind: "run", id: node.task.id, run: run(node.task) };
      }
    });
  }, [nodes, liveTask, messages.data, resolveActor, strings, status, userId]);

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

  // Mentions: the people, agents and squads a comment can call in, and issues found as the user types.
  const [mentionQuery, setMentionQuery] = useState<string | null>(null);
  const [issueHits, setIssueHits] = useState<SearchIssueResult[]>([]);
  useEffect(() => {
    const q = mentionQuery?.trim();
    if (!q) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      api
        .searchIssues({ q, limit: 5, include_closed: true, signal: controller.signal })
        .then((response) => setIssueHits(response.issues))
        .catch(() => undefined);
    }, SEARCH_DELAY);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [mentionQuery]);
  const candidates = useMemo<MentionCandidate[]>(
    () => [
      ...assignees.members.map((entry) => ({ type: "member" as const, id: entry.id, label: entry.actor.name, actor: entry.actor })),
      ...assignees.agents.map((entry) => ({ type: "agent" as const, id: entry.id, label: entry.actor.name, actor: entry.actor, detail: strings.issue.agents })),
      ...assignees.squads.map((entry) => ({ type: "squad" as const, id: entry.id, label: entry.actor.name, actor: entry.actor, detail: strings.issue.squads })),
      ...(mentionQuery?.trim() ? issueHits : []).map((hit) => ({ type: "issue" as const, id: hit.id, label: hit.identifier, detail: hit.title })),
    ],
    [assignees, issueHits, mentionQuery, strings],
  );

  const missing = detail.error instanceof ApiError && detail.error.status === 404;
  const project = issue?.project_id ? (projects.data?.find((p) => p.id === issue.project_id)?.title ?? null) : null;
  const issueUrl = issue ? `${endpoints.webUrl}${paths.workspace(workspace.slug).issueDetail(issue.identifier)}` : null;
  const linkOf = (target: Issue): IssueLinkModel => {
    const presentation: StatusPresentation = status(target.status);
    const category = catalog.categoryOf(target.status);
    return { id: target.id, identifier: target.identifier, title: target.title, status: presentation, done: category === "done" || category === "closed" };
  };
  const today = toDay(new Date());

  const failed = () => Alert.alert(strings.issue.updateFailed);
  const update = (fields: Parameters<typeof updateIssue.mutate>[0]) => updateIssue.mutate(fields, { onError: failed });
  const openField = (field: IssueFieldKey) => issue && navigation.navigate("IssueField", { issueId: issue.id, field });

  const subscribed = Boolean(userId && subscribers.data?.some((s) => s.user_type === "member" && s.user_id === userId));
  const pinned = Boolean(issue && pins.data?.some((pin) => pin.item_type === "issue" && pin.item_id === issue.id));

  const openMenu = async () => {
    if (!issue) return;
    type Action = "rename" | "copyLink" | "copyId" | "share" | "subscribe" | "pin" | "parent" | "removeParent" | "delete";
    const options: { value: Action; label: string; destructive?: boolean }[] = [
      { value: "rename", label: strings.issue.menu.rename },
      { value: "copyLink", label: strings.issue.menu.copyLink },
      { value: "copyId", label: strings.issue.menu.copyIdentifier },
      { value: "share", label: strings.issue.menu.share },
      { value: "subscribe", label: subscribed ? strings.issue.menu.unsubscribe : strings.issue.menu.subscribe },
      { value: "pin", label: pinned ? strings.issue.menu.unpin : strings.issue.menu.pin },
      { value: "parent", label: strings.issue.menu.setParent },
    ];
    if (issue.parent_issue_id) options.push({ value: "removeParent", label: strings.issue.menu.removeParent });
    if (deletion.canDelete(issue)) options.push({ value: "delete", label: strings.issue.menu.delete, destructive: true });
    const action = await pick(`${issue.identifier} · ${issue.title}`, options, strings.common.cancel);
    switch (action) {
      case "rename": {
        const title = await promptText(strings.issue.menu.renameTitle, issue.title, strings.form.save, strings.common.cancel);
        if (title?.trim() && title.trim() !== issue.title) update({ id: issue.id, title: title.trim() });
        return;
      }
      case "copyLink":
        if (issueUrl) await Clipboard.setStringAsync(issueUrl);
        return;
      case "copyId":
        await Clipboard.setStringAsync(issue.identifier);
        return;
      case "share":
        if (issueUrl) void Share.share({ url: issueUrl, message: Platform.OS === "ios" ? undefined : issueUrl });
        return;
      case "subscribe":
        if (userId) toggleSubscriber.mutate({ userId, userType: "member", subscribed }, { onError: failed });
        return;
      case "pin":
        if (pinned) deletePin.mutate({ itemType: "issue", itemId: issue.id }, { onError: failed });
        else createPin.mutate({ item_type: "issue", item_id: issue.id }, { onError: failed });
        return;
      case "parent":
        openField("parent");
        return;
      case "removeParent":
        update({ id: issue.id, parent_issue_id: null });
        return;
      case "delete": {
        const sure = await confirmDestructive(strings.issue.menu.deleteTitle(issue.identifier), strings.issue.menu.deleteDetail, strings.issue.menu.delete, strings.common.cancel);
        if (!sure) return;
        deleteIssue.mutate(issue.id, { onError: failed });
        navigation.goBack();
        return;
      }
    }
  };

  const addProperty = async () => {
    if (!issue) return;
    const options: { value: IssueFieldKey; label: string }[] = [];
    if (!issue.project_id) options.push({ value: "project", label: strings.issue.project });
    if (!issue.labels?.length) options.push({ value: "labels", label: strings.issue.labels });
    if (!issue.start_date) options.push({ value: "start", label: strings.issue.start });
    if (!issue.due_date) options.push({ value: "due", label: strings.issue.due });
    if (!issue.parent_issue_id) options.push({ value: "parent", label: strings.issue.parent });
    const field = await pick(strings.issue.addProperty, options, strings.common.cancel);
    if (field) openField(field);
  };

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
              assignee: issue.assignee_type && issue.assignee_id ? resolveAssignee(issue.assignee_type, issue.assignee_id) : null,
              project,
              labels: issue.labels ?? [],
              start: issue.start_date ? formatDay(issue.start_date, strings) : null,
              due: issue.due_date ? formatDay(issue.due_date, strings) : null,
              overdue: Boolean(issue.due_date && issue.due_date.slice(0, 10) < today && !["done", "closed"].includes(catalog.categoryOf(issue.status))),
              parent: parent.data ? linkOf(parent.data) : null,
              children: (children.data ?? []).map(linkOf),
            }
          : null
      }
      pullRequests={prs}
      timeline={items}
      onBack={() => navigation.goBack()}
      onRetry={() => void detail.refetch()}
      onMore={issue ? () => void openMenu() : undefined}
      properties={
        issue
          ? {
              onStatus: async () => {
                const value = await pick(
                  strings.issue.statusTitle,
                  catalog.activeStatuses.map((entry) => ({ value: entry.key, label: status(entry.key).label })),
                  strings.common.cancel,
                );
                if (value && value !== issue.status) update({ id: issue.id, status: value });
              },
              onPriority: async () => {
                const value = await pick<IssuePriority>(
                  strings.issue.priorityTitle,
                  PRIORITY_ORDER.map((value) => ({ value, label: strings.priority[value] })),
                  strings.common.cancel,
                );
                if (value && value !== issue.priority) update({ id: issue.id, priority: value });
              },
              onAssignee: () => openField("assignee"),
              onProject: () => openField("project"),
              onLabels: () => openField("labels"),
              onStart: () => openField("start"),
              onDue: () => openField("due"),
              onAdd: () => void addProperty(),
            }
          : undefined
      }
      onOpenUrl={(url) => void Linking.openURL(url)}
      onOpenIssue={(target) => detailNav.open({ kind: "issue", issueId: target })}
      comments={{
        onReact: (commentId, emoji) => {
          const entry = timeline.data?.find((candidate) => candidate.id === commentId);
          const existing = entry?.reactions?.find((r) => r.emoji === emoji && r.actor_type === "member" && r.actor_id === userId);
          toggleReaction.mutate({ commentId, emoji, existing }, { onError: failed });
        },
        onCopy: (text) => void Clipboard.setStringAsync(text),
        onCopyLink: (commentId) => {
          if (issueUrl) void Clipboard.setStringAsync(`${issueUrl}#comment-${commentId}`);
        },
        onResolve: (commentId, resolved) => resolveComment.mutate({ commentId, resolved }, { onError: failed }),
        onDelete: async (commentId) => {
          const sure = await confirmDestructive(strings.issue.deleteComment, undefined, strings.issue.delete, strings.common.cancel);
          if (sure) deleteComment.mutate(commentId, { onError: failed });
        },
        onStop: liveTask ? (runId) => cancelRun.mutate(runId) : undefined,
      }}
      onComment={async (text, parentId) => {
        try {
          await createComment.mutateAsync({ content: text, parentId: parentId ?? undefined });
        } catch (error) {
          Alert.alert(strings.issue.commentFailed);
          throw error;
        }
      }}
      onEditComment={async (commentId, text) => {
        const entry = timeline.data?.find((candidate) => candidate.id === commentId);
        try {
          await updateComment.mutateAsync({ commentId, content: text, attachmentIds: (entry?.attachments ?? []).map((file) => file.id) });
        } catch (error) {
          Alert.alert(strings.issue.updateFailed);
          throw error;
        }
      }}
      mentions={{ candidates, onQuery: setMentionQuery }}
    />
  );
}
