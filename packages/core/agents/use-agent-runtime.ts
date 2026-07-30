"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentListOptions } from "../workspace/queries";
import { runtimeListOptions } from "../runtimes/queries";

/**
 * The provider slug ("claude", "codex", …) backing an agent's runtime, or
 * `null` when it can't be resolved — lists still loading, an agent outside the
 * workspace list (archived assignee on an old issue), a runtime that no longer
 * exists, or a backend that omitted the field. Callers render nothing on null
 * rather than a placeholder: a wrong provider mark is worse than none.
 *
 * Derived from `runtime_id` on every read instead of stored on the agent, so
 * moving an agent from one runtime to another moves its provider mark with it.
 *
 * Both queries are ones the surfaces that show a provider mark already
 * subscribe to (`useActorName`, presence), so this costs a cache read rather
 * than a request.
 */
export function useAgentRuntimeProvider(
  wsId: string | undefined,
  agentId: string | undefined,
): string | null {
  const { data: agents } = useQuery({
    ...agentListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const { data: runtimes } = useQuery({
    ...runtimeListOptions(wsId ?? ""),
    enabled: !!wsId,
  });

  return useMemo(() => {
    if (!agentId || !agents || !runtimes) return null;
    const agent = agents.find((a) => a.id === agentId);
    if (!agent) return null;
    const runtime = runtimes.find((r) => r.id === agent.runtime_id);
    return runtime?.provider?.trim() || null;
  }, [agentId, agents, runtimes]);
}
