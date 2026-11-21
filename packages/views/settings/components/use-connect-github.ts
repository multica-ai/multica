"use client";

import { useCallback, useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useGitHubConnectionStore } from "@multica/core/github";
import { isDesktopShell, openExternal } from "../../platform";
import { useT } from "../../i18n";

export function useConnectGitHub(wsId: string) {
  const { t } = useT("settings");
  const [connecting, setConnecting] = useState(false);
  const connectedInstallationID = useGitHubConnectionStore((state) =>
    state.connection?.workspaceID === wsId ? state.connection.installationID : "",
  );

  const connect = async () => {
    setConnecting(true);
    try {
      const response = await api.getGitHubConnectURL(wsId, { desktop: isDesktopShell() });
      if (!response.configured || !response.url) {
        toast.error(t(($) => $.github.toast_not_configured));
        return;
      }
      if (isDesktopShell()) {
        useGitHubConnectionStore.getState().begin(wsId, response.connection_id);
      }
      openExternal(response.url, { webTarget: "same-tab" });
    } catch (error) {
      useGitHubConnectionStore.getState().reset(wsId);
      toast.error(error instanceof Error ? error.message : t(($) => $.github.toast_open_failed));
    } finally {
      setConnecting(false);
    }
  };

  const reset = useCallback(() => useGitHubConnectionStore.getState().reset(wsId), [wsId]);
  return { connecting, connect, connectedInstallationID, reset };
}
