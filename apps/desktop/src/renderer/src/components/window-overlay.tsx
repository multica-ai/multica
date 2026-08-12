import { useQuery } from "@tanstack/react-query";
import { InvitePage } from "@multica/views/invite";
import { InvitationsPage } from "@multica/views/invitations";
import { OnboardingFlow } from "@multica/views/onboarding";
import { useNavigation } from "@multica/views/navigation";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";
import { useLocalRuntimesPending } from "../platform/use-local-runtimes-pending";
import { useDesktopRuntimeContext } from "./use-desktop-runtime-context";

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
  // Live local-daemon signal for the onboarding runtime step so it doesn't
  // flash "no runtime found" while the daemon is still probing CLI versions.
  const runtimesPending = useLocalRuntimesPending();
  // Live install state for the Multica-managed runtime, so the runtime step
  // can show progress and the reason behind a failure.
  const { managedRuntimeSetup } = useDesktopRuntimeContext();

  if (!overlay) return null;

  // Back is only meaningful when there's somewhere to go — i.e. the user
  // has at least one workspace. Zero-workspace users can only Log out or
  // complete the flow.
  const onBack = wsList.length > 0 ? close : undefined;

  // The daemon's PATH probe runs once at boot, so a newly-installed CLI
  // (Claude / Codex / Cursor) does not show up until the daemon is bounced.
  // Both onboarding entries need this — creating a second workspace hits the
  // same runtime step.
  const restartDaemon = async () => {
    await window.daemonAPI?.restart?.();
  };

  // Only ever called from the runtime step's explicit "install" action —
  // Desktop no longer downloads a runtime on startup.
  const installBuiltInRuntime = () =>
    window.daemonAPI.installRuntime("pi");

  return (
    <div className="fixed inset-0 z-50 flex flex-col overflow-auto bg-background">
      {/* Creating a workspace is the onboarding flow entered at the
          workspace step: a second workspace still needs its own runtime and
          its own Mika, so running one flow keeps the two from drifting. */}
      {overlay.type === "new-workspace" && (
        <OnboardingFlow
          mode="new_workspace"
          onCancel={onBack}
          onRuntimeRefresh={restartDaemon}
          runtimesPending={runtimesPending}
          onInstallBuiltInRuntime={installBuiltInRuntime}
          managedRuntimeSetup={managedRuntimeSetup}
          onComplete={(ws, destination) => {
            close();
            if (ws && destination?.kind === "chat") {
              push(paths.workspace(ws.slug).chatSession(destination.sessionId));
            } else if (ws && destination?.kind === "issue") {
              push(paths.workspace(ws.slug).issueDetail(destination.issueId));
            } else if (ws) {
              push(paths.workspace(ws.slug).issues());
            }
          }}
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
          onComplete={(ws, destination) => {
            close();
            if (ws && destination?.kind === "chat") {
              push(
                paths.workspace(ws.slug).chatSession(destination.sessionId),
              );
            } else if (ws && destination?.kind === "issue") {
              push(
                paths.workspace(ws.slug).issueDetail(destination.issueId),
              );
            } else if (ws) {
              push(paths.workspace(ws.slug).issues());
            } else {
              push(paths.root());
            }
          }}
          // Restart the bundled daemon when the user hits Refresh on
          onRuntimeRefresh={restartDaemon}
          runtimesPending={runtimesPending}
          onInstallBuiltInRuntime={installBuiltInRuntime}
          managedRuntimeSetup={managedRuntimeSetup}
        />
      )}
    </div>
  );
}
