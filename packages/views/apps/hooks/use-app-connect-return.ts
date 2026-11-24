"use client";

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { FEATURE_LOCKED_CODE, workspaceFeatureKeys } from "@multica/core/billing";
import { connectedAppsKeys } from "@multica/core/connected-apps";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { isDesktopShell } from "../../platform";

const CONNECT_FAILED = "composio_connect_failed";

/**
 * The hosted connect flow comes back to the page that started it with
 * `?app_connected=<slug>` or `?app_connect_error=<slug>`; flows started
 * without a return path land with `?connected=<slug>` or
 * `?error=composio_connect_failed`. Report the result once, then drop the
 * params so a refresh does not repeat it. A team account the plan refused
 * also carries `app_connect_error_reason` / `reason=feature_locked`; it shows
 * the plan message and refetches the plan instead of asking for a retry.
 * `tab` rewrites the settings tab of
 * a retired link that was redirected here.
 *
 * Desktop finishes the flow in the system browser, so that return never
 * reaches the window; reload the accounts when it is focused again instead.
 */
export function useAppConnectReturn({ tab }: { tab?: string } = {}) {
  const { t } = useT("settings");
  const { t: tBilling } = useT("billing");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const navigation = useNavigation();
  const params = navigation.searchParams;
  const connected = params.get("app_connected") ?? params.get("connected");
  const failed = params.get("app_connect_error") ?? (params.get("error") === CONNECT_FAILED ? CONNECT_FAILED : null);
  const locked = (params.get("app_connect_error_reason") ?? params.get("reason")) === FEATURE_LOCKED_CODE;
  const consumed = useRef<string | null>(null);

  useEffect(() => {
    const key = connected ? `connected:${connected}` : failed ? `failed:${failed}` : null;
    if (!key || consumed.current === key) return;
    consumed.current = key;
    if (connected) {
      toast.success(t(($) => $.connected_apps.toast_connected));
      void qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) });
    } else if (locked) {
      toast.error(tBilling(($) => $.paid_feature.locked_error));
      void qc.invalidateQueries({ queryKey: workspaceFeatureKeys.all(wsId) });
    } else {
      toast.error(t(($) => $.connected_apps.toast_connect_failed));
    }
    const next = new URLSearchParams(params);
    for (const k of ["app_connected", "app_connect_error", "app_connect_error_reason", "connected", "scope", "integration"]) next.delete(k);
    if (next.get("error") === CONNECT_FAILED) {
      next.delete("error");
      next.delete("reason");
    }
    if (tab) next.set("tab", tab);
    const search = next.toString();
    navigation.replace(`${navigation.pathname}${search ? `?${search}` : ""}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, failed]);

  useEffect(() => {
    if (!isDesktopShell()) return;
    const refresh = () => {
      void qc.invalidateQueries({ queryKey: connectedAppsKeys.overview(wsId) });
      void qc.invalidateQueries({ queryKey: connectedAppsKeys.myPendingRequests(wsId) });
    };
    window.addEventListener("focus", refresh);
    return () => window.removeEventListener("focus", refresh);
  }, [qc, wsId]);
}
