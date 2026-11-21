"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "../types";
import { agentListOptions } from "../workspace/queries";
import { runtimeListOptions } from "../runtimes/queries";
import {
  agentEffectiveRuntimeId,
  type EffectiveRuntimeAgent,
} from "./effective-runtime";

export function resolveAgentRuntimeProvider(
  agent: EffectiveRuntimeAgent | undefined,
  runtimes: AgentRuntime[] | undefined,
): string | null {
  if (!agent || !runtimes) return null;
  const runtimeId = agentEffectiveRuntimeId(agent);
  if (!runtimeId) return null;
  const runtime = runtimes.find((candidate) => candidate.id === runtimeId);
  return runtime?.provider?.trim() || null;
}

/**
 * The provider slug ("claude", "codex", …) backing an agent's runtime, or
 * `null` when it can't be resolved — lists still loading, an agent outside the
 * workspace list (archived assignee on an old issue), a runtime that no longer
 * exists, or a backend that omitted the field. Callers render nothing on null
 * rather than a placeholder: a wrong provider mark is worse than none.
 *
 * Derived from the effective runtime id on every read instead of stored on the
 * agent, so moving an agent from one runtime to another moves its provider
 * mark with it. Smart-runtime agents use the current viewer's resolved runtime
 * rather than the agent template's recommended runtime.
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
    if (!agentId) return null;
    const agent = agents?.find((a) => a.id === agentId);
    return resolveAgentRuntimeProvider(agent, runtimes);
  }, [agentId, agents, runtimes]);
}
