// Desktop renderer entry — a thin, two-stage pre-bootstrap loader.
//
// It exists so the dev-only performance recorder can install its React DevTools
// hook BEFORE react-dom is ever imported. bippy (the hook library) only sees
// commits from renderers that register AFTER the hook exists on `window`, so the
// hook install must strictly precede the react-dom evaluation that happens
// inside ./app-bootstrap (MUL-4466 §6.2 Desktop two-stage bootstrap).
//
// Do NOT import react / react-dom / the app root here — that would defeat the
// ordering guarantee. Everything React lives in ./app-bootstrap.

async function bootstrap(): Promise<void> {
  // perf-recorder: Dev/Profiling-only frontend performance flight recorder.
  // Opt-in per developer via VITE_PERF_RECORDER (local, gitignored env file);
  // the whole branch is tree-shaken out of production builds, so Production
  // never loads, requests, or exposes the recorder. Installing the hook must
  // complete before ./app-bootstrap evaluates react-dom.
  if (import.meta.env.DEV && import.meta.env.VITE_PERF_RECORDER) {
    try {
      const { installRecorderHook, createRecorder } = await import("@multica/perf-recorder");
      installRecorderHook();
      createRecorder({
        appVersion: import.meta.env.VITE_APP_VERSION ?? "dev",
        surface: "desktop-renderer",
        mode: "development",
      });
    } catch (error) {
      // A dev-tool failure must never blank the app — log once and continue.
      console.error("[perf-recorder] failed to load; continuing without it", error);
    }
  }

  // Stage two: load react-dom + the app. The hook (if any) is already installed.
  await import("./app-bootstrap");
}

void bootstrap();
