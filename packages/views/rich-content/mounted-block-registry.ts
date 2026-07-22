/**
 * Records which rich blocks have already been mounted in this page session.
 *
 * The "mount once" guarantee cannot live in component state alone. Chat's list
 * is virtualized: scrolling far enough unmounts a row entirely, taking any
 * local `mounted` flag with it. Scrolling back would then re-run Mermaid, build
 * a fresh sandboxed iframe, and discard the viewer's pan/zoom — the repeated
 * cost the lazy shell exists to avoid, reappearing on every pass.
 *
 * Keyed by a hash of language + source, so the same diagram is recognised
 * wherever it reappears (a recycled Virtuoso row, the live -> persisted handoff,
 * or the same content quoted in two places).
 *
 * Deliberately module-level rather than a store: this is render bookkeeping, not
 * application state. It is never persisted and never read on the server.
 */

// Bounded so a very long session cannot grow this without limit. Entries are
// short hash strings and eviction is oldest-first (Set preserves insertion
// order); evicting a block only means it pays its mount cost once more.
const MAX_TRACKED_BLOCKS = 500;

const mountedBlocks = new Set<string>();

// DJB2 — the same cheap, synchronous hash the Mermaid layout cache uses. The
// source text itself is unsuitable as a key: a transcript full of large
// diagrams would keep every one of them alive in this Set.
function hashSource(source: string): string {
  let hash = 5381;
  for (let i = 0; i < source.length; i++) {
    hash = ((hash << 5) + hash) ^ source.charCodeAt(i);
  }
  return (hash >>> 0).toString(36);
}

export function mountedBlockKey(language: string, source: string): string {
  return `${language}:${hashSource(source)}`;
}

export function hasBlockMounted(key: string): boolean {
  return mountedBlocks.has(key);
}

export function markBlockMounted(key: string): void {
  if (mountedBlocks.has(key)) return;
  if (mountedBlocks.size >= MAX_TRACKED_BLOCKS) {
    const oldest = mountedBlocks.values().next();
    if (!oldest.done) mountedBlocks.delete(oldest.value);
  }
  mountedBlocks.add(key);
}

/** Test-only: the registry outlives individual renders by design. */
export function resetMountedBlocks(): void {
  mountedBlocks.clear();
}
