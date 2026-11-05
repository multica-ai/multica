import { ChooseRuntimePage } from "@multica/views/agents";
import { useDesktopRuntimeContext } from "./use-desktop-runtime-context";

/**
 * Desktop wrapper around step 1 of agent creation: only the desktop app knows
 * which daemon is "this device", which the "this device" section is built from.
 */
export function DesktopChooseRuntimePage() {
  const context = useDesktopRuntimeContext();
  return (
    <ChooseRuntimePage
      localDaemonId={context.localDaemonId}
      localMachineName={context.localMachineName}
      hasLocalMachine
      bootstrapping={context.bootstrapping}
    />
  );
}
