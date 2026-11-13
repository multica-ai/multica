"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import {
  buildInboxSections,
  nextInboxWake,
  type InboxSections,
} from "@multica/core/inbox/sections";
import type { AgentTask, InboxItem } from "@multica/core/types";

const EMPTY_TASKS: AgentTask[] = [];
// The longest delay a timer takes; a snooze further out re-arms when it fires.
const MAX_TIMER_MS = 2_147_000_000;

/**
 * The current time, moved forward when the next snooze in `items` runs out.
 * Nothing arrives from the server when a snooze simply elapses, so the list
 * has to notice by itself that an issue is back.
 */
function useInboxClock(items: readonly InboxItem[]): number {
  const [now, setNow] = useState(() => Date.now());
  const nextWake = useMemo(() => nextInboxWake(items, now), [items, now]);
  useEffect(() => {
    if (nextWake == null) return;
    const delay = Math.min(Math.max(nextWake - Date.now(), 0) + 50, MAX_TIMER_MS);
    const timer = setTimeout(() => setNow(Date.now()), delay);
    return () => clearTimeout(timer);
  }, [nextWake]);
  return now;
}

/**
 * The Inbox list split into its sections.
 *
 * `rows` is the list as filtered, one notification per issue; `items` is every
 * active notification. "Working" is read from the workspace's task snapshot,
 * which realtime already keeps fresh, and is left out while `filtered`.
 */
export function useInboxSections(
  wsId: string,
  rows: readonly InboxItem[],
  items: readonly InboxItem[],
  { filtered = false }: { filtered?: boolean } = {},
): InboxSections {
  const userId = useAuthStore((s) => s.user?.id);
  const { data: tasks = EMPTY_TASKS } = useQuery(agentTaskSnapshotOptions(wsId));
  const now = useInboxClock(items);
  return useMemo(
    () => buildInboxSections(rows, items, now, filtered ? undefined : { tasks, userId }),
    [rows, items, now, filtered, tasks, userId],
  );
}
