import { DevToolbar } from "@multica/views/dev-toolbar";
import { useActiveTabUrl } from "@/stores/tab-store";
import { routeContentLinkPath } from "@/platform/navigation";

const BUILD =
  import.meta.env.MODE === "development" ? "desktop" : `desktop · ${import.meta.env.MODE}`;

export function DesktopDevToolbar({ apiUrl }: { apiUrl: string }) {
  const route = useActiveTabUrl() ?? "/";
  return <DevToolbar build={BUILD} apiUrl={apiUrl} route={route} onNavigate={routeContentLinkPath} />;
}
