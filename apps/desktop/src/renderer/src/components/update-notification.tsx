import { useEffect, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { RefreshCw, X } from "@multica/ui/icons";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { useT } from "@multica/views/i18n";

// Downloads run silently in the background (main process has
// autoDownload=true). The renderer only renders UI once the package is fully
// downloaded and waiting for a restart.
type UpdateState =
  | { status: "idle" }
  | { status: "ready"; version: string };

function changelogUrl(version: string): string {
  return `https://multica.ai/changelog#release-${version.replace(/\./g, "-")}`;
}

export function UpdateNotification() {
  const { t } = useT("settings");
  const prefersReducedMotion = useReducedMotion();
  const [state, setState] = useState<UpdateState>({ status: "idle" });
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    const cleanup = window.updater.onUpdateDownloaded((info) => {
      setState({ status: "ready", version: info.version });
      setDismissed(false);
    });
    return cleanup;
  }, []);

  const visible = state.status === "ready" && !dismissed;

  return (
    <AnimatePresence>
      {visible && (
        <motion.div
          key="update-notification"
          initial={{ opacity: 0, y: prefersReducedMotion ? 0 : 8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: prefersReducedMotion ? 0 : 8 }}
          transition={{
            duration: prefersReducedMotion ? 0 : UI_MOTION_DURATION.standard,
            ease: UI_EASE_OUT,
          }}
          className="fixed bottom-4 right-4 z-50 w-80 rounded-lg border border-border bg-background p-4 shadow-lg"
        >
          <button
            type="button"
            onClick={() => setDismissed(true)}
            className="absolute top-2 right-2 rounded-md p-1 text-muted-foreground hover:text-foreground transition-colors"
            aria-label={t(($) => $.desktop.updates.banner_dismiss)}
          >
            <X className="size-3.5" />
          </button>

          <div className="flex items-start gap-3">
            <div className="mt-0.5 rounded-md bg-success/10 p-1.5">
              <RefreshCw className="size-4 text-success" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-body font-medium">
                {t(($) => $.desktop.updates.banner_title)}
              </p>
              <p className="text-caption text-muted-foreground mt-0.5">
                {t(($) => $.desktop.updates.banner_body, { version: state.version })}
              </p>
              <div className="mt-2 flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() =>
                    window.desktopAPI.openExternal(changelogUrl(state.version))
                  }
                  className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-caption font-medium text-foreground hover:bg-accent transition-colors"
                >
                  {t(($) => $.desktop.updates.banner_changelog)}
                </button>
                <button
                  type="button"
                  onClick={() => window.updater.installUpdate()}
                  className="inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-caption font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
                >
                  {t(($) => $.desktop.updates.banner_restart)}
                </button>
              </div>
            </div>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
