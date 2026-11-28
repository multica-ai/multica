"use client";

import { agentEffectiveRuntimeId, type EffectiveRuntimeAgent } from "@multica/core/agents";
import { useQuery } from "@tanstack/react-query";
import {
  runtimeListOptions,
  readRuntimeCliVersion,
  chatProjectContextSupported,
} from "@multica/core/runtimes";

/**
 * Whether the active agent's daemon is new enough to inject a chat session's
 * project description into the run brief. `null` means "cannot tell" (no
 * agent, no bound runtime, or the runtime row not in cache yet) and must NOT
 * warn: this is a soft gate, and a spurious warning is worse than a
 * description an old daemon drops.
 *
 * Shared by both send chains (chat tab controller and floating ChatWindow) so
 * the resolution rule cannot drift between the two surfaces.
 */
export function useChatProjectContextSupport(
  wsId: string,
  agent: (Omit<EffectiveRuntimeAgent, "runtime_id"> & { runtime_id?: string | null }) | null | undefined,
): boolean | null {
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  const runtimeId = agent ? agentEffectiveRuntimeId({ ...agent, runtime_id: agent.runtime_id ?? "" }) : null;
  if (!runtimeId) return null;
  const runtime = runtimes.find((r) => r.id === runtimeId);
  if (!runtime) return null;
  return chatProjectContextSupported(readRuntimeCliVersion(runtime.metadata));
}
