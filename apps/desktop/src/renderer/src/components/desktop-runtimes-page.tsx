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

  useEffect(() => {
    window.daemonAPI.getStatus().then(setStatus);
    return window.daemonAPI.onStatusChange(setStatus);
  }, []);

  const bootstrapping =
    status.state === "installing_cli" ||
    status.state === "starting" ||
    status.state === "running";

  return (
    <RuntimesPage
      localDaemonId={status.daemonId ?? null}
      localMachineName={status.deviceName ?? null}
      localMachineActions={<DaemonRuntimeActions />}
      bootstrapping={bootstrapping}
    />
  );
}
