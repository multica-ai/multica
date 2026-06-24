"use client";

import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { InboxItem } from "@multica/core/types";

function updateInboxIssueSiblings(
  items: InboxItem[] | undefined,
  id: string,
  update: (item: InboxItem) => InboxItem,
): InboxItem[] | undefined {
  const target = items?.find((item) => item.id === id);
  const issueId = target?.issue_id;
  return items?.map((item) =>
    item.id === id || (issueId && item.issue_id === issueId)
      ? update(item)
      : item,
  );
}

type InboxPages = InfiniteData<InboxItem[], number>;

function isInboxPages(value: InboxItem[] | InboxPages | undefined): value is InboxPages {
  return !!value && !Array.isArray(value) && "pages" in value;
}

function filterArchivedCache(
  old: InboxItem[] | InboxPages | undefined,
  id: string,
  issueId: string | null | undefined,
): InboxItem[] | InboxPages | undefined {
  const keep = (item: InboxItem) =>
    item.id === id || (issueId && item.issue_id === issueId) ? false : true;
  if (isInboxPages(old)) {
    return {
      ...old,
      pages: old.pages.map((page) => page.filter(keep)),
    };
  }
  return old?.filter(keep);
}

function findArchivedCacheItem(
  old: InboxItem[] | InboxPages | undefined,
  id: string,
): InboxItem | undefined {
  if (isInboxPages(old)) {
    return old.pages.flat().find((item) => item.id === id);
  }
  return old?.find((item) => item.id === id);
}

/**
 * Mute an inbox item until the supplied timestamp. Optimistic — sets
 * `muted_until` locally before the server round-trip and rolls back on
 * error, mirroring the upstream archive/read mutations.
 */
export function useMuteInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, mutedUntil }: { id: string; mutedUntil: Date }) =>
      api.muteInbox(id, mutedUntil),
    onMutate: async ({ id, mutedUntil }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      // FIR-1730: muting must not mark the row unread — a reminder/snooze only
      // becomes unread when it FIRES (the cerebro_reminder sweeper re-surfaces
      // it), not when it is set. Setting read:false here made the muted row
      // count as unread while hidden, inflating the unread badge.
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        updateInboxIssueSiblings(old, id, (item) =>
          ({ ...item, muted_until: mutedUntil.toISOString() }),
        ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useUnmuteInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unmuteInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        updateInboxIssueSiblings(old, id, (item) => ({ ...item, muted_until: null })),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

/**
 * Restore an archived inbox row to the active inbox. Mirror of the upstream
 * `useArchiveInbox` — optimistically flips `archived` back to `false` so the
 * row disappears from the archived view immediately, then invalidates both
 * the active and archived list caches so the row resurfaces in the active
 * inbox without a manual reload.
 */
export function useUnarchiveInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unarchiveInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.archivedList(wsId) });
      const prev = qc.getQueryData<InboxItem[] | InboxPages>(inboxKeys.archivedList(wsId));
      const target = findArchivedCacheItem(prev, id);
      const issueId = target?.issue_id;
      qc.setQueryData<InboxItem[] | InboxPages>(inboxKeys.archivedList(wsId), (old) =>
        filterArchivedCache(old, id, issueId),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.archivedList(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.archivedList(wsId) });
      // Active inbox query must refetch too — the unarchived row needs to
      // resurface there without a manual reload.
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

/**
 * Force an inbox row back to unread. Counterpart to the existing
 * `useMarkInboxRead` from `@multica/core/inbox/mutations`.
 */
export function useMarkInboxUnread() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxUnread(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          item.id === id ? { ...item, read: false } : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

/**
 * FIR-2385 — the agent owner accepts a private_agent_run_request from their
 * inbox. The server enqueues the agent on the tagged comment and archives the
 * request, so optimistically drop the row from the active inbox.
 */
export function useRunPrivateAgentRequest() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.runPrivateAgentRunRequest(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.filter((item) => item.id !== id),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

/**
 * Manually add an issue to the member's inbox. Idempotent: if the issue is
 * already in the active inbox it is surfaced (marked unread) rather than
 * duplicated. Invalidates the inbox list on settle so the new row appears.
 */
export function useAddIssueToInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (issueId: string) => api.addIssueToInbox(issueId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useCreateInboxReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      text,
      plannedAt,
      issueId,
      // FIR-2643 — pin the reminder to one specific comment.
      commentId,
    }: {
      text: string;
      plannedAt: Date;
      issueId?: string | null;
      commentId?: string | null;
    }) => api.createInboxReminder({ text, plannedAt, issueId, commentId }),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

// FIR-394 — create a reminder as its OWN entity linked back to the comment,
// instead of an inbox_item on the conversation (which is what locked DM threads,
// FIR-249). Posts straight to /api/cerebro/reminders; kept here (rather than
// importing @multica/cerebro-reminders) so the inbox package takes no dependency
// on the reminders package — that would close a views↔inbox dependency cycle.
// The query key mirrors reminderKeys.all so the reminder overview refreshes too.
export function useCreateCommentReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      message_id,
      remind_at,
      text,
    }: {
      message_id: string;
      remind_at: string;
      text: string;
    }) =>
      api.cerebroRequest("/api/cerebro/reminders", {
        method: "POST",
        body: JSON.stringify({ message_id, remind_at, text }),
      }),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["cerebro-reminders", wsId] });
    },
  });
}

// FIR-394 — consolidate the legacy "remind me later / snooze" entries (inbox
// row, channel, focus list) onto the ONE reminder entity. Each of those
// surfaces still hides its row via its own mute/snooze, but now ALSO records a
// reminder so it shows up in the unified reminder overview and re-fires at the
// chosen time. Anchored to the row's issue/project. Posted inline (not via
// @multica/cerebro-reminders) to keep the inbox package free of a reminders-
// package dependency.

// The subset of the reminder API shape we need to find a row's snooze-reminder.
interface AnchoredReminderRow {
  id: string;
  status: string;
  message_id: string | null;
  conversation_id: string | null;
  project_id: string | null;
}

// The active reminders the CURRENT row's snooze created: anchored to this
// issue/project and NOT message-level (a comment reminder must survive). Used to
// keep snooze idempotent (no stacking) and to remove the reminder when the row
// is un-muted (so "remove" means remove everywhere).
async function findRowSnoozeReminderIds(
  issueId?: string | null,
  projectId?: string | null,
): Promise<string[]> {
  if (!issueId && !projectId) return [];
  const list = await api.cerebroRequest<AnchoredReminderRow[]>("/api/cerebro/reminders");
  if (!Array.isArray(list)) return [];
  return list
    .filter(
      (r) =>
        r &&
        r.status !== "done" &&
        !r.message_id &&
        ((issueId != null && r.conversation_id === issueId) ||
          (projectId != null && r.project_id === projectId)),
    )
    .map((r) => r.id);
}

async function deleteReminders(ids: string[]): Promise<void> {
  await Promise.all(
    ids.map((id) =>
      api.cerebroRequest(`/api/cerebro/reminders/${id}`, { method: "DELETE" }),
    ),
  );
}

export function useSnoozeAsReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({
      remindAt,
      text,
      issueId,
      projectId,
    }: {
      remindAt: Date;
      text: string;
      issueId?: string | null;
      projectId?: string | null;
    }) => {
      // Idempotent per row: snoozing the same row again replaces its reminder
      // instead of stacking a second one.
      await deleteReminders(await findRowSnoozeReminderIds(issueId, projectId));
      return api.cerebroRequest("/api/cerebro/reminders", {
        method: "POST",
        body: JSON.stringify({
          remind_at: remindAt.toISOString(),
          text: text.trim() || "Reminder",
          ...(issueId
            ? { issue_id: issueId }
            : projectId
              ? { project_id: projectId }
              : {}),
        }),
      });
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["cerebro-reminders", wsId] });
    },
  });
}

// FIR-394 — the inbox-toolbar "New reminder" now creates a free reminder on the
// ONE reminder entity (POST /api/cerebro/reminders) instead of the legacy
// inbox-item reminder (/api/inbox/reminders), so every "remind me" entry point
// feeds the same overview. Posted inline to keep the inbox package free of a
// reminders-package dependency.
export function useCreateGlobalReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ text, plannedAt }: { text: string; plannedAt: Date }) =>
      api.cerebroRequest("/api/cerebro/reminders", {
        method: "POST",
        body: JSON.stringify({ text, remind_at: plannedAt.toISOString() }),
      }),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["cerebro-reminders", wsId] });
    },
  });
}

// Un-muting a row removes the reminder its snooze created, so "remove" means
// remove in BOTH places (the row reappears AND it leaves the reminder overview).
export function useRemoveRowSnoozeReminder() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({
      issueId,
      projectId,
    }: {
      issueId?: string | null;
      projectId?: string | null;
    }) => {
      await deleteReminders(await findRowSnoozeReminderIds(issueId, projectId));
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ["cerebro-reminders", wsId] });
    },
  });
}
