import { useEffect, useState } from "react";
import type { DaemonStatus } from "../../../shared/daemon-types";

/**
 * True while the local daemon is still expected to produce agent runtimes: it
 * is booting (`starting` / `installing_cli`) or it is running and reports agent
 * CLIs detected on this host that may not have finished registering yet.
 *
 * The onboarding runtime step reads this so it keeps showing the scanning
 * skeleton instead of flashing "no runtime found" while the daemon is still
 * probing CLI versions — a false negative on a machine that does have coding
 * tools installed (MUL-5119). The daemon's `/health` `agents` list comes from
 * its boot-time PATH scan, so it is populated well before the slower
 * version-registration finishes, which is exactly what makes it a reliable
 * "runtimes are coming" hint.
 *
 * It goes false once the daemon settles into a state that will not yield
 * runtimes (running with zero detected agents, stopped, cli_not_found,
 * auth_expired), letting the step fall through to its genuine empty exits.
 *
 * Starts true so a daemon that is mid-boot when the user lands on the step
 * doesn't get a scanning→empty flash before the first status resolves.
 */
export function useLocalRuntimesPending(): boolean {
  const [pending, setPending] = useState(true);
  useEffect(() => {
    const apply = (s: DaemonStatus) => {
      setPending(
        s.state === "starting" ||
          s.state === "installing_cli" ||
          (s.state === "running" && (s.agents?.length ?? 0) > 0),
      );
    };
    let cancelled = false;
    window.daemonAPI
      .getStatus()
      .then((s) => {
        if (!cancelled) apply(s);
      })
      .catch(() => {
        // No daemon status available — leave `pending` at its initial true so
        // the step relies on the runtime step's absolute hard-timeout ceiling
        // rather than flashing empty prematurely.
      });
    const unsubscribe = window.daemonAPI.onStatusChange(apply);
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }, []);
  return pending;
}
