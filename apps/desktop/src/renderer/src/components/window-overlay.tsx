import { LogOut } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { NewWorkspacePage } from "@multica/views/workspace/new-workspace-page";
import { InvitePage } from "@multica/views/invite";
import { InvitationsPage } from "@multica/views/invitations";
import { OnboardingFlow } from "@multica/views/onboarding";
import { useNavigation } from "@multica/views/navigation";
import { useLogout } from "@multica/views/auth";
import { paths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
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
        <>
          {/*
           * OnboardingFlow has no built-in logout/back affordance (unlike
           * NewWorkspacePage), so a desktop user who lands here on the wrong
           * or empty Google account — deep-link OAuth picks the browser's
           * active account with no chooser — is trapped with only "Create
           * workspace" and no URL bar to escape. We overlay a fixed-position
           * "Log out" escape + the signed-in email so the wrong-account case
           * is visible and exitable without creating a junk workspace.
           */}
          <OnboardingAccountEscape />
          <OnboardingFlow
            onComplete={(ws) => {
              close();
              // Starter content is offered after landing in the workspace.
              if (ws) {
                push(paths.workspace(ws.slug).issues());
              } else {
                push(paths.root());
              }
            }}
          />
        </>
      )}
    </div>
  );
}

/**
 * Always-visible "switch account" escape for the desktop onboarding overlay.
 * Sits at the top-right (below the 48px macOS drag strip, so no `no-drag`
 * override is needed) and shows the logged-in email so a wrong-account
 * situation is obvious at a glance. `useLogout` runs the full desktop logout
 * — clears tabs, stops the daemon, navigates to /login — the same flow
 * NewWorkspacePage's Log out button uses.
 */
function OnboardingAccountEscape() {
  const logout = useLogout();
  const email = useAuthStore((s) => s.user?.email);

  return (
    <div className="absolute top-16 right-4 z-50 flex items-center gap-2 sm:right-12">
      {email ? (
        <span className="hidden max-w-[40vw] truncate text-xs text-muted-foreground sm:inline">
          {email}
        </span>
      ) : null}
      <Button
        variant="ghost"
        size="sm"
        className="text-muted-foreground hover:text-destructive"
        onClick={logout}
      >
        <LogOut />
        Log out
      </Button>
    </div>
  );
}
