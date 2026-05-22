import { create } from "zustand";

/**
 * Welcome signal — written by Step 3 of the onboarding flow when the user
 * clicks "Start exploring" or "Skip", consumed exactly once by the
 * `<WelcomeAfterOnboarding />` component mounted inside the workspace
 * shell.
 *
 * The signal is the bridge between the onboarding screen and the workspace
 * shell that succeeds it. `onboarded_at` is already true by the time we
 * land in the workspace — this carries the *transient* "I just finished
 * Step 3, here is what I chose" intent without persisting it on the user
 * row (the v2 design persisted it as fields and leaked complexity
 * everywhere).
 *
 * `choice` is "runtime" or "skip":
 *   - "runtime": user picked a runtime in Step 3. `runtimeId` is set. The
 *     workspace welcome hook creates a Multica Helper agent on that runtime
 *     and presents starter cards.
 *   - "skip": user explicitly skipped. `runtimeId` is undefined. The hook
 *     seeds two issues (install-runtime guide + create-agent guide) and
 *     shows them in a Modal.
 */
export interface WelcomeSignal {
  workspaceId: string;
  choice: "runtime" | "skip";
  runtimeId?: string;
}

interface WelcomeStoreState {
  signal: WelcomeSignal | null;
  /**
   * True after the user has explicitly engaged with the Welcome surface —
   * picked a starter card, opened a seeded issue, closed the Skip modal.
   * Components MUST treat `signal && !dismissed` as the render condition;
   * `dismiss()` is what removes the modal from view.
   */
  dismissed: boolean;
  set: (signal: WelcomeSignal) => void;
  /**
   * Mark the Welcome surface as engaged-with. Idempotent. Keeps the
   * signal in the store as a record but components will stop rendering.
   * We don't fully clear the store on dismiss so a debug peek (or a
   * future analytics hook) can still see which path the user took.
   */
  dismiss: () => void;
  /**
   * Full reset — clear signal AND dismissed. Used by tests and by the
   * user logout flow (so a different user logging into the same browser
   * doesn't inherit a stale signal).
   */
  reset: () => void;
}

/**
 * Zustand store, deliberately NOT persisted. Reloading the page drops the
 * signal — that's the correct behavior: the user already finished
 * onboarding (onboarded_at is set), and Welcome was a one-shot UI. We
 * never want a refresh to re-trigger Helper agent creation.
 *
 * Why subscription model instead of read-once-consume: React 18+ StrictMode
 * dev mode mounts components twice (mount → unmount → mount). If the
 * component consumed the store in a `useState` initializer, the second
 * mount would see an already-empty store and render null — Welcome would
 * never appear in dev. Subscribing to a stable store + tracking dismissal
 * in the store sidesteps both mounts seeing the same state.
 */
export const useWelcomeStore = create<WelcomeStoreState>((set) => ({
  signal: null,
  dismissed: false,
  set: (signal) => set({ signal, dismissed: false }),
  dismiss: () => set({ dismissed: true }),
  reset: () => set({ signal: null, dismissed: false }),
}));
