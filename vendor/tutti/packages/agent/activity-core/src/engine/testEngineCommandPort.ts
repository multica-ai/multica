import type {
  AgentSessionEffectPort,
  EngineEffectOptions,
  EngineExternalCommand,
  EngineTypedCommandPort
} from "./types.ts";

export type TestEngineCommandExecutor = (
  command: EngineExternalCommand,
  options?: EngineEffectOptions
) => Promise<unknown>;

/** Test-only adapter that records typed effects as their originating command. */
export function createTestEngineCommandPort(
  execute: TestEngineCommandExecutor = async () => ({ ok: true })
): EngineTypedCommandPort {
  const commands = new Map<string, EngineExternalCommand>();

  function executeObserved<TResult>(
    options: EngineEffectOptions | undefined
  ): Promise<TResult> {
    const commandId = options?.commandId.trim();
    const command = commandId ? commands.get(commandId) : undefined;
    if (!command) {
      return Promise.reject(
        new Error(`test command was not observed: ${commandId ?? "unknown"}`)
      );
    }
    return execute(command, options) as Promise<TResult>;
  }

  const effects: AgentSessionEffectPort = {
    activateSession(_input, options) {
      return executeObserved(options);
    },
    updateSessionSettings(_input, options) {
      return executeObserved(options);
    }
  };

  return {
    effects,
    execute(command, options) {
      return execute(command, options);
    },
    kind: "typed",
    observe(command) {
      commands.set(command.commandId, command);
    }
  };
}
