import { useCallback, useEffect, useState } from "react";
import { ArrowDownToLine, RefreshCw, X } from "lucide-react";

type UpdateState =
  | { status: "idle" }
  | { status: "available"; version: string }
  | { status: "downloading"; percent: number }
  | { status: "ready" };

export function UpdateNotification() {
  const [state, setState] = useState<UpdateState>({ status: "idle" });
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    const cleanups: (() => void)[] = [];

    cleanups.push(
      window.updater.onUpdateAvailable((info) => {
        setState({ status: "available", version: info.version });
        setDismissed(false);
      }),
    );

    cleanups.push(
      window.updater.onDownloadProgress((progress) => {
        setState({ status: "downloading", percent: progress.percent });
      }),
    );

    cleanups.push(
      window.updater.onUpdateDownloaded(() => {
        setState({ status: "ready" });
      }),
    );

    return () => cleanups.forEach((fn) => fn());
  }, []);

  const handleDownload = useCallback(() => {
    // Prevent double-click: immediately transition to downloading state
    if (state.status !== "available") return;
    setState({ status: "downloading", percent: 0 });
    window.updater.downloadUpdate();
  }, [state.status]);

  const handleInstall = useCallback(() => {
    window.updater.installUpdate();
  }, []);

  // Only allow dismiss when update is available (not during download or ready)
  if (state.status === "idle") return null;
  if (dismissed && state.status === "available") return null;

  return (
    <div className="fixed bottom-4 right-4 z-50 w-80 rounded-lg border border-border bg-background p-4 shadow-lg animate-in slide-in-from-bottom-2 fade-in duration-300">
      <button
        onClick={() => setDismissed(true)}
        className="absolute top-2 right-2 rounded-md p-1 text-muted-foreground hover:text-foreground transition-colors"
      >
        <X className="size-3.5" />
      </button>

      {state.status === "available" && (
        <div className="flex items-start gap-3">
          <div className="mt-0.5 rounded-md bg-primary/10 p-1.5">
            <ArrowDownToLine className="size-4 text-primary" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium">New version available</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              v{state.version} is ready to download
            </p>
            <button
              onClick={handleDownload}
              className="mt-2 inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              Download update
            </button>
          </div>
        </div>
      )}

      {state.status === "downloading" && (
        <div className="flex items-start gap-3">
          <div className="mt-0.5 rounded-md bg-primary/10 p-1.5">
            <ArrowDownToLine className="size-4 text-primary animate-pulse" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium">Downloading update...</p>
            <div className="mt-2 h-1.5 w-full rounded-full bg-muted overflow-hidden">
              <div
                className="h-full rounded-full bg-primary transition-all duration-300"
                style={{ width: `${Math.round(state.percent)}%` }}
              />
            </div>
            <p className="text-xs text-muted-foreground mt-1">
              {Math.round(state.percent)}%
            </p>
          </div>
        </div>
      )}

      {state.status === "ready" && (
        <div className="flex items-start gap-3">
          <div className="mt-0.5 rounded-md bg-success/10 p-1.5">
            <RefreshCw className="size-4 text-success" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium">Update ready</p>
            <p className="text-xs text-muted-foreground mt-0.5">
              Restart to apply the update
            </p>
            <div className="mt-2 flex items-center gap-1.5">
              {/* Secondary "See changes" — gives the user a reason to
                  restart by surfacing what they're about to get. Opens
                  in the default browser via the shared openExternal
                  bridge so the URL hits the same allow-list as every
                  other outbound link. */}
              <button
                onClick={() => window.desktopAPI.openExternal("https://multica.ai/changelog")}
                className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-xs font-medium text-foreground hover:bg-accent transition-colors"
              >
                See changes
              </button>
              <button
                onClick={handleInstall}
                className="inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
              >
                Restart now
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
