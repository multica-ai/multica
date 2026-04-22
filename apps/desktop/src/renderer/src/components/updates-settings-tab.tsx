import { useCallback, useState } from "react";
import { AlertCircle, ArrowDownToLine, Check, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

type CheckState =
  | { status: "idle" }
  | { status: "checking" }
  | { status: "up-to-date"; currentVersion: string }
  | { status: "available"; latestVersion: string }
  | { status: "error"; message: string };

export function UpdatesSettingsTab() {
  const [state, setState] = useState<CheckState>({ status: "idle" });

  const handleCheck = useCallback(async () => {
    setState({ status: "checking" });
    const result = await window.updater.checkForUpdates();
    if (!result.ok) {
      setState({ status: "error", message: result.error });
      return;
    }
    setState(
      result.available
        ? { status: "available", latestVersion: result.latestVersion }
        : { status: "up-to-date", currentVersion: result.currentVersion },
    );
  }, []);

  return (
    <div>
      <h2 className="text-lg font-semibold">Updates</h2>
      <p className="text-sm text-muted-foreground mt-1">
        The desktop app checks for new versions automatically once an hour and
        shortly after launch.
      </p>

      <div className="mt-6 divide-y">
        <div className="flex items-start justify-between gap-6 py-4">
          <div className="min-w-0">
            <p className="text-sm font-medium">Check for updates</p>
            <p className="text-sm text-muted-foreground mt-0.5">
              Trigger a check now instead of waiting for the next automatic
              poll. Available updates appear as a notification in the corner.
            </p>
            {state.status === "up-to-date" && (
              <p className="text-sm text-muted-foreground mt-2 inline-flex items-center gap-1.5">
                <Check className="size-3.5 text-success" />
                You&apos;re on the latest version (v{state.currentVersion}).
              </p>
            )}
            {state.status === "available" && (
              <p className="text-sm text-muted-foreground mt-2 inline-flex items-center gap-1.5">
                <ArrowDownToLine className="size-3.5 text-primary" />
                v{state.latestVersion} is available — see the download prompt
                in the corner.
              </p>
            )}
            {state.status === "error" && (
              <p className="text-sm text-destructive mt-2 inline-flex items-center gap-1.5">
                <AlertCircle className="size-3.5" />
                {state.message}
              </p>
            )}
          </div>
          <div className="shrink-0">
            <Button
              variant="outline"
              size="sm"
              onClick={handleCheck}
              disabled={state.status === "checking"}
            >
              {state.status === "checking" ? (
                <>
                  <Loader2 className="size-3.5 animate-spin" />
                  Checking…
                </>
              ) : (
                "Check now"
              )}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
