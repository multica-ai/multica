import { useConfigStore } from "../config";
import { daemonCommands } from "./commands";

export function useDaemonSetupCommands() {
  const status = useConfigStore((s) => s.daemonConfigStatus);
  const target = useConfigStore((s) => s.daemonSetupTarget);
  const serverUrl = useConfigStore((s) => s.daemonServerUrl);
  const appUrl = useConfigStore((s) => s.daemonAppUrl);
  return daemonCommands({ status, target, serverUrl, appUrl });
}
