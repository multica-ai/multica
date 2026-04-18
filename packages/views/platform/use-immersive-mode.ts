import { useEffect } from "react";

type ImmersiveCapableAPI = {
  setImmersiveMode?: (immersive: boolean) => Promise<void> | void;
};

function getDesktopAPI(): ImmersiveCapableAPI | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { desktopAPI?: ImmersiveCapableAPI }).desktopAPI;
}

/**
 * Enter "immersive" mode for the lifetime of the component that calls it.
 *
 * On macOS desktop this hides the traffic-light window controls so full-screen
 * modals (e.g. create-workspace) can place UI in the top-left corner without
 * fighting the native controls' hit-test. On web or non-macOS desktop this
 * is a no-op.
 */
export function useImmersiveMode(): void {
  useEffect(() => {
    const api = getDesktopAPI();
    api?.setImmersiveMode?.(true);
    return () => {
      api?.setImmersiveMode?.(false);
    };
  }, []);
}
