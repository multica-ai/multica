import { useMemo } from "react";
import { isRouteErrorResponse, useLocation, useRouteError } from "react-router-dom";
import { AlertTriangle, Compass, RotateCw, Send, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { DesktopRouteErrorFeedbackContext } from "@multica/core/feedback";
import { useModalStore } from "@multica/core/modals";
import { useTabStore } from "@/stores/tab-store";
import { useT } from "@multica/views/i18n";

export function createRouteErrorFeedbackContext({
  error,
  trigger,
}: {
  error: unknown;
  trigger: string;
}): DesktopRouteErrorFeedbackContext {
  const normalized = normalizeError(error);
  return {
    kind: "desktop_route_error",
    trigger,
    error: normalized,
  };
}

/**
 * Resolve the workspace the user is actually in, for a recovery entry point.
 *
 * Reads the tab store's `activeWorkspaceSlug` — the real session context —
 * rather than the failed URL. This is load-bearing, not stylistic: the routes
 * that land here most often are the ones whose first segment is not a workspace
 * at all. Deriving a slug from `/Users/me/shot.png` yields "Users" and a
 * "recovery" button pointing at `/Users/issues`, i.e. a second 404 (MUL-4899).
 * A pathname we already failed to route cannot be a source of truth about where
 * the user belongs.
 *
 * Returns null when there is no active workspace, in which case the caller
 * offers only unconditionally-safe actions (close the tab).
 */
function useRecoveryRoute(): string | null {
  const activeWorkspaceSlug = useTabStore((state) => state.activeWorkspaceSlug);
  return activeWorkspaceSlug ? `/${activeWorkspaceSlug}/issues` : null;
}

export function DesktopRouteErrorPage() {
  const error = useRouteError();

  // A 404 is not a crash — it is a route that does not exist, which is a normal
  // and fully recoverable product state. It reaches this boundary because React
  // Router routes "no route matched" to the nearest errorElement, the same place
  // a thrown render error lands. Splitting them here is the whole point of
  // MUL-4899: 8 of 18 desktop_route_error reports were users clicking an agent's
  // `/Users/...` link and being told the app broke and to report a bug.
  if (isRouteErrorResponse(error) && error.status === 404) {
    return <DesktopNotFoundPage />;
  }
  return <DesktopUnexpectedErrorPage error={error} />;
}

function DesktopNotFoundPage() {
  const { t } = useT("layout");
  const location = useLocation();
  const recoveryRoute = useRecoveryRoute();

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-muted p-3 text-muted-foreground">
        <Compass className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-title font-semibold">
          {t(($) => $.desktop.not_found_title)}
        </h2>
        <p className="max-w-lg text-body text-muted-foreground">
          {t(($) => $.desktop.not_found_description)}
        </p>
        <p className="max-w-lg truncate font-mono text-caption text-muted-foreground">
          {location.pathname}
        </p>
      </div>
      <div className="flex gap-2">
        {recoveryRoute ? (
          <Button
            type="button"
            variant="outline"
            // Session mutation, not a router call: the Coordinator projects
            // the new session URL into the router (MUL-4741 invariant 1).
            onClick={() =>
              useTabStore
                .getState()
                .navigateActiveSession(recoveryRoute, { replace: true })
            }
          >
            {t(($) => $.desktop.go_to_issues)}
          </Button>
        ) : null}
        <Button
          type="button"
          onClick={() => useTabStore.getState().closeActiveTab()}
        >
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.close_tab)}
        </Button>
      </div>
    </div>
  );
}

function DesktopUnexpectedErrorPage({ error }: { error: unknown }) {
  const { t } = useT("layout");
  const recoveryRoute = useRecoveryRoute();
  const feedbackContext = useMemo(
    () =>
      createRouteErrorFeedbackContext({
        error,
        trigger: "route-errorElement",
      }),
    [error],
  );
  const message = normalizeError(
    error,
    t(($) => $.desktop.unknown_route_error),
  ).message;

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-destructive/10 p-3 text-destructive">
        <AlertTriangle className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-title font-semibold">
          {t(($) => $.desktop.unexpected_title)}
        </h2>
        <p className="max-w-lg text-body text-muted-foreground">
          {t(($) => $.desktop.unexpected_description)}
        </p>
        <p className="max-w-lg truncate text-caption text-muted-foreground">{message}</p>
      </div>
      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().reloadActiveTab()}
        >
          <RotateCw className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.reload_tab)}
        </Button>
        {recoveryRoute ? (
          <Button
            type="button"
            variant="outline"
            // Session mutation, not a router call: the Coordinator projects
            // the new session URL into the router (MUL-4741 invariant 1).
            onClick={() =>
              useTabStore
                .getState()
                .navigateActiveSession(recoveryRoute, { replace: true })
            }
          >
            {t(($) => $.desktop.go_to_issues)}
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().closeActiveTab()}
        >
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.close_tab)}
        </Button>
        <Button
          type="button"
          onClick={() =>
            useModalStore.getState().open("feedback", {
              kind: "bug",
              context: feedbackContext,
            })
          }
        >
          <Send className="mr-2 h-4 w-4" aria-hidden="true" />
          {t(($) => $.desktop.report_error)}
        </Button>
      </div>
    </div>
  );
}

function normalizeError(
  error: unknown,
  unknownMessage = "Unknown route error",
): { name: string; message: string; stack?: string } {
  if (error instanceof Error) {
    return {
      name: error.name || "Error",
      message: error.message || unknownMessage,
      stack: error.stack,
    };
  }
  if (typeof error === "string") {
    return { name: "Error", message: error };
  }
  return { name: "Error", message: unknownMessage, stack: safeJson(error) };
}

function safeJson(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
