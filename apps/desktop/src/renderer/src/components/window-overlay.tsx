import { useQuery } from "@tanstack/react-query";
import { useImmersiveMode } from "@multica/views/platform";
import { NewWorkspacePage } from "@multica/views/workspace/new-workspace-page";
import { InvitePage } from "@multica/views/invite";
import { useNavigation } from "@multica/views/navigation";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

/**
 * Window-level transition overlay: renders above the tab system when the
 * user is in a pre-workspace flow (create workspace, accept invite).
 *
 * This component is a thin **platform shell**:
 *  - Hands the window-drag strip and macOS traffic-light hiding
 *    (`useImmersiveMode`) — both are platform-specific, web has neither
 *  - Covers the tab system (fixed inset, z-50) so the Shell's own TabBar
 *    doesn't leak through
 *
 * All UX affordances (Back button, Log out button, welcome copy, invite
 * card) live inside the shared `NewWorkspacePage` / `InvitePage`
 * components under `packages/views/`, so web and desktop render identical
 * content. The platform split is: UX in shared code, chrome here.
 */
export function WindowOverlay() {
  const overlay = useWindowOverlayStore((s) => s.overlay);
  if (!overlay) return null;
  return <WindowOverlayInner />;
}

function WindowOverlayInner() {
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const close = useWindowOverlayStore((s) => s.close);
  const { push } = useNavigation();
  const { data: wsList = [] } = useQuery(workspaceListOptions());

  useImmersiveMode();

  if (!overlay) return null;

  // Back is only meaningful when there's somewhere to go — i.e. the user
  // has at least one workspace. Zero-workspace users can only Log out or
  // complete the flow.
  const onBack = wsList.length > 0 ? close : undefined;

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-background">
      {/* Window-drag strip. Rendered as a flex *child* (not absolute
          overlay) so it owns its own 48px of real layout space — the
          prior absolute-positioned approach relied on z-index stacking
          to beat the content wrapper's no-drag, which in practice didn't
          hit-test reliably for `-webkit-app-region` on the welcome
          screen. A real flex row with nothing else in it has no such
          ambiguity: any pixel at top-48 is drag, full stop.

          Height matches `MainTopBar` (48px) so the drag-to-grab area
          feels consistent with the rest of the app. The strip is
          invisible; macOS traffic lights would normally sit here but
          `useImmersiveMode` has hidden them for the overlay's lifetime. */}
      <div
        aria-hidden
        className="h-12 shrink-0"
        style={{ WebkitAppRegion: "drag" } as React.CSSProperties}
      />

      <div
        className="flex-1 min-h-0 overflow-auto"
        style={{ WebkitAppRegion: "no-drag" } as React.CSSProperties}
      >
        {overlay.type === "new-workspace" && (
          <NewWorkspacePage
            onSuccess={(ws) => push(paths.workspace(ws.slug).issues())}
            onBack={onBack}
          />
        )}
        {overlay.type === "invite" && (
          <InvitePage
            invitationId={overlay.invitationId}
            onBack={onBack}
          />
        )}
      </div>
    </div>
  );
}
