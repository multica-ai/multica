// The one module the host loader evaluates BEFORE react-dom.
//
// Importing this module runs bippy's side-effect that installs
// `window.__REACT_DEVTOOLS_GLOBAL_HOOK__`. React only registers itself with the
// hook that exists at the moment `react-dom` first evaluates, so this module
// MUST be imported before the app bootstrap (see MUL-4466 §6.2, Desktop
// two-stage bootstrap). It deliberately does NOT import react / react-dom.
//
// `instrument()` from bippy composes onto the hook instead of replacing it, so
// an already-present React DevTools install or another bippy consumer keeps
// working. Installation is idempotent and HMR-safe.
import { instrument } from "bippy";

/** A committed fiber root, kept structurally loose to avoid coupling to bippy's internal types. */
export type CommitRoot = { current: unknown } & Record<string, unknown>;
export type CommitListener = (root: CommitRoot) => void;

const commitListeners = new Set<CommitListener>();
let uninstrument: (() => void) | null = null;

/**
 * Ensure the React DevTools hook exists and our commit dispatcher is attached.
 * Safe to call multiple times; only the first call patches the hook.
 */
export function installRecorderHook(): void {
  if (uninstrument) return;
  uninstrument = instrument({
    onCommitFiberRoot(_rendererID: number, root: unknown) {
      // fail-open for the host app: a broken listener never bubbles into React.
      for (const listener of commitListeners) {
        try {
          listener(root as CommitRoot);
        } catch {
          /* swallow — recording must never break the page */
        }
      }
    },
  });
}

/** True once the global hook is present on `window`. */
export function isRecorderHookInstalled(): boolean {
  return (
    uninstrument !== null &&
    typeof window !== "undefined" &&
    typeof (window as unknown as Record<string, unknown>).__REACT_DEVTOOLS_GLOBAL_HOOK__ !==
      "undefined"
  );
}

/** Subscribe to React commits. Returns an unsubscribe function. */
export function subscribeCommit(listener: CommitListener): () => void {
  commitListeners.add(listener);
  return () => commitListeners.delete(listener);
}

/** Detach our dispatcher and drop listeners (used on full teardown / HMR dispose). */
export function uninstallRecorderHook(): void {
  uninstrument?.();
  uninstrument = null;
  commitListeners.clear();
}
