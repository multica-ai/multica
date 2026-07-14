// Self-mounting entry for the `<script src>` global build (dist/auto.global.js),
// the shape the standalone package ships — same pattern as react-scan's
// dist/auto.global.js. Importing this module installs the hook immediately
// (before React, when the script is loaded early enough) and mounts the panel
// once the DOM is ready. The host may pre-set `window.__MULTICA_PERF_RECORDER__`
// with a HostConfig before the script loads.
import { installRecorderHook } from "./install";
import { mountPanel } from "./panel";
import { Recorder } from "./recorder";
import type { HostConfig, RecorderMode, Surface } from "./types";

// Install the hook at module-evaluation time — earliest possible moment.
installRecorderHook();

function readConfig(): HostConfig {
  const raw =
    typeof window !== "undefined"
      ? (window as unknown as Record<string, unknown>).__MULTICA_PERF_RECORDER__
      : undefined;
  const cfg = (raw && typeof raw === "object" ? raw : {}) as Partial<HostConfig>;
  const surface: Surface = cfg.surface === "desktop-renderer" ? "desktop-renderer" : "web";
  const mode: RecorderMode = cfg.mode === "profiling" ? "profiling" : "development";
  return {
    appVersion: typeof cfg.appVersion === "string" ? cfg.appVersion : "unknown",
    surface,
    mode,
    thresholds: cfg.thresholds,
    boundaryAllowlist: cfg.boundaryAllowlist,
    testIdAllowlist: cfg.testIdAllowlist,
  };
}

function boot(): void {
  const recorder = new Recorder(readConfig());
  mountPanel(recorder);
}

if (typeof document !== "undefined") {
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot, { once: true });
  } else {
    boot();
  }
}
