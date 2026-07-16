// FIR-3212 Swap slice — client for the runtime swap preview.
//
// GET /api/agents/{id}/capabilities/swap?runtime_id={target} answers "what does
// this agent lose if I move it to that engine?". The diff itself is computed on
// the server, next to the support matrix it is tested against; this client only
// carries it. Nothing here re-derives an outcome from handling strings — that
// would be the same knowledge in two places, and the copy in the browser is the
// one that never gets checked against an installed CLI.
//
// Per the API Response Compatibility rules (CLAUDE.md) every field is parsed
// with an explicit fallback rather than cast: a drifting backend must downgrade
// the panel to "we cannot say", never white-screen the Setup tab. Outcome and
// handling strings are kept as plain strings so an unrecognised value from a
// newer server renders as a neutral badge instead of throwing.

import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import {
  AgentCapabilityRuntimeOptionsSchema,
  EMPTY_RUNTIME_OPTIONS,
} from "./api";

const SwapFieldTransitionSchema = z
  .object({
    field: z.string().catch(""),
    from_handling: z.string().catch(""),
    to_handling: z.string().catch(""),
    // outcome is a server enum (retained|lost|gained|unavailable); an unknown
    // value must survive to the UI's default branch.
    outcome: z.string().catch(""),
    // The bit the warning hierarchy is built on: the target drops this value
    // with no log line, so the operator would keep believing it is enforced.
    silent_on_target: z.boolean().catch(false),
  })
  .loose();

const SwapSystemPromptTransitionSchema = z
  .object({
    from_native: z.boolean().catch(false),
    to_native: z.boolean().catch(false),
    from_modes: z.array(z.string()).catch([]),
    to_modes: z.array(z.string()).catch([]),
    // unchanged | downgraded_to_prepend | ignored | gained_native
    outcome: z.string().catch(""),
  })
  .loose();

const EMPTY_IMPACT = {
  status: "unknown",
  from: "",
  to: "",
  fields: [],
  lost: [],
  gained: [],
  silently_lost: [],
};

const SwapImpactSchema = z
  .object({
    // status=unknown means the matrix has no entry for one of the two engines.
    // It never means the swap loses everything — the UI must say "we cannot
    // say" and enumerate nothing.
    status: z.string().catch("unknown"),
    from: z.string().catch(""),
    to: z.string().catch(""),
    fields: z.array(SwapFieldTransitionSchema).catch([]),
    lost: z.array(z.string()).catch([]),
    gained: z.array(z.string()).catch([]),
    silently_lost: z.array(z.string()).catch([]),
    system_prompt: SwapSystemPromptTransitionSchema.optional(),
  })
  .loose()
  .catch(EMPTY_IMPACT);

export const AgentCapabilitySwapSchema = z
  .object({
    agent_id: z.string().catch(""),
    current: AgentCapabilityRuntimeOptionsSchema.catch(EMPTY_RUNTIME_OPTIONS),
    target: AgentCapabilityRuntimeOptionsSchema.catch(EMPTY_RUNTIME_OPTIONS),
    same_runtime: z.boolean().catch(false),
    impact: SwapImpactSchema.default(EMPTY_IMPACT),
  })
  .loose();

export type AgentCapabilitySwapFieldTransition = z.infer<
  typeof SwapFieldTransitionSchema
>;
export type AgentCapabilitySwapSystemPrompt = z.infer<
  typeof SwapSystemPromptTransitionSchema
>;
export type AgentCapabilitySwapImpact = z.infer<typeof SwapImpactSchema>;
export type AgentCapabilitySwap = z.infer<typeof AgentCapabilitySwapSchema>;

const EMPTY_SWAP: AgentCapabilitySwap = {
  agent_id: "",
  current: EMPTY_RUNTIME_OPTIONS,
  target: EMPTY_RUNTIME_OPTIONS,
  same_runtime: false,
  impact: EMPTY_IMPACT,
};

export async function getAgentCapabilitySwap(
  agentId: string,
  targetRuntimeId: string,
): Promise<AgentCapabilitySwap> {
  const path = `/api/agents/${agentId}/capabilities/swap?runtime_id=${encodeURIComponent(targetRuntimeId)}`;
  const raw = await api.cerebroRequest<unknown>(path);
  return parseWithFallback(raw, AgentCapabilitySwapSchema, EMPTY_SWAP, {
    endpoint: path,
  }) as AgentCapabilitySwap;
}
