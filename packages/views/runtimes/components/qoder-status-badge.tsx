import type { QoderConnection } from "@multica/core/runtimes/qoder";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../../i18n";

export function QoderStatusBadge({
  connection,
}: {
  connection: QoderConnection;
}) {
  const { t } = useT("runtimes");
  const status = !connection.configured
    ? "not_configured"
    : !connection.enabled
      ? "stopped"
      : connection.status === "online"
        ? "online"
        : connection.status === "error"
          ? "error"
          : connection.status === "offline"
            ? "offline"
            : "starting";
  const tone =
    status === "online"
      ? "bg-success/10 text-success"
      : status === "error"
        ? "bg-destructive/10 text-destructive"
        : status === "starting"
          ? "bg-warning/10 text-warning"
          : "bg-muted text-muted-foreground";
  return (
    <Badge variant="secondary" className={tone}>
      <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />
      {t(($) => $.qoder[status])}
    </Badge>
  );
}
