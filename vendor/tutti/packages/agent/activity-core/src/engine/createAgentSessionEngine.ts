import type { EngineDiagnosticSink } from "./diagnostics.ts";
import type { AgentActivityComposerOptions } from "../types.ts";
import { projectPublicAgentSessionEngineState } from "./engineState.publicProjection.ts";
import { createEngineEffectExecutor } from "./effectExecutor.ts";
import { createEngineExpiryClock } from "./expiryClock.ts";
import {
  intentForEngineIdentity,
  withEngineObservationTime
} from "./engineIntent.normalization.ts";
import {
  selectEngineActiveTurn,
} from "./sessionLifecycle.selectors.ts";
import { selectPendingSubmitsForSession } from "./pendingIntents.selectors.ts";
import {
  selectEngineHasVisibleQueuedSubmit,
  selectEngineSubmitWouldBeVisibleInQueue
} from "./promptQueue.selectors.ts";
import { requestSessionActivation } from "./sessionActivation.operation.ts";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import {
  isEngineInternalCommand,
  type AgentSessionEngine,
  type AgentSessionEngineIdentity,
  type AgentSessionEngineIntentObserver,
  type AgentSessionEngineListener,
  type AgentSessionActivationInput,
  type AgentSessionSubmitPromptInput,
  type AgentSessionSubmitPromptResult,
  type EngineClock,
  type EngineDispatchOptions,
  type EngineIntent,
  type EngineScheduledTask,
  type EngineScheduler,
  type EngineTypedCommandPort
} from "./types.ts";
import type {
  RootAgentSessionEngineState,
  RootEngineIntent
} from "./rootReducer.types.ts";

// All engine state lives inside this closure: one instance per workspace + origin
// pair, injected explicitly by the host. The dispatch loop is serial — an
// intent dispatched while a drain is running is queued and reduced within the
// same drain — and subscribers are notified at most once per drain cycle.

/**
 * Frame window for coalescing high-frequency intents (streaming message
 * updates). Migrated from the desktop activity service's 33ms event batching;
 * the engine owns this timing once event wiring moves over in later slices.
 */
export const ENGINE_INTENT_BATCH_DELAY_MS = 33;

export interface CreateAgentSessionEngineInput {
  batchDelayMs?: number;
  clock: EngineClock;
  commandPort: EngineTypedCommandPort;
  diagnosticSink?: EngineDiagnosticSink;
  identity: AgentSessionEngineIdentity;
  intentObserver?: AgentSessionEngineIntentObserver;
  scheduler: EngineScheduler;
}

export function createAgentSessionEngine({
  batchDelayMs = ENGINE_INTENT_BATCH_DELAY_MS,
  clock,
  commandPort,
  diagnosticSink,
  identity,
  intentObserver,
  scheduler
}: CreateAgentSessionEngineInput): AgentSessionEngine {
  if (identity.workspaceId.trim().length === 0) {
    throw new Error("agent session engine requires a non-empty workspaceId");
  }
  if (identity.origin.trim().length === 0) {
    throw new Error("agent session engine requires a non-empty origin");
  }
  const engineIdentity: AgentSessionEngineIdentity = Object.freeze({
    origin: identity.origin,
    workspaceId: identity.workspaceId
  });

  let state: RootAgentSessionEngineState =
    createInitialAgentSessionEngineState();
  let publicSnapshot = projectPublicAgentSessionEngineState(state);
  const listeners = new Set<AgentSessionEngineListener>();
  const intentQueue: RootEngineIntent[] = [];
  const batchedIntents: EngineIntent[] = [];
  let batchFlushTask: EngineScheduledTask | null = null;
  let draining = false;
  let disposed = false;

  const expiryClock = createEngineExpiryClock({
    clock,
    onExpired: (intent) => {
      dispatch(intent);
    },
    scheduler
  });

  const effectExecutor = createEngineEffectExecutor({
    clock,
    commandPort,
    onResult: (intent) => {
      dispatch(intent);
    },
    scheduler,
    ...(diagnosticSink === undefined ? {} : { diagnosticSink })
  });

  function notifyListeners(): void {
    for (const listener of listeners) {
      try {
        listener(publicSnapshot);
      } catch (error) {
        if (diagnosticSink) {
          diagnosticSink({ error, type: "listenerError" });
        } else {
          console.error(
            "[agent-session-engine-diagnostic]",
            JSON.stringify({
              event: "listener_error",
              error: error instanceof Error ? error.message : String(error),
              origin: engineIdentity.origin,
              workspaceId: engineIdentity.workspaceId
            })
          );
        }
      }
    }
  }

  function drainQueue(): void {
    if (draining || disposed) {
      return;
    }
    draining = true;
    const publicSnapshotBeforeDrain = publicSnapshot;
    try {
      for (
        let intent = intentQueue.shift();
        intent !== undefined;
        intent = intentQueue.shift()
      ) {
        const result = rootEngineReducer(state, intent);
        if (result.state !== state) {
          state = result.state;
          publicSnapshot = projectPublicAgentSessionEngineState(
            state,
            publicSnapshot
          );
        }
        if (result.followUpIntents?.length) {
          intentQueue.unshift(...result.followUpIntents);
        }
        for (const command of result.commands) {
          if (command.type === "engine/abortExternalCommand") {
            effectExecutor.abort(command.targetCommandId, command.reason);
          } else if (isEngineInternalCommand(command)) {
            expiryClock.apply(command);
          } else {
            effectExecutor.execute(command);
          }
        }
      }
    } finally {
      draining = false;
    }
    if (publicSnapshot !== publicSnapshotBeforeDrain) {
      notifyListeners();
    }
  }

  function flushBatchedIntents(): void {
    if (batchFlushTask !== null) {
      batchFlushTask.cancel();
      batchFlushTask = null;
    }
    if (batchedIntents.length === 0) {
      return;
    }
    intentQueue.push(...batchedIntents);
    batchedIntents.length = 0;
  }

  function dispatch(
    intent: EngineIntent,
    options?: EngineDispatchOptions
  ): void {
    if (disposed) {
      diagnosticSink?.({
        intentType: intent.type,
        type: "intentDroppedAfterDispose"
      });
      return;
    }
    const scopedIntent = intentForEngineIdentity(
      withEngineObservationTime(intent, clock),
      engineIdentity
    );
    if (!scopedIntent) {
      diagnosticSink?.({
        intentType: intent.type,
        type: "intentDroppedForIdentityMismatch"
      });
      return;
    }
    if (intentObserver) {
      try {
        intentObserver(scopedIntent);
      } catch (error) {
        diagnosticSink?.({
          error,
          intentType: scopedIntent.type,
          type: "intentObserverError"
        });
      }
    }
    if (options?.batch === true) {
      batchedIntents.push(scopedIntent);
      if (batchFlushTask === null) {
        batchFlushTask = scheduler.schedule(batchDelayMs, () => {
          batchFlushTask = null;
          flushBatchedIntents();
          drainQueue();
        });
      }
      return;
    }
    // Non-batched dispatch flushes the pending frame first so cross-intent
    // ordering is preserved (a terminal event never overtakes the streaming
    // updates that preceded it).
    flushBatchedIntents();
    intentQueue.push(scopedIntent);
    drainQueue();
  }

  function activateSession(input: AgentSessionActivationInput): boolean {
    return requestSessionActivation(
      {
        clock,
        dispatch,
        getSnapshot: () => publicSnapshot,
        workspaceId: engineIdentity.workspaceId
      },
      input
    );
  }

  // Sending belongs to the prompt service; a host presents it over this engine.
  function submitPrompt(): AgentSessionSubmitPromptResult {
    return { accepted: false, queued: false };
  }

  const engine: AgentSessionEngine = {
    activateSession,
    identity: engineIdentity,
    dispatch,
    dispose() {
      if (disposed) {
        return;
      }
      disposed = true;
      if (batchFlushTask !== null) {
        batchFlushTask.cancel();
        batchFlushTask = null;
      }
      batchedIntents.length = 0;
      intentQueue.length = 0;
      expiryClock.dispose();
      effectExecutor.dispose();
      listeners.clear();
    },
    getSnapshot() {
      return publicSnapshot;
    },
    submitPrompt,
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    }
  };
  return engine;
}
