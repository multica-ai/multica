import { AnimatePresence, motion, useIsPresent } from "motion/react";
import { useQuery } from "@tanstack/react-query";
import { useReducedMotion } from "@multica/core/appearance";
import { UI_EASE_IN, UI_EASE_OUT } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import { InvitationsPage } from "@multica/views/invitations";
import { OnboardingFlow } from "@multica/views/onboarding";
import { useNavigation } from "@multica/views/navigation";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useWindowOverlayStore, type WindowOverlay as WindowOverlayData } from "@/stores/window-overlay-store";
import { useLocalRuntimesPending } from "../platform/use-local-runtimes-pending";

const OVERLAY_ENTER_DURATION = 0.18;
const OVERLAY_EXIT_DURATION = 0.16;

/**
 * Window-level transition overlay: renders above the tab system when the
 * user is in a pre-workspace flow (onboarding, create workspace, accept
 * invite).
 *
 * This component is intentionally thin — just a fixed positioning shell
 * that covers the tab system. It does NOT hide traffic lights or provide
 * a drag strip: each contained view (OnboardingFlow, NewWorkspacePage,
 * InvitationsPage) renders its own `<DragStrip />` as a flex-child at top so
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
  const reduceMotion = useReducedMotion();

  if (reduceMotion) {
    return overlay ? <WindowOverlayInner overlay={overlay} reduceMotion /> : null;
  }

  return (
    <AnimatePresence>
      {overlay && (
        <WindowOverlayInner
          key={overlay.type}
          overlay={overlay}
          reduceMotion={false}
        />
      )}
    </AnimatePresence>
  );
}

function WindowOverlayInner({
  overlay,
  reduceMotion,
}: {
  overlay: NonNullable<WindowOverlayData>;
  reduceMotion: boolean;
}) {
  const isPresent = useIsPresent();
  const close = useWindowOverlayStore((s) => s.close);
  const { push } = useNavigation();
  const { data: wsList = [] } = useQuery(workspaceListOptions());
  // Live local-daemon signal for the onboarding runtime step so it doesn't
  // flash "no runtime found" while the daemon is still probing CLI versions.
  const runtimesPending = useLocalRuntimesPending();

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

  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0 }}
      animate={{
        opacity: 1,
        transition: { duration: reduceMotion ? 0 : OVERLAY_ENTER_DURATION, ease: UI_EASE_OUT },
      }}
      exit={{
        opacity: 0,
        transition: { duration: reduceMotion ? 0 : OVERLAY_EXIT_DURATION, ease: UI_EASE_IN },
      }}
      className={cn(
        "fixed inset-0 z-50 flex flex-col overflow-auto bg-background",
        !isPresent && "pointer-events-none",
      )}
    >
      {/* Creating a workspace is the onboarding flow entered at the
          workspace step: a second workspace still needs its own runtime and
          its own Mika, so running one flow keeps the two from drifting. */}
      {overlay.type === "new-workspace" && (
        <OnboardingFlow
          mode="new_workspace"
          onCancel={onBack}
          onRuntimeRefresh={restartDaemon}
          runtimesPending={runtimesPending}
          onComplete={(ws, destination) => {
            close();
            if (ws && destination?.kind === "chat") {
              push(paths.workspace(ws.slug).chatSession(destination.sessionId));
            } else if (ws && destination?.kind === "issue") {
              push(paths.workspace(ws.slug).issueDetail(destination.issueRef));
            } else if (ws) {
              push(paths.workspace(ws.slug).issues());
            }
          }}
        />
      )}
      {overlay.type === "invitations" && (
        <InvitationsPage
          invitationId={overlay.invitationId}
          onBack={onBack}
        />
      )}
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
                paths.workspace(ws.slug).issueDetail(destination.issueRef),
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
        />
      )}
    </motion.div>
  );
}
