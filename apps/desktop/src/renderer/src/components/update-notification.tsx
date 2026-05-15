import { useEffect, useState } from "react";
import { Loader2, RefreshCw, X } from "lucide-react";

// Downloads run silently in the background (main process has
// autoDownload=true). The renderer only renders UI once the package is fully
// downloaded and waiting for a restart.
type UpdateState =
  | { status: "idle" }
  | { status: "ready"; version: string };

export function UpdateNotification() {
  const [state, setState] = useState<UpdateState>({ status: "idle" });
  const [dismissed, setDismissed] = useState(false);
  const [installing, setInstalling] = useState(false);

  useEffect(() => {
    const cleanup = window.updater.onUpdateDownloaded((info) => {
      setState({ status: "ready", version: info.version });
      setDismissed(false);
      setInstalling(false);
    });
    return cleanup;
  }, []);
  if (state.status === "idle") return null;
  if (dismissed) return null;

  return (
    <div className="fixed bottom-4 right-4 z-50 w-80 rounded-lg border border-border bg-background p-4 shadow-lg animate-in slide-in-from-bottom-2 fade-in duration-300">
      <button
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
          <p className="text-sm font-medium">Update ready</p>
          <p className="text-xs text-muted-foreground mt-0.5">
            v{state.version} will be applied on next launch.
          </p>
          <div className="mt-2 flex items-center gap-1.5">
            <button
              onClick={() => setDismissed(true)}
              className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
            >
              Later
            </button>
            <button
              onClick={() =>
                window.desktopAPI.openExternal("https://multica.furtherref.com/changelog")
              }
              disabled={installing}
              className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors disabled:opacity-60"
            >
              See changes
            </button>
            <button
              onClick={() => {
                if (installing) return;
                setInstalling(true);
                void window.updater.installUpdate().catch(() => {
                  setInstalling(false);
                });
              }}
              disabled={installing}
              aria-busy={installing}
              className="inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-60"
            >
              {installing && <Loader2 className="size-3 animate-spin" />}
              {installing ? "Restarting..." : "Restart now"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
