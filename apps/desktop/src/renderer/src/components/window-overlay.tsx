import { useQuery } from "@tanstack/react-query";
import { NewWorkspacePage } from "@multica/views/workspace/new-workspace-page";
import { InvitePage } from "@multica/views/invite";
import { InvitationsPage } from "@multica/views/invitations";
import { OnboardingFlow } from "@multica/views/onboarding";
import { useNavigation } from "@multica/views/navigation";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

/**
 * Window-level transition overlay: renders above the tab system when the
 * user is in a pre-workspace flow (onboarding, create workspace, accept
 * invite).
 *
 * This component is intentionally thin — just a fixed positioning shell
 * that covers the tab system. It does NOT hide traffic lights or provide
 * a drag strip: each contained view (OnboardingFlow, NewWorkspacePage,
 * InvitePage) renders its own `<DragStrip />` as a flex-child at top so
 * native macOS traffic lights stay visible and the page content can fill
 * the window edge-to-edge. This matches the Linear/Notion/Arc pattern for
 * pre-dashboard flows and keeps platform chrome consistent across every
 * "not-in-dashboard" surface.
 *
 * All UX affordances (Back button, Log out button, welcome copy, invite
 * card) live inside the shared view components under `packages/views/`,
 * so web and desktop render identical content.
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

  if (!overlay) return null;

  // Back is only meaningful when there's somewhere to go — i.e. the user
  // has at least one workspace. Zero-workspace users can only Log out or
  // complete the flow.
  const onBack = wsList.length > 0 ? close : undefined;

  return (
    <div className="fixed inset-0 z-50 flex flex-col overflow-auto bg-background">
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
      {overlay.type === "invitations" && <InvitationsPage />}
      {overlay.type === "onboarding" && (
        <OnboardingFlow
          onComplete={(ws) => {
            close();
            // Post-onboarding landing is always the workspace issues
            // list. The welcome-issue flow moved into a dialog that
            // renders on that page (StarterContentPrompt), so the
            // flow doesn't need to thread a target issue id back here.
            if (ws) {
              push(paths.workspace(ws.slug).issues());
            } else {
              push(paths.root());
            }
          }}
        />
      )}
    </div>
  );
}
