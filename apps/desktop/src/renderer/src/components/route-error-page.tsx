import { useMemo } from "react";
import { isRouteErrorResponse, useLocation, useRouteError } from "react-router-dom";
import { AlertTriangle, Compass, RotateCw, Send, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useModalStore } from "@multica/core/modals";
import { useTabStore } from "@/stores/tab-store";

type DesktopAppInfo = {
  version?: string;
  os?: string;
};

export function formatRouteErrorReport({
  error,
  url,
  appInfo,
  trigger,
}: {
  error: unknown;
  url: string;
  appInfo?: DesktopAppInfo;
  trigger: string;
}) {
  const normalized = normalizeError(error);
  return [
    "kind: desktop_route_error",
    `trigger: ${trigger}`,
    `url: ${url}`,
    `app_version: ${appInfo?.version ?? "unknown"}`,
    `runtime_os: ${appInfo?.os ?? "unknown"}`,
    "",
    "context:",
    `- name: ${normalized.name}`,
    `- message: ${normalized.message}`,
    "",
    "stack:",
    "```",
    normalized.stack ?? "<no stack>",
    "```",
    "",
    "TODO: promote error context to structured feedback fields once the feedback API supports them.",
  ].join("\n");
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
        <h2 className="text-lg font-semibold">This page doesn&apos;t exist</h2>
        <p className="max-w-lg text-sm text-muted-foreground">
          Nothing in Multica matches this address. If you got here from a link,
          it probably points at a file on someone else&apos;s computer rather
          than a page.
        </p>
        <p className="max-w-lg truncate font-mono text-xs text-muted-foreground">
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
            Go to issues
          </Button>
        ) : null}
        <Button
          type="button"
          onClick={() => useTabStore.getState().closeActiveTab()}
        >
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          Close tab
        </Button>
      </div>
    </div>
  );
}

function DesktopUnexpectedErrorPage({ error }: { error: unknown }) {
  const location = useLocation();
  const recoveryRoute = useRecoveryRoute();
  const report = useMemo(
    () =>
      formatRouteErrorReport({
        error,
        url:
          typeof window !== "undefined"
            ? `${window.location.origin}${location.pathname}${location.search}${location.hash}`
            : location.pathname,
        appInfo: typeof window !== "undefined" ? window.desktopAPI?.appInfo : undefined,
        trigger: "route-errorElement",
      }),
    [error, location.hash, location.pathname, location.search],
  );
  const message = normalizeError(error).message;

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-destructive/10 p-3 text-destructive">
        <AlertTriangle className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">Something went wrong in this tab</h2>
        <p className="max-w-lg text-sm text-muted-foreground">
          A route-level renderer error was contained before it could take down the
          desktop shell. Reload this tab, or send the report if it keeps happening.
        </p>
        <p className="max-w-lg truncate text-xs text-muted-foreground">{message}</p>
      </div>
      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().reloadActiveTab()}
        >
          <RotateCw className="mr-2 h-4 w-4" aria-hidden="true" />
          Reload tab
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
            Go to issues
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().closeActiveTab()}
        >
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          Close tab
        </Button>
        <Button
          type="button"
          onClick={() =>
            useModalStore.getState().open("feedback", {
              initialMessage: report,
              kind: "bug",
            })
          }
        >
          <Send className="mr-2 h-4 w-4" aria-hidden="true" />
          Report error
        </Button>
      </div>
    </div>
  );
}

function normalizeError(error: unknown): { name: string; message: string; stack?: string } {
  if (error instanceof Error) {
    return {
      name: error.name || "Error",
      message: error.message || "Unknown route error",
      stack: error.stack,
    };
  }
  if (typeof error === "string") {
    return { name: "Error", message: error };
  }
  return { name: "Error", message: "Unknown route error", stack: safeJson(error) };
}

function safeJson(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
