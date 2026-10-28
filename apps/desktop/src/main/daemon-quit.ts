interface QuitDependencies {
  loadPrefs: () => Promise<{ autoStop: boolean }>;
  pause: () => void;
  resume: (restoreDesired: boolean) => void;
  stop: () => Promise<boolean>;
  confirmIgnore: (error: unknown) => Promise<boolean>;
  quit: () => void;
}

export function createDaemonQuitHandler(deps: QuitDependencies) {
  let busy = false;
  let ready = false;
  return (event: { preventDefault: () => void }) => {
    if (ready) return;
    event.preventDefault();
    if (busy) return;
    busy = true;
    void (async () => {
      let paused = false;
      let restoreDesired = true;
      try {
        if ((await deps.loadPrefs()).autoStop) {
          deps.pause();
          paused = true;
          try {
            if (!await deps.stop()) return;
          } catch (error) {
            restoreDesired = false;
            if (!await deps.confirmIgnore(error)) return;
          }
        }
        ready = true;
        deps.quit();
      } finally {
        if (paused && !ready) deps.resume(restoreDesired);
        busy = false;
      }
    })().catch((error) => console.warn("[daemon] quit interrupted:", error));
  };
}
