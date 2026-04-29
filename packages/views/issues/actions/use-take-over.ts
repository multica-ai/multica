"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueKeys } from "@multica/core/issues/queries";
import { canTakeOverLocally } from "@multica/core/issues/take-over";
import { runtimeListOptions } from "@multica/core/runtimes/queries";

interface DesktopAPIWithTakeOver {
  takeOverIssue?: (
    issueId: string,
  ) => Promise<{
    ok: boolean;
    command?: string;
    workDir?: string;
    error?: string;
  }>;
}

function getDesktopAPI(): DesktopAPIWithTakeOver | null {
  if (typeof window === "undefined") return null;
  const w = window as unknown as { desktopAPI?: DesktopAPIWithTakeOver };
  return w.desktopAPI ?? null;
}

export interface UseTakeOverResult {
  /** True when the menu item should render (desktop + at least one resumable run). */
  visible: boolean;
  /** Triggers the IPC + clipboard copy + toast. No-op when not visible. */
  takeOver: () => Promise<void>;
}

/**
 * Drives the desktop "Take Over Locally" menu item. The hook gates its
 * own data-fetching on `enabled` so callers in board/list views (where
 * the action is never offered) don't pay for a runs/runtimes round-trip.
 *
 * The visibility decision lives in {@link canTakeOverLocally} so it can
 * be unit-tested without React.
 */
export function useTakeOver(
  issueId: string | null,
  opts: { enabled: boolean } = { enabled: true },
): UseTakeOverResult {
  const wsId = useWorkspaceId();
  const desktopAPI = getDesktopAPI();

  // Two short-circuits, in order:
  //   - the renderer is web (no `desktopAPI`) — the action could never
  //     execute, so don't fetch.
  //   - the caller hasn't asked for take-over (`enabled: false`) — the
  //     dropdown isn't shown on this surface.
  const queryEnabled = opts.enabled && !!desktopAPI && !!issueId;

  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId ?? ""),
    queryFn: () => api.listTasksByIssue(issueId!),
    enabled: queryEnabled,
    staleTime: 30_000,
  });

  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(wsId),
    enabled: queryEnabled,
  });

  const visible = useMemo(
    () =>
      canTakeOverLocally({
        tasks,
        runtimes,
        clientType: desktopAPI ? "desktop" : "web",
      }),
    [tasks, runtimes, desktopAPI],
  );

  const takeOver = useCallback(async () => {
    if (!issueId || !desktopAPI?.takeOverIssue) return;
    const result = await desktopAPI.takeOverIssue(issueId);
    if (!result.ok) {
      toast.error("Could not prepare take-over command", {
        description: result.error,
      });
      return;
    }
    // Workdir is parsed out of the `cd '...' && ` prefix by the main
    // process. When the worktree was GC'd the CLI omits the prefix, so
    // workDir is undefined — fall back to the command itself.
    const description = result.workDir
      ? result.workDir
      : (result.command ?? undefined);
    toast.success("Command copied. Paste in your terminal to take over.", {
      description,
    });
  }, [issueId, desktopAPI]);

  return { visible, takeOver };
}
