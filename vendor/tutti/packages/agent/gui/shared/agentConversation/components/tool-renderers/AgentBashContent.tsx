import type { JSX } from "react";
import { AgentTerminalBlock } from "./terminal/AgentTerminalBlock";
import type { AgentToolRendererProps } from "./agentToolContentShared";
import {
  getCommandRenderData,
  getToolCallFailureText
} from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentBashContent = observer(AgentBashContentRender);

function AgentBashContentRender({ call }: AgentToolRendererProps): JSX.Element {
  "use memo";
  const commandData = getCommandRenderData(call);
  const structuredFailureText =
    commandData.status === "failed" &&
    !commandData.stdout &&
    !commandData.stderr
      ? getToolCallFailureText(call)
      : null;

  return (
    <div className="workspace-agents-status-panel__detail-tool-body workspace-agents-status-panel__detail-tool-body--plain">
      <AgentTerminalBlock
        command={commandData.command}
        stdout={commandData.stdout}
        stderr={commandData.stderr || structuredFailureText || ""}
        exitCode={commandData.exitCode}
        durationMs={commandData.durationMs}
        status={commandData.status}
      />
    </div>
  );
}
