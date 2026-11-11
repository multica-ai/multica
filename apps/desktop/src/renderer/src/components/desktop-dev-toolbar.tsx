import { DevToolbar, type ScreenshotCapture } from "@multica/views/dev-toolbar";
import { routeIssueDeepLinkPath } from "@/platform/navigation";
import { useActiveTabUrl } from "@/stores/tab-store";

const BUILD =
  import.meta.env.MODE === "development" ? "desktop" : `desktop · ${import.meta.env.MODE}`;

const SCREENSHOT: ScreenshotCapture = {
  capture: async () =>
    new Blob([await window.desktopAPI.captureWindowForDev()], { type: "image/png" }),
  automatic: true,
};

export function DesktopDevToolbar({ apiUrl }: { apiUrl: string }) {
  const route = useActiveTabUrl() ?? "/";
  return (
    <DevToolbar
      build={BUILD}
      apiUrl={apiUrl}
      route={route}
      openPath={routeIssueDeepLinkPath}
      screenshot={SCREENSHOT}
    />
  );
}
