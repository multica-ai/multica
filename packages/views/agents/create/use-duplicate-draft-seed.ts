"use client";

import { useEffect, useRef } from "react";
import { buildDuplicateDraft, type AgentDraft } from "@multica/core/agents";
import type { Agent, RuntimeDevice } from "@multica/core/types";

/**
 * Seeds the duplicate draft exactly once, and only from a runtime list that has
 * actually answered.
 *
 * The ordering matters: the agent list and the runtime list are independent
 * queries, so on a cold start (a direct `?duplicate=<id>` link, or a refresh)
 * the agent can arrive while runtimes are still pending. Seeding then would run
 * `buildDuplicateDraft` against `[]`, read the source runtime as unavailable,
 * and clear the model / thinking / speed that a same-runtime copy must keep —
 * permanently, because seeding happens once. Re-running on every runtime change
 * instead would overwrite edits the user already made, so the gate is on the
 * query being decidable rather than on the data changing.
 */
export function useDuplicateDraftSeed({
  source,
  runtimesSettled,
  runtimes,
  currentUserId,
  fallbackRuntimeId,
  nameSuffix,
  onSeed,
}: {
  source: Agent | null;
  /** The runtime query resolved or failed — `false` while it is pending. */
  runtimesSettled: boolean;
  runtimes: RuntimeDevice[];
  currentUserId: string | null;
  fallbackRuntimeId: string;
  nameSuffix: string;
  onSeed: (draft: AgentDraft, runtimeReset: boolean) => void;
}): void {
  const seededRef = useRef(false);
  // The callback is recreated every render by design (it closes over setState),
  // so keep it out of the effect's dependency list.
  const onSeedRef = useRef(onSeed);
  onSeedRef.current = onSeed;

  useEffect(() => {
    if (seededRef.current || !source || !runtimesSettled) return;
    seededRef.current = true;
    const draft = buildDuplicateDraft(source, {
      runtimes,
      currentUserId,
      fallbackRuntimeId,
      nameSuffix,
    });
    onSeedRef.current(draft, draft.runtimeId !== source.runtime_id);
  }, [
    currentUserId,
    fallbackRuntimeId,
    nameSuffix,
    runtimes,
    runtimesSettled,
    source,
  ]);
}
