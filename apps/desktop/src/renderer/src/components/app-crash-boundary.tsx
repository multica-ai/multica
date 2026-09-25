import type { ReactNode } from "react";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { Button } from "@multica/ui/components/ui/button";
import { captureException } from "@multica/core/analytics";
import { DragStrip } from "@multica/views/platform";

/**
 * Last-resort boundary around the entire desktop renderer.
 *
 * The renderer had no boundary above the shell at all: `createRoot().render()`
 * mounted <App /> bare, and the router's `errorElement` only covers what the
 * router itself renders — the sidebar, search dialog, modal registry and
 * window overlay are siblings of the router, outside its reach. A render-time
 * throw in any of them unmounted the whole React tree and left an empty,
 * unresponsive window with no way back except force-quitting the app. That is
 * exactly what #7021 reported after deleting the last workspace (MUL-6231).
 *
 * The specific throw behind that report is fixed at its source, but "one
 * component throws" must not stay a whole-app kill switch on desktop, where
 * there is no reload button and no URL bar to escape with. This turns the
 * blank window into a readable error plus a Reload button.
 *
 * Deliberately NOT a `reset()` fallback: whatever state produced the throw is
 * still in the stores, so re-rendering the same tree usually re-throws.
 * A full `location.reload()` re-runs bootstrap from persisted state, which is
 * the recovery path the user would otherwise get by restarting the app.
 */
export function AppCrashBoundary({ children }: { children: ReactNode }) {
  return (
    <ErrorBoundary
      // captureException, not captureEvent: only `$exception` events pass
      // through initAnalytics' before_send hook, which drops known-benign
      // noise, runs redactExceptionProperties over the message and stack, and
      // fuses repeats via shouldDropException. A plain event would ship a raw
      // message and stack — which can carry emails, tokenised URLs or typed
      // user input — straight to storage. Same entry point the web
      // global-error route uses.
      onError={(error) => {
        captureException(error, { source: "desktop-renderer-boundary" });
      }}
      fallback={({ error }) => <CrashFallback error={error} />}
    >
      {children}
    </ErrorBoundary>
  );
}

/**
 * Full-window view outside the dashboard shell, so it owes the same window
 * chrome every other one does: `<DragStrip />` as the first flex child, or the
 * user loses the draggable top edge exactly when the app is least usable. The
 * Reload button sits inside the centred `flex-1` region, well clear of the top
 * 48px, so it needs no `WebkitAppRegion: "no-drag"` opt-out.
 *
 * Everything rendered here has to be safe under a broken tree: an error thrown
 * while rendering a fallback is NOT caught by its own boundary and would blank
 * the window all over again. DragStrip and Button are static markup with no
 * hooks, stores or workspace context.
 */
function CrashFallback({ error }: { error: Error }) {
  return (
    <div className="flex h-screen flex-col bg-background text-foreground">
      <DragStrip />
      <div
        role="alert"
        className="flex flex-1 items-center justify-center overflow-auto p-8"
      >
        <div className="max-w-xl rounded-lg border bg-card p-6 shadow-sm">
          <h1 className="text-title font-semibold">Something went wrong</h1>
          <p className="mt-3 text-body text-muted-foreground">
            Multica Desktop hit an unexpected error and could not keep
            rendering. Reloading usually recovers — your work is stored on the
            server.
          </p>
          <pre className="mt-4 max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-3 text-caption text-muted-foreground">
            {error.message || "An unexpected error occurred."}
          </pre>
          <Button className="mt-4" onClick={() => window.location.reload()}>
            Reload
          </Button>
        </div>
      </div>
    </div>
  );
}
