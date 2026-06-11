// TECH-3413 — fetches the same merged inbox data the classic inbox uses and
// exposes it in a shape the dynamic sections can filter/group. Mirrors the data
// layer of packages/views/inbox/components/inbox-page.tsx so both inboxes read
// identical server state (one source of truth via the TanStack Query cache).
"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  inboxListOptions,
  activeIssueTasksOptions,
  deduplicateInboxItems,
  inboxItemSortTime,
} from "@multica/core/inbox/queries";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import { channelListOptions } from "@multica/core/channels";
import { projectListOptions } from "@multica/core/projects";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useInboxWakeupStates, useInboxPinnedMatcher } from "@multica/cerebro-inbox";
import type { InboxActionContext } from "@multica/cerebro-inbox";
import type { Channel, Project } from "@multica/core/types";
import type { DynInboxEntry, SectionFilterContext } from "./section-filter";

// Trivial mirror of views/common/agent-run-pip's taskStatusToRunState (not
// exported across the package boundary): queued stays queued, anything else is
// an active run.
function taskStatusToRunState(status: string): "active" | "queued" {
  return status === "queued" ? "queued" : "active";
}

export interface DynamicInboxData {
  entries: DynInboxEntry[];
  filterContext: SectionFilterContext;
  projectMap: Map<string, Project>;
  projects: Project[];
  loading: boolean;
}

export function useDynamicInboxData(wsId: string): DynamicInboxData {
  const userId = useAuthStore((s) => s.user?.id ?? "");

  const inboxQuery = useQuery(inboxListOptions(wsId));
  const items = useMemo(
    () => deduplicateInboxItems(inboxQuery.data ?? []),
    [inboxQuery.data],
  );

  const chatQuery = useQuery(chatSessionsOptions(wsId));
  const chatSessions = useMemo(
    () => (chatQuery.data ?? []).filter((s) => s.status !== "archived"),
    [chatQuery.data],
  );

  const channelsEnabled = useFeatureFlag("cerebro_channels");
  const channelQuery = useQuery({
    ...channelListOptions(wsId),
    enabled: channelsEnabled,
  });
  const channels = useMemo<Channel[]>(() => channelQuery.data ?? [], [channelQuery.data]);
  const channelMap = useMemo(() => new Map(channels.map((c) => [c.id, c])), [channels]);

  const { data: activeIssueTasksData } = useQuery(activeIssueTasksOptions(wsId));
  const issueRunStates = useMemo(
    () =>
      new Map(
        activeIssueTasksData?.tasks?.length
          ? activeIssueTasksData.tasks.map((t) => [t.issue_id, taskStatusToRunState(t.status)])
          : (activeIssueTasksData?.issue_ids ?? []).map((id) => [id, "active" as const]),
      ),
    [activeIssueTasksData],
  );
  const subIssueRunStates = useMemo(() => {
    const map = new Map<string, "active" | "queued">();
    for (const t of activeIssueTasksData?.tasks ?? []) {
      if (!t.parent_issue_id) continue;
      if (map.get(t.parent_issue_id) === "active") continue;
      map.set(t.parent_issue_id, taskStatusToRunState(t.status));
    }
    return map;
  }, [activeIssueTasksData]);

  const { data: pendingChatTasksData } = useQuery(pendingChatTasksOptions(wsId));
  const chatRunStates = useMemo(
    () =>
      new Map(
        (pendingChatTasksData?.tasks ?? []).map((t) => [t.chat_session_id, taskStatusToRunState(t.status)]),
      ),
    [pendingChatTasksData],
  );

  const wakeupRunningEnabled = useFeatureFlag("cerebro_inbox_wakeup_running");
  const wakeupStates = useInboxWakeupStates(wsId, wakeupRunningEnabled);
  const wakeupIssueIds = useMemo(() => new Set(wakeupStates.keys()), [wakeupStates]);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const projectMap = useMemo(() => new Map(projects.map((p) => [p.id, p])), [projects]);

  const mentionedChannels = useMemo(() => {
    const set = new Set<string>();
    for (const item of items) {
      if (item.read || item.type !== "mentioned" || !item.issue_id) continue;
      if (!channelMap.has(item.issue_id)) continue;
      set.add(item.issue_id);
    }
    return set;
  }, [items, channelMap]);

  const matchesPins = useInboxPinnedMatcher(wsId, userId);

  // Build the merged entry list — channels swallow their own notifications
  // (a notif whose issue_id is a channel is folded into the channel row).
  const entries = useMemo<DynInboxEntry[]>(() => {
    const out: DynInboxEntry[] = [];
    for (const channel of channels) {
      const time = new Date(channel.last_message?.created_at ?? channel.updated_at).getTime();
      out.push({ kind: "channel", id: channel.id, time, channel });
    }
    for (const session of chatSessions) {
      out.push({ kind: "chat", id: session.id, time: new Date(session.updated_at).getTime(), session });
    }
    for (const item of items) {
      if (item.issue_id && channelMap.has(item.issue_id)) continue;
      out.push({ kind: "notif", id: item.id, time: inboxItemSortTime(item), item });
    }
    return out;
  }, [channels, chatSessions, items, channelMap]);

  const filterContext = useMemo<SectionFilterContext>(() => {
    const action: InboxActionContext = {
      userId,
      issueRunStates,
      subIssueRunStates,
      chatRunStates,
      mentionedChannels,
      wakeupIssueIds,
    };
    return { action, matchesPins: (entry) => matchesPins(entry) };
  }, [userId, issueRunStates, subIssueRunStates, chatRunStates, mentionedChannels, wakeupIssueIds, matchesPins]);

  return {
    entries,
    filterContext,
    projectMap,
    projects,
    loading: inboxQuery.isLoading,
  };
}
