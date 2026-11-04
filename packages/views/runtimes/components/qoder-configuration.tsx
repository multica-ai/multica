"use client";

import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { qoderOptions } from "@multica/core/runtimes/qoder";
import { QoderConnectForm } from "./qoder-connect-form";
import { useT } from "../../i18n";

/**
 * Connects the workspace to Qoder. An existing connection is managed section
 * by section on the Qoder runtime page.
 */
export function QoderConfiguration({
  onConnected,
}: {
  /** Called after a first-time connection succeeds. */
  onConnected?: () => void;
} = {}) {
  const wsId = useWorkspaceId();
  const query = useQuery(qoderOptions(wsId));
  const { t } = useT("runtimes");
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const member = members.find((m) => m.user_id === user?.id);
  const canManage = member?.role === "owner" || member?.role === "admin";
  if (query.isPending) return <p role="status">{t(($) => $.qoder.loading)}</p>;
  if (query.isError) return <p role="alert">{query.error.message}</p>;
  if (query.data?.available !== true)
    return <p>{t(($) => $.qoder.unavailable)}</p>;
  if (query.data.configured)
    return <p role="status">{t(($) => $.qoder.connect.connected)}</p>;
  if (!canManage)
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.qoder.page.not_connected_hint)}
      </p>
    );
  return (
    <QoderConnectForm
      wsId={wsId}
      connection={query.data}
      submitLabel={t(($) => $.qoder.connect.submit)}
      onConnected={() => {
        toast.success(t(($) => $.qoder.connect.connected));
        onConnected?.();
      }}
    />
  );
}
