import { useEffect, useState } from "react";
import type { DaemonStatus } from "../../../shared/daemon-types";

export interface DesktopRuntimeContext {
  localDaemonId: string | null;
  localMachineName: string | null;
  bootstrapping: boolean;
}

interface DaemonIdentity {
  daemonId: string | null;
  deviceName: string | null;
}

// Route transitions remount this hook. Keep the last daemon identity at the
// desktop platform boundary so a stopped daemon still maps to the same local
// machine on both the collection and detail routes.
let lastDaemonIdentity: DaemonIdentity = {
  daemonId: null,
  deviceName: null,
};

/** Shared desktop bridge for both the machine list and machine detail route. */
export function useDesktopRuntimeContext(): DesktopRuntimeContext {
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  const [lastIdentity, setLastIdentity] =
    useState<DaemonIdentity>(lastDaemonIdentity);
  const [hostName, setHostName] = useState<string | null>(null);

  useEffect(() => {
    const apply = (next: DaemonStatus) => {
      setStatus(next);
      if (next.daemonId) {
        const identity = {
          daemonId: next.daemonId,
          deviceName: next.deviceName ?? null,
        };
        lastDaemonIdentity = identity;
        setLastIdentity(identity);
      }
    };
    window.daemonAPI.getStatus().then(apply);
    window.daemonAPI.getHostName().then((name) => setHostName(name || null));
    return window.daemonAPI.onStatusChange(apply);
  }, []);

  return {
    localDaemonId: status.daemonId ?? lastIdentity.daemonId,
    localMachineName:
      status.deviceName ?? lastIdentity.deviceName ?? hostName,
    bootstrapping:
      status.state === "installing_cli" ||
      status.state === "starting" ||
      status.state === "running",
  };
}
