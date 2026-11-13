import type { DaemonCommands } from "@multica/core/runtimes";
import { useT } from "../i18n";

export function DaemonSetupNotice({
  status,
}: {
  status: Exclude<DaemonCommands["status"], "ready">;
}) {
  const { t } = useT("common");
  return (
    <p
      role={status === "loading" ? "status" : "alert"}
      className="text-caption text-muted-foreground"
    >
      {t(($) => $.daemon_setup[status])}
    </p>
  );
}
