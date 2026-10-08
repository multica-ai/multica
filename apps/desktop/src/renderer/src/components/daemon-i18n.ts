import type { useT } from "@multica/views/i18n";
import type { DaemonState } from "../../../shared/daemon-types";

export type DaemonTranslator = ReturnType<typeof useT<"settings">>["t"];

export function daemonStateLabel(
  state: DaemonState,
  t: DaemonTranslator,
): string {
  switch (state) {
    case "stopped":
      return t(($) => $.desktop.daemon.state_stopped);
    case "starting":
      return t(($) => $.desktop.daemon.state_starting);
    case "running":
      return t(($) => $.desktop.daemon.state_running);
    case "stopping":
      return t(($) => $.desktop.daemon.state_stopping);
    case "cli_not_found":
      return t(($) => $.desktop.daemon.state_cli_not_found);
    case "installing_cli":
      return t(($) => $.desktop.daemon.state_installing_cli);
    case "recovery_paused":
      return t(($) => $.desktop.daemon.state_recovery_paused);
    case "auth_expired":
      return t(($) => $.desktop.daemon.state_auth_expired);
  }
}
