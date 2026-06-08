"use client";

import { useEffect, useMemo, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  runtimeListOptions,
  checkQuickCreateCliVersion,
  readRuntimeCliVersion,
} from "@multica/core/runtimes";
import type { Agent } from "@multica/core/types";

/**
 * useQuickCreateVersionAutoSwitch — FIR-33 (sub-issue of FIR-18).
 *
 * In the "Create with agent" modal, the daemon's bundled multica CLI must be
 * at least `MIN_QUICK_CREATE_CLI_VERSION` for the agent-create flow to work.
 * The upstream behaviour when the picked agent's runtime is below that gate is
 * to show a warning banner and disable Create — which leaves the user stuck
 * with no way forward.
 *
 * This hook turns that dead-end into an automatic fall-back: as soon as the
 * runtime list has loaded and the picked agent fails the version gate, it
 * calls `onSwitchToManual()` so the modal flips to manual create (which carries
 * the typed prompt, project, and parent over). The warning banner stays in the
 * upstream component as a backstop for when this hook is disabled or the modal
 * has no `onSwitchMode` channel to flip through.
 *
 * The decision (flag gate + version check + "loaded yet?") lives here in the
 * cerebro zone; the upstream modal only feeds in the picked agent + workspace
 * and the `switchToManual` callback.
 *
 * @param params.workspaceId      Active workspace id (keys the runtime query).
 * @param params.selectedAgent    The agent picked in the modal, or null/undefined.
 * @param params.onSwitchToManual Flip the modal to manual create (carries draft).
 */
export function useQuickCreateVersionAutoSwitch({
  workspaceId,
  selectedAgent,
  onSwitchToManual,
}: {
  workspaceId: string;
  selectedAgent: Agent | null | undefined;
  onSwitchToManual: () => void;
}): void {
  const flagOn = useFeatureFlag("cerebro_quick_create_version_autoswitch");

  // Re-uses the same query the modal already runs (TanStack dedupes by key),
  // so this adds no extra network round-trip — it just observes the cache.
  const { data: runtimes = [], isSuccess: runtimesLoaded } = useQuery(
    runtimeListOptions(workspaceId),
  );

  const selectedRuntime = useMemo(
    () =>
      selectedAgent?.runtime_id
        ? runtimes.find((r) => r.id === selectedAgent.runtime_id)
        : undefined,
    [runtimes, selectedAgent?.runtime_id],
  );

  const versionBlocked = useMemo(
    () =>
      checkQuickCreateCliVersion(readRuntimeCliVersion(selectedRuntime?.metadata))
        .state !== "ok",
    [selectedRuntime?.metadata],
  );

  // Fire at most once per picked agent. After a successful switch the modal
  // unmounts this panel, so this guard only matters when `onSwitchToManual` is
  // a no-op channel (e.g. unit tests, or a missing `onSwitchMode`) — there it
  // stops the effect from looping every render.
  const switchedForRef = useRef<string | null>(null);

  useEffect(() => {
    if (!flagOn || !runtimesLoaded || !selectedAgent || !versionBlocked) return;
    if (switchedForRef.current === selectedAgent.id) return;
    switchedForRef.current = selectedAgent.id;
    onSwitchToManual();
  }, [
    flagOn,
    runtimesLoaded,
    selectedAgent,
    versionBlocked,
    onSwitchToManual,
  ]);
}
