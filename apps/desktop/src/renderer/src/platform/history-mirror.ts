import type { DataRouter } from "react-router-dom";

/**
 * Per-tab history mirror for the Session-model tab runtime (MUL-4475).
 *
 * React Router 7.14.0's `createMemoryRouter` owns navigation *semantics* but
 * does NOT expose its internal entries stack. To persist a tab's full history
 * (`entries: fullPath[]` + `index`, incl. search/hash) we keep a serializable
 * *mirror* that is a pure projection of the router's observed
 * `(location, historyAction)` transitions. The router remains authoritative for
 * what a navigation *does*; the mirror only records the resulting stack.
 *
 * The behavior projected here is locked by
 * `memory-router.characterization.test.tsx`:
 *   - navigate(path)              => PUSH   (append, truncating any forward tail)
 *   - navigate(path,{replace})    => REPLACE(overwrite the current entry)
 *   - <Navigate replace> / index  => REPLACE(render-driven, arrives post-mount)
 *   - navigate(±n)                => POP    (move index by the *known* delta)
 *   - navigate(1) past the end    => no-op  (=> the index clamp below)
 *
 * POP determinism: a memory router has no browser back button, so every POP in
 * the app is one we initiate via `navigateByDelta`. That records the exact
 * delta, so the mirror never has to guess direction/magnitude (the failure mode
 * of the old `popDirectionHints` map on duplicate-path stacks).
 *
 * Assumes the app's routes are loader-free (they are — see routes.tsx), so a
 * committed idle tick carries the final `(location, historyAction)`. Loader
 * `redirect()` during an in-flight PUSH is intentionally not special-cased.
 */

export interface HistorySnapshot {
  entries: string[];
  index: number;
}

type RouterSubscriber = Parameters<DataRouter["subscribe"]>[0];
type ObservedRouterState = Parameters<RouterSubscriber>[0];

/**
 * Pending relative-navigation deltas, keyed by router. The single mechanism by
 * which a POP's delta reaches the mirror observing that router. WeakMap so a
 * disposed router's entry is collected with it.
 */
const pendingPopDelta = new WeakMap<DataRouter, number>();

/**
 * The single entry point for relative (back/forward) navigation. Records the
 * delta for any `HistoryMirror` watching this router, then performs the POP.
 *
 * All direct `router.navigate(±n)` call sites (use-tab-history, the navigation
 * adapter's `back`) MUST route through here so the mirror's index arithmetic
 * stays exact. Returns the underlying navigation promise so callers/tests can
 * await settlement; call sites that fire-and-forget should prefix with `void`.
 */
export function navigateByDelta(router: DataRouter, delta: number): Promise<void> {
  if (delta === 0) return Promise.resolve();
  pendingPopDelta.set(router, delta);
  return router.navigate(delta);
}

/** Read and clear a router's pending POP delta, if any. */
export function consumePendingDelta(router: DataRouter): number | undefined {
  const delta = pendingPopDelta.get(router);
  if (delta !== undefined) pendingPopDelta.delete(router);
  return delta;
}

/** Serialize a location to the `pathname+search+hash` shape the mirror stores. */
export function locationToFullPath(location: {
  pathname: string;
  search: string;
  hash: string;
}): string {
  return `${location.pathname}${location.search}${location.hash}`;
}

/** Clamp an index into `[0, length-1]`; 0 for an empty stack (never expected). */
function clampIndex(index: number, length: number): number {
  if (length <= 0) return 0;
  return Math.max(0, Math.min(index, length - 1));
}

export class HistoryMirror {
  private entries: string[];
  private currentIndex: number;
  private unsubscribe: (() => void) | null;

  /**
   * @param router  A memory router whose transitions this mirror projects.
   * @param seed    The `{ entries, index }` the router was created from
   *                (`initialEntries` / `initialIndex`). Must match, or the
   *                mirror starts out of sync — the runtime seeds both from the
   *                same persisted session.
   */
  constructor(
    private readonly router: DataRouter,
    seed: HistorySnapshot,
  ) {
    this.entries =
      seed.entries.length > 0
        ? [...seed.entries]
        : [locationToFullPath(router.state.location)];
    this.currentIndex = clampIndex(seed.index, this.entries.length);
    this.unsubscribe = router.subscribe((state) => this.onCommit(state));
  }

  private onCommit(state: ObservedRouterState): void {
    // Only committed (idle) transitions mutate the stack. Loading ticks carry
    // the still-current location and a pending navigation that may yet redirect.
    if (state.navigation.state !== "idle") return;

    const full = locationToFullPath(state.location);
    switch (state.historyAction) {
      case "PUSH":
        // Drop any forward tail, then append. Diverging PUSH after a back
        // truncates the abandoned branch (characterization: no forward left).
        this.entries = this.entries.slice(0, this.currentIndex + 1);
        this.entries.push(full);
        this.currentIndex = this.entries.length - 1;
        break;
      case "REPLACE":
        this.entries[this.currentIndex] = full;
        break;
      case "POP": {
        const delta = consumePendingDelta(this.router) ?? this.inferDelta(full);
        this.currentIndex = clampIndex(
          this.currentIndex + delta,
          this.entries.length,
        );
        // The router is authoritative for the landed location; keep the mirror
        // entry consistent in case of any drift.
        this.entries[this.currentIndex] = full;
        break;
      }
    }
  }

  /**
   * Fallback for a POP with no recorded delta (an invariant violation in this
   * app, since all POPs go through `navigateByDelta`). Infer ±1 from the
   * neighbours; give up (0) if neither matches.
   */
  private inferDelta(full: string): number {
    if (this.currentIndex > 0 && this.entries[this.currentIndex - 1] === full) {
      return -1;
    }
    if (
      this.currentIndex < this.entries.length - 1 &&
      this.entries[this.currentIndex + 1] === full
    ) {
      return 1;
    }
    return 0;
  }

  /** Serializable snapshot for persisting into the tab session. */
  snapshot(): HistorySnapshot {
    return { entries: [...this.entries], index: this.currentIndex };
  }

  get index(): number {
    return this.currentIndex;
  }

  get canGoBack(): boolean {
    return this.currentIndex > 0;
  }

  get canGoForward(): boolean {
    return this.currentIndex < this.entries.length - 1;
  }

  dispose(): void {
    this.unsubscribe?.();
    this.unsubscribe = null;
  }
}
