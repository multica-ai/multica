import { useEffect } from "react";
import { useConfigStore, featureFlagEnabled } from "@multica/core/config";
import { HANG_STACK_CAPTURE_FLAG } from "../../../shared/diagnostics-control";

/**
 * Carries the server's diagnostics kill switch to the main process.
 *
 * The main process is the party that holds a debugger channel open and reads
 * stacks out of a hung renderer, but it has no HTTP client and no session — it
 * cannot read `/api/config` itself. So the renderer, which already has the
 * flags, forwards the one bit main needs.
 *
 * Main starts fail-closed and only enables capture on an explicit `true`, so a
 * config that never arrives, a backend that has never heard of the flag, and a
 * renderer that never mounts this all land on "off".
 */
export function DiagnosticsControlReporter() {
  // Subscribed, not read once: the flags arrive asynchronously after auth, so
  // a component that read them at mount would publish the empty pre-config
  // state and never correct it.
  const stackCaptureEnabled = useConfigStore((state) =>
    featureFlagEnabled(state.featureFlags, HANG_STACK_CAPTURE_FLAG),
  );

  useEffect(() => {
    window.desktopAPI.setDiagnosticsControl({ stackCaptureEnabled });
  }, [stackCaptureEnabled]);

  return null;
}
