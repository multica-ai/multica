"use client";

import { useEffect, useRef, useState } from "react";
import type { Agent } from "@multica/core/types";

/**
 * Runtime policy for the creation flow.
 *
 * Kept beside the create pages instead of inside `AgentDraft` so the upstream
 * draft shape, its stored form and the builder protocol stay untouched — the
 * flag is submitted on create and never round-trips through a stored draft.
 *
 * A blank create defaults to smart runtime (`share_runtime=false`): the agent
 * runs on each caller's own runtime with the bound one as the recommendation.
 * A duplicate inherits the source agent's policy, where a missing value means
 * the legacy fixed-runtime behaviour.
 */
export function useRuntimeStrategy(duplicateSource?: Agent | null) {
  const [shareRuntime, setShareRuntime] = useState(false);
  // The duplicate source arrives asynchronously (it waits for the agent list),
  // and the user may flip the policy afterwards — seed once per source.
  const seededSourceId = useRef<string | null>(null);

  useEffect(() => {
    if (!duplicateSource) return;
    if (seededSourceId.current === duplicateSource.id) return;
    seededSourceId.current = duplicateSource.id;
    setShareRuntime(duplicateSource.share_runtime ?? true);
  }, [duplicateSource]);

  return {
    shareRuntime,
    setShareRuntime,
  };
}
