// FIR-2580: keep the desktop dock / window icon in sync with the active
// workspace's logo. The bundled app icon is build-time and cannot vary per
// workspace, so this renderer component fetches the active logo, converts it
// to a data URL, and hands it to the main process over the workspace-icon:set
// IPC channel (main applies it via app.dock.setIcon / win.setIcon). When the
// flag is off or no logo is set it sends null, which restores the default
// bundled icon. Mounted inside WorkspaceRouteLayout so useCurrentWorkspace
// resolves the active workspace.

import { useEffect } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

async function toDataUrl(url: string): Promise<string | null> {
  try {
    const res = await fetch(url, { credentials: "include" });
    if (!res.ok) return null;
    const blob = await res.blob();
    return await new Promise<string | null>((resolve) => {
      const reader = new FileReader();
      reader.onloadend = () =>
        resolve(typeof reader.result === "string" ? reader.result : null);
      reader.onerror = () => resolve(null);
      reader.readAsDataURL(blob);
    });
  } catch {
    return null;
  }
}

export function CerebroWorkspaceDockIcon() {
  const enabled = useFeatureFlag("cerebro_workspace_logo");
  const workspace = useCurrentWorkspace();
  const logoUrl = enabled ? resolvePublicFileUrl(workspace?.avatar_url) : null;

  useEffect(() => {
    const setIcon = window.desktopAPI?.setWorkspaceIcon;
    if (!setIcon) return;
    if (!logoUrl) {
      setIcon(null);
      return;
    }
    let cancelled = false;
    void toDataUrl(logoUrl).then((dataUrl) => {
      if (!cancelled) setIcon(dataUrl);
    });
    return () => {
      cancelled = true;
    };
  }, [logoUrl]);

  return null;
}
