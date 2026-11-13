import { useEffect, useState } from "react";
import { DevToolbar } from "@multica/views/dev-toolbar";
import { useActiveTabUrl } from "@/stores/tab-store";
import { routeContentLinkPath } from "@/platform/navigation";

const BUILD =
  import.meta.env.MODE === "development" ? "desktop" : `desktop · ${import.meta.env.MODE}`;

export function DesktopDevToolbar({ apiUrl }: { apiUrl: string }) {
  const [visible, setVisible] = useState(() => window.desktopAPI.getDevToolbarVisible());
  useEffect(() => window.desktopAPI.onDevToolbarVisibleChange(setVisible), []);
  const route = useActiveTabUrl() ?? "/";
  if (!visible) return null;
  return <DevToolbar build={BUILD} apiUrl={apiUrl} route={route} onNavigate={routeContentLinkPath} />;
}
