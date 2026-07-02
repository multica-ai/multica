import { useEffect } from "react";
import { hasOptedOutCapturing } from "@multica/core/analytics";
import { useConfigStore } from "@multica/core/config";

/**
 * Pushes the CPU-profiling gate to the Electron main process (MUL-3738).
 *
 * The main process samples a hung renderer only when BOTH the backend flag is
 * on AND the user hasn't opted out of analytics — but at hang time it can no
 * longer ask the frozen renderer either fact. So the renderer pre-pushes the
 * combined gate here; main holds the last value and decides synchronously when
 * a hang fires. Re-pushes whenever the flag changes (it arrives async from
 * `/api/config`); the opt-out value is re-read on each push.
 *
 * No-op on web — `window.desktopAPI` is undefined there.
 */
export function DiagnosticsControlBridge() {
  const cpuProfileEnabled = useConfigStore((s) => s.cpuProfileEnabled);

  useEffect(() => {
    window.desktopAPI?.setDiagnosticsControl?.({
      cpuProfileEnabled,
      optOut: hasOptedOutCapturing(),
    });
  }, [cpuProfileEnabled]);

  return null;
}
