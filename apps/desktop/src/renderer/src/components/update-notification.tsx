import { useEffect, useRef, useState } from "react";
import { RefreshCw, X } from "lucide-react";
import type { UpdateInstallState } from "../../../shared/updater-types";

// Downloads run silently in the background (main process has
// autoDownload=true). The renderer only renders UI once the package is fully
// downloaded and waiting for a restart.
function changelogUrl(version: string): string {
  return `https://multica.ai/changelog#release-${version.replace(/\./g, "-")}`;
}

export function UpdateNotification() {
  const [state, setState] = useState<UpdateInstallState>({ status: "idle" });
  const [dismissed, setDismissed] = useState(false);
  const [requestPending, setRequestPending] = useState(false);
  const [requestFailed, setRequestFailed] = useState(false);
  const eventRevision = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    const revision = eventRevision.current;
    const cleanup = window.updater.onInstallStateChanged((next) => {
      eventRevision.current++;
      setState(next);
      setDismissed(false);
    });
    // Subscribe first, then hydrate: a newer event must win over a slow snapshot.
    void window.updater.getInstallState().then((snapshot) => {
      if (mounted.current && eventRevision.current === revision) setState(snapshot);
    }).catch(() => { /* A later update event can still populate the prompt. */ });
    return () => { mounted.current = false; cleanup(); };
  }, []);

  const install = async () => {
    if (requestPending || state.status === "checking") return;
    setRequestPending(true);
    setRequestFailed(false);
    const revision = eventRevision.current;
    try {
      const result = await window.updater.installUpdate();
      if (mounted.current && eventRevision.current === revision) setState(result);
    } catch {
      if (mounted.current) setRequestFailed(true);
    } finally {
      if (mounted.current) setRequestPending(false);
    }
  };

  if (state.status === "idle") return null;
  if (dismissed) return null;
  const checking = requestPending || state.status === "checking";

  return (
    <div className="fixed bottom-4 right-4 z-50 w-80 rounded-lg border border-border bg-background p-4 shadow-lg animate-in slide-in-from-bottom-2 fade-in duration-300">
      <button
        type="button"
        aria-label="Dismiss update notification"
        onClick={() => setDismissed(true)}
        className="absolute top-2 right-2 rounded-md p-1 text-muted-foreground hover:text-foreground transition-colors"
      >
        <X className="size-3.5" />
      </button>

      <div className="flex items-start gap-3">
        <div className="mt-0.5 rounded-md bg-success/10 p-1.5">
          <RefreshCw className="size-4 text-success" />
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-body font-medium">Update ready</p>
          <p className="text-caption text-muted-foreground mt-0.5">
            {window.updater.installRequiresStoppedRuntime === true
              ? `v${state.version} is ready. Installation waits until the bundled runtime is stopped.`
              : `v${state.version} will be applied on next launch.`}
          </p>
          {state.status === "deferred" && (
            <p role="status" className="text-caption text-muted-foreground mt-2">
              {state.reason === "runtime_running"
                ? "Finish active runs, then open Runtimes, select this computer, choose Stop and retry. This check never stops agents."
                : "Runtime status could not be checked. Finish active runs, then open Runtimes, select this computer and choose Stop. Download the matching installer below, quit Multica, then run the installer."}
            </p>
          )}
          {state.status === "deferred" && state.reason === "probe_failed" && (
            <button
              type="button"
              onClick={() => window.desktopAPI.openExternal("https://multica.ai/download")}
              className="mt-2 text-caption font-medium text-primary underline-offset-4 hover:underline"
            >
              Download installer
            </button>
          )}
          {requestFailed && (
            <p role="alert" className="text-caption text-destructive mt-2">Could not request installation. Try again.</p>
          )}
          <div className="mt-2 flex items-center gap-1.5">
            <button
              type="button"
              onClick={() =>
                window.desktopAPI.openExternal(changelogUrl(state.version))
              }
              className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-caption font-medium text-foreground hover:bg-accent transition-colors"
            >
              See changelog
            </button>
            <button
              type="button"
              onClick={() => void install()}
              disabled={checking}
              aria-busy={checking || undefined}
              className="inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-caption font-medium text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {checking ? "Checking runtime…" : state.status === "deferred" ? "Retry installation" : "Restart now"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
