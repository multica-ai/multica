import { RefreshCw } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { paths } from "@multica/core/paths";
import { useAppUpdate } from "@/hooks/use-app-update";
import { useTabStore } from "@/stores/tab-store";

/**
 * Top-bar update affordance.
 *
 * Renders nothing while the app is up to date — the auto-update check runs in
 * the background (see `src/main/updater.ts`) and this button only appears once
 * that check reports a newer version. Clicking it installs the update when it
 * has finished downloading, or opens Settings → Updates (where the download
 * progress lives) while it is still being fetched.
 */
export function UpdateButton() {
  const update = useAppUpdate();

  // Hidden when up to date. This is the whole point: no update, no button.
  if (!update) return null;

  const handleClick = () => {
    if (update.downloaded) {
      // Package is staged — quit and apply it now.
      void window.updater.installUpdate();
      return;
    }
    // Still downloading in the background: send the user to Settings → Updates,
    // which shows live progress and the install affordance.
    const slug = useTabStore.getState().activeWorkspaceSlug;
    if (!slug) return;
    window.dispatchEvent(
      new CustomEvent("multica:navigate", {
        detail: { path: paths.workspace(slug).settings() },
      }),
    );
  };

  const label = update.downloaded
    ? `Restart to update to v${update.version}`
    : `Update available: v${update.version}`;

  return (
    <button
      type="button"
      onClick={handleClick}
      aria-label={label}
      title={label}
      // Tinted with the brand color so an available update reads as a live
      // affordance rather than another muted nav control.
      className={cn(
        "flex size-7 items-center justify-center rounded-md text-primary transition-colors",
        "hover:bg-sidebar-accent hover:text-primary",
      )}
      style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
    >
      <RefreshCw className="size-4" />
    </button>
  );
}
