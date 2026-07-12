import { RuntimesPage } from "@multica/views/runtimes";
import { useDesktopRuntimeContext } from "./use-desktop-runtime-context";

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
  const context = useDesktopRuntimeContext();

  return (
    <RuntimesPage
      localDaemonId={context.localDaemonId}
      localMachineName={context.localMachineName}
      // Desktop owns a local machine for the lifetime of the app, even
      // while the daemon is stopped or hasn't registered yet. Lifecycle
      // controls live on the machine detail page so this collection stays
      // consistent with every other machine row.
      hasLocalMachine
      bootstrapping={context.bootstrapping}
    />
  );
}
