import { useEffect, useState } from "react";
import { RuntimesPage } from "@multica/views/runtimes";
import { DaemonRuntimeActions } from "./daemon-runtime-card";
import type { DaemonStatus } from "../../../shared/daemon-types";

/**
 * Desktop wrapper around the shared `RuntimesPage`. Bridges the Electron
 * `daemonAPI` (main-process daemon state) into the page so its empty
 * state can distinguish "no runtime registered" from "runtime is on its
 * way" — without the bundled daemon's status, the page shows a
 * misleading "Run multica daemon start" hint during the few seconds
 * between page load and the daemon's first registration.
 *
 * `bootstrapping` is true while the daemon is installing, starting, or
 * already running but hasn't surfaced as a server-side runtime yet.
 * RuntimeList only shows the spinner when the runtime list is also
 * empty, so once the daemon registers (and the list fills) the flag
 * has no visible effect.
 */
export function DesktopRuntimesPage() {
  const [status, setStatus] = useState<DaemonStatus>({ state: "stopped" });
  // Remember the last known daemonId/deviceName. After the daemon is
  // stopped, `status.daemonId` goes back to undefined — without this
  // sticky cache the local row would either disappear or get reclassified
  // as a remote machine (since `isCurrent` requires a daemonId match),
  // taking the Start button with it.
  const [lastIdentity, setLastIdentity] = useState<{
    daemonId: string | null;
    deviceName: string | null;
  }>({ daemonId: null, deviceName: null });

  useEffect(() => {
    const apply = (s: DaemonStatus) => {
      setStatus(s);
      if (s.daemonId) {
        setLastIdentity({
          daemonId: s.daemonId,
          deviceName: s.deviceName ?? null,
        });
      }
    };
    window.daemonAPI.getStatus().then(apply);
    return window.daemonAPI.onStatusChange(apply);
  }, []);

  const bootstrapping =
    status.state === "installing_cli" ||
    status.state === "starting" ||
    status.state === "running";

  return (
    <RuntimesPage
      localDaemonId={status.daemonId ?? lastIdentity.daemonId}
      localMachineName={status.deviceName ?? lastIdentity.deviceName}
      localMachineActions={<DaemonRuntimeActions />}
      // Desktop owns a local machine for the lifetime of the app, even
      // while the daemon is stopped or hasn't registered yet. The shared
      // page synthesizes a placeholder local row when no real runtime
      // matches, so the Start button is always reachable.
      hasLocalMachine
      bootstrapping={bootstrapping}
    />
  );
}
