// Public API for host loaders that import the recorder as an ESM module
// (the in-repo dev-only path). The eventual standalone package also ships
// `./auto` (self-mounting) for the `<script src>` path — see src/auto.ts.

import { installRecorderHook } from "./install";
import { mountPanel, type PanelHandle } from "./panel";
import { Recorder } from "./recorder";
import type { HostConfig } from "./types";

export { installRecorderHook, isRecorderHookInstalled, uninstallRecorderHook } from "./install";
export { Recorder } from "./recorder";
export type { LiveStatus, RecorderState } from "./recorder";
export * from "./types";

export interface RecorderHandle {
  recorder: Recorder;
  panel: PanelHandle;
  destroy: () => void;
}

/**
 * Create a recorder and mount the (collapsed) panel. Does NOT start recording —
 * collection only begins when the user clicks Start (MUL-4466 §7). The hook is
 * installed here too, but for the mechanism to catch React commits it must have
 * been installed BEFORE react-dom evaluated — host loaders call
 * `installRecorderHook()` up front (Desktop two-stage bootstrap / Web
 * beforeInteractive) and this call is then an idempotent no-op.
 */
export function createRecorder(config: HostConfig): RecorderHandle {
  installRecorderHook();
  const recorder = new Recorder(config);
  const panel = mountPanel(recorder);
  return {
    recorder,
    panel,
    destroy: () => {
      recorder.clear();
      panel.destroy();
    },
  };
}
