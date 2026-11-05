import {
  LocalComputerLinedIcon,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  WorktreeLinedIcon,
  cn
} from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import styles from "../AgentGUINode.styles";
import type { AgentGUISessionLaunchMode } from "../model/agentSessionLaunchMode";
const LaunchModeIcon = observer(LaunchModeIconRender);

export const AgentSessionLaunchModeSelect = observer(
  function AgentSessionLaunchModeSelect(
    input: AgentSessionLaunchModeSelectProps
  ): React.JSX.Element {
    return (
      <Select value={input.mode} onValueChange={input.onModeChange}>
        <SelectTrigger
          aria-label={input.labels.launchMode}
          className={cn("w-auto", styles.composerMenuTrigger)}
          data-testid="agent-gui-session-launch-mode"
        >
          <span className="flex min-w-0 items-center gap-2">
            <LaunchModeIcon mode={input.mode} />
            <span className="truncate">
              {input.mode === "worktree"
                ? input.labels.worktree
                : input.labels.local}
            </span>
          </span>
        </SelectTrigger>
        <SelectContent align="start">
          <SelectItem value="local">
            <span className="flex items-center gap-2">
              <LaunchModeIcon mode="local" />
              {input.labels.local}
            </span>
          </SelectItem>
          <SelectItem value="worktree">
            <span className="flex items-center gap-2">
              <LaunchModeIcon mode="worktree" />
              {input.labels.worktree}
            </span>
          </SelectItem>
        </SelectContent>
      </Select>
    );
  }
);

function LaunchModeIconRender({
  mode
}: {
  mode: AgentGUISessionLaunchMode;
}): React.JSX.Element {
  return mode === "worktree" ? (
    <WorktreeLinedIcon
      aria-hidden="true"
      data-agent-session-launch-icon="worktree"
      size={15}
    />
  ) : (
    <LocalComputerLinedIcon
      aria-hidden="true"
      data-agent-session-launch-icon="local"
      size={15}
    />
  );
}

type AgentSessionLaunchModeSelectProps = {
  labels: {
    launchMode: string;
    local: string;
    worktree: string;
  };
  mode: AgentGUISessionLaunchMode;
  onModeChange: (mode: AgentGUISessionLaunchMode) => void;
};
