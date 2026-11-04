import type {
  AgentSessionEngineStateBase,
  EngineCommand,
  EngineIntent
} from "./types.ts";

/**
 * Root-reducer-only intents. Hosts dispatch `EngineIntent`; continuations in
 * this union are created and drained entirely inside one Engine instance.
 */
export type RootEngineIntent = EngineIntent;

/**
 * Root-reducer-only state. Public snapshots project the host-observable state
 * and omit execution bookkeeping.
 */
export type RootAgentSessionEngineState = AgentSessionEngineStateBase;

export interface RootEngineReducerResult<TState> {
  commands: readonly EngineCommand[];
  followUpIntents?: readonly RootEngineIntent[];
  state: TState;
}
