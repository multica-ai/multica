import type { EngineDiagnosticSink } from "./diagnostics.ts";
import type { AgentActivitySendInput } from "../types.ts";
import type {
  AgentSessionActivateEffectInput,
  EngineClock,
  EngineCommandResultContract,
  EngineCommandResultIntent,
  EngineExternalCommand,
  EngineScheduledTask,
  EngineScheduler,
  EngineTypedCommandPort
} from "./types.ts";

// Effect executor: performs external command descriptions through the
// injected command port and feeds every settlement (success, failure,
// timeout) back into the dispatch loop as a command-result intent.
//
// The executor holds no decision logic. Retrying, fallback, and recovery are
// reducer transitions, never executed in place here.

export interface CreateEngineEffectExecutorInput {
  clock: EngineClock;
  commandPort: EngineTypedCommandPort;
  diagnosticSink?: EngineDiagnosticSink;
  onResult: (intent: EngineCommandResultIntent) => void;
  scheduler: EngineScheduler;
}

export interface EngineEffectExecutor {
  abort(commandId: string, reason: string): void;
  dispose(): void;
  execute(command: EngineExternalCommand): void;
}

export function createEngineEffectExecutor({
  clock,
  commandPort,
  diagnosticSink,
  onResult,
  scheduler
}: CreateEngineEffectExecutorInput): EngineEffectExecutor {
  let disposed = false;
  const timeoutTasks = new Set<EngineScheduledTask>();
  const abortControllersByCommandId = new Map<string, AbortController>();

  const settle = (intent: EngineCommandResultIntent): void => {
    if (disposed) {
      diagnosticSink?.({
        commandId: intent.commandId,
        type: "commandResultAfterDispose"
      });
      return;
    }
    onResult(intent);
  };

  return {
    abort(commandId, reason) {
      const controller = abortControllersByCommandId.get(commandId.trim());
      if (!controller || controller.signal.aborted) return;
      const error = new Error(
        reason.trim() || "engine command aborted"
      ) as Error & { code: string };
      error.code = "aborted";
      error.name = "AbortError";
      controller.abort(error);
    },
    dispose() {
      disposed = true;
      for (const task of timeoutTasks) {
        task.cancel();
      }
      timeoutTasks.clear();
      for (const controller of abortControllersByCommandId.values()) {
        controller.abort(new Error("engine disposed"));
      }
      abortControllersByCommandId.clear();
    },
    execute(command) {
      commandPort.observe?.(command);
      let settled = false;
      let timeoutTask: EngineScheduledTask | null = null;
      const abortController = new AbortController();
      abortControllersByCommandId.set(command.commandId, abortController);
      const resultContract = commandResultContract(command);
      const execution = executeCommand(
        commandPort,
        command,
        abortController.signal
      );

      const finishTimeoutTask = (): void => {
        if (
          abortControllersByCommandId.get(command.commandId) === abortController
        ) {
          abortControllersByCommandId.delete(command.commandId);
        }
        if (timeoutTask !== null) {
          timeoutTasks.delete(timeoutTask);
          timeoutTask.cancel();
          timeoutTask = null;
        }
      };

      const timeoutMs = "timeoutMs" in command ? command.timeoutMs : undefined;
      if (timeoutMs !== undefined) {
        timeoutTask = scheduler.schedule(timeoutMs, () => {
          if (settled) {
            return;
          }
          settled = true;
          abortController.abort(new Error("engine command timed out"));
          finishTimeoutTask();
          settle({
            commandId: command.commandId,
            commandType: command.type,
            ...commandCorrelationFields(command),
            outcome: "timedOut",
            settledAtUnixMs: clock.nowUnixMs(),
            resultContract,
            type: "engine/commandResult"
          });
        });
        timeoutTasks.add(timeoutTask);
      }

      execution.then(
        (value) => {
          if (settled) {
            diagnosticSink?.({
              commandId: command.commandId,
              type: "commandResultAfterTimeout"
            });
            return;
          }
          settled = true;
          finishTimeoutTask();
          settle({
            commandId: command.commandId,
            commandType: command.type,
            ...commandCorrelationFields(command),
            outcome: "succeeded",
            settledAtUnixMs: clock.nowUnixMs(),
            resultContract,
            type: "engine/commandResult",
            value
          });
        },
        (error: unknown) => {
          if (settled) {
            diagnosticSink?.({
              commandId: command.commandId,
              type: "commandResultAfterTimeout"
            });
            return;
          }
          settled = true;
          finishTimeoutTask();
          settle({
            commandId: command.commandId,
            commandType: command.type,
            ...commandCorrelationFields(command),
            ...engineCommandErrorFields(error),
            outcome: "failed",
            settledAtUnixMs: clock.nowUnixMs(),
            resultContract,
            type: "engine/commandResult"
          });
        }
      );
    }
  };
}

function executeCommand(
  commandPort: EngineTypedCommandPort,
  command: EngineExternalCommand,
  signal: AbortSignal
): Promise<unknown> {
  const effects = commandPort.effects;
  switch (command.type) {
    case "session/activate":
      return effects.activateSession(activationInput(command), {
        commandId: command.commandId,
        origin: "engine",
        signal
      });
    case "session/updateSettings":
      return effects.updateSessionSettings(
        {
          agentSessionId: command.agentSessionId,
          commandId: command.commandId,
          correlationId: command.correlationId,
          settings: command.settings,
          workspaceId: command.workspaceId
        },
        { commandId: command.commandId, origin: "engine", signal }
      );
    default:
      return commandPort.execute(command, {
        commandId: command.commandId,
        origin: "engine",
        signal
      });
  }
}

function commandResultContract(
  command: EngineExternalCommand
): EngineCommandResultContract {
  if (command.type === "session/activate") return "activation-v1";
  return "opaque";
}

function activationInput(
  command: Extract<EngineExternalCommand, { type: "session/activate" }>
): AgentSessionActivateEffectInput {
  const shared = {
    activationId: command.correlationId,
    agentSessionId: command.agentSessionId,
    ...(command.capabilityRefs?.length
      ? { capabilityRefs: command.capabilityRefs }
      : {}),
    ...(command.cwd !== undefined ? { cwd: command.cwd } : {}),
    ...(command.initialContent
      ? { initialContent: [...command.initialContent] }
      : {}),
    ...(command.initialDisplayPrompt !== undefined
      ? { initialDisplayPrompt: command.initialDisplayPrompt }
      : {}),
    ...(command.isolation ? { isolation: command.isolation } : {}),
    ...(command.modelExplicit !== undefined
      ? { modelExplicit: command.modelExplicit }
      : {}),
    ...(command.railPlacement
      ? { railPlacement: { ...command.railPlacement } }
      : {}),
    ...(command.reasoningEffortExplicit !== undefined
      ? { reasoningEffortExplicit: command.reasoningEffortExplicit }
      : {}),
    ...(command.settings ? { settings: { ...command.settings } } : {}),
    ...(command.submitDiagnostics
      ? { submitDiagnostics: { ...command.submitDiagnostics } }
      : {}),
    ...(command.title !== undefined ? { title: command.title } : {}),
    ...(command.visible !== undefined ? { visible: command.visible } : {}),
    workspaceId: command.workspaceId
  };
  return command.mode === "new"
    ? {
        ...shared,
        agentTargetId: command.agentTargetId,
        clientSubmitId: command.clientSubmitId,
        mode: "new"
      }
    : {
        ...shared,
        agentTargetId: command.agentTargetId,
        mode: "existing"
      };
}

function commandCorrelationFields(command: EngineExternalCommand): {
  correlationId?: string;
} {
  if (!("correlationId" in command)) {
    return {};
  }
  const value = command.correlationId;
  return typeof value === "string" && value.trim()
    ? { correlationId: value.trim() }
    : {};
}

function engineCommandErrorFields(error: unknown): {
  errorCode?: string;
  errorReason?: string;
  errorMessage: string;
} {
  const record =
    error && typeof error === "object"
      ? (error as Record<string, unknown>)
      : null;
  const code = typeof record?.code === "string" ? record.code.trim() : "";
  const normalizedCode =
    code ||
    (error instanceof Error && error.name === "AbortError" ? "aborted" : "");
  const reason = typeof record?.reason === "string" ? record.reason.trim() : "";
  const message =
    error instanceof Error
      ? error.message
      : typeof record?.message === "string"
        ? record.message
        : String(error);
  return {
    ...(normalizedCode ? { errorCode: normalizedCode } : {}),
    ...(reason ? { errorReason: reason } : {}),
    errorMessage: message
  };
}
