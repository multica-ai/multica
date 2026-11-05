import type { AgentHostInputApi } from "@tutti-os/agent-gui";

export type ComposerCommandName = "side" | "reasoning" | "model" | "status";
export interface ComposerCommandRequest {
  name: ComposerCommandName;
  agentSessionId: string | null;
  sequence: number;
}

export function composerCommands(
  chinese: boolean,
  side: boolean,
  execute: (name: ComposerCommandName, agentSessionId: string | null) => void
): NonNullable<AgentHostInputApi["composerCommands"]> {
  const commands = [
    ...(side ? [{ name: "side" as const, label: chinese ? "侧边对话" : "Side conversation" }] : []),
    { name: "reasoning" as const, label: chinese ? "推理" : "Reasoning" },
    { name: "model" as const, label: chinese ? "模型" : "Model" },
    { name: "status" as const, label: chinese ? "状态" : "Status" }
  ];
  return {
    commands,
    execute: async ({ name, agentSessionId }) => {
      const command = commands.find((entry) => entry.name === name);
      if (command) execute(command.name, agentSessionId);
    }
  };
}
