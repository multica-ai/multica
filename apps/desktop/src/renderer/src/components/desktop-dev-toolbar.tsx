import { DevToolbar } from "@multica/views/dev-toolbar";
import { useActiveTabUrl } from "@/stores/tab-store";

const BUILD =
  import.meta.env.MODE === "development" ? "desktop" : `desktop · ${import.meta.env.MODE}`;

export function DesktopDevToolbar({ apiUrl }: { apiUrl: string }) {
  const route = useActiveTabUrl() ?? "/";
  return <DevToolbar build={BUILD} apiUrl={apiUrl} route={route} />;
}
