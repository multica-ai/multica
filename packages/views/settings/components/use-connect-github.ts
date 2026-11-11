"use client";

import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { GitHubConnectionEventSchema } from "@multica/core/github";
import { useWSEvent } from "@multica/core/realtime";
import { isDesktopShell, openExternal } from "../../platform";
import { useT } from "../../i18n";

export function useConnectGitHub(wsId: string) {
  const { t } = useT("settings");
  const [connecting, setConnecting] = useState(false);
  const [connectedInstallationID, setConnectedInstallationID] = useState("");
  const pending = useRef<{ workspaceID: string; connectionID?: string } | null>(null);

  useWSEvent("github_installation:created", useCallback((payload) => {
    const event = GitHubConnectionEventSchema.safeParse(payload);
    if (!pending.current || !event.success) return;
    const { installation, connection_id: connectionID } = event.data;
    if (
      pending.current.workspaceID !== wsId ||
      installation.workspace_id !== wsId ||
      (pending.current.connectionID && pending.current.connectionID !== connectionID)
    ) return;
    pending.current = null;
    setConnectedInstallationID(installation.id);
  }, [wsId]));

  const connect = async () => {
    setConnecting(true);
    try {
      const response = await api.getGitHubConnectURL(wsId, { desktop: isDesktopShell() });
      if (!response.configured || !response.url) {
        toast.error(t(($) => $.github.toast_not_configured));
        return;
      }
      if (isDesktopShell()) {
        pending.current = { workspaceID: wsId, connectionID: response.connection_id };
      }
      openExternal(response.url, { webTarget: "same-tab" });
    } catch (error) {
      pending.current = null;
      toast.error(error instanceof Error ? error.message : t(($) => $.github.toast_open_failed));
    } finally {
      setConnecting(false);
    }
  };

  const reset = useCallback(() => setConnectedInstallationID(""), []);
  return { connecting, connect, connectedInstallationID, reset };
}
