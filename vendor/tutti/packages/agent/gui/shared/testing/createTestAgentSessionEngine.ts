import {
  AGENT_SESSION_ENGINE_LOCAL_ORIGIN,
  createAgentSessionEngine,
  type AgentSessionEffectPort,
  type AgentSessionEngine,
  type EngineEffectOptions,
  type EngineExternalCommand,
  type EngineTypedCommandPort,
} from "@tutti-os/agent-activity-core";

export interface TestEngineCommandHandler {
  execute(
    command: EngineExternalCommand,
    options?: EngineEffectOptions
  ): Promise<unknown>;
}

export function createTestAgentSessionEngine(
  workspaceId = "test-workspace",
  commandHandler: TestEngineCommandHandler = {
    execute: async () => ({ ok: true })
  }
): AgentSessionEngine {
  return createEngine(workspaceId, createTestEngineCommandPort(commandHandler));
}

export function createTestAgentSessionEngineWithEffects(
  workspaceId: string,
  effectOverrides: Partial<AgentSessionEffectPort>
): AgentSessionEngine {
  const commandPort = createTestEngineCommandPort({
    execute: async () => ({ ok: true })
  });
  return createEngine(workspaceId, {
    ...commandPort,
    effects: { ...commandPort.effects, ...effectOverrides }
  });
}

function createEngine(
  workspaceId: string,
  commandPort: EngineTypedCommandPort
): AgentSessionEngine {
  const engine = createAgentSessionEngine({
    clock: { nowUnixMs: () => Date.now() },
    commandPort,
    identity: { origin: AGENT_SESSION_ENGINE_LOCAL_ORIGIN, workspaceId },
    scheduler: {
      schedule(delayMs, task) {
        const timer = setTimeout(task, delayMs);
        return { cancel: () => clearTimeout(timer) };
      }
    }
  });
  engine.dispatch({ type: "workspace/reconcileRequested", workspaceId });
  return engine;
}

export function createTestEngineCommandPort(
  commandHandler: TestEngineCommandHandler
): EngineTypedCommandPort {
  const commands = new Map<string, EngineExternalCommand>();
  function executeObserved<TResult>(
    options: EngineEffectOptions | undefined
  ): Promise<TResult> {
    const command = options ? commands.get(options.commandId) : undefined;
    if (!command) {
      return Promise.reject(new Error("test Engine command was not observed"));
    }
    return commandHandler.execute(command, options) as Promise<TResult>;
  }
  const effects: AgentSessionEffectPort = {
    activateSession: (_input, options) => executeObserved(options),
    sendInput: (_input, options) => executeObserved(options),
    updateSessionSettings: (_input, options) => executeObserved(options)
  };
  return {
    effects,
    execute(command, options) {
      return commandHandler.execute(command, options);
    },
    kind: "typed",
    observe(command) {
      commands.set(command.commandId, command);
    }
  };
}
