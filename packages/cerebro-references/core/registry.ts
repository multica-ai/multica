import type { IssueReference, ObjectRenderer, ReferenceObject } from "./types";

// In-memory renderer registry. Object kinds are looked up by their wire
// string ("github_pr", "stripe_customer", …). The registry is process-wide
// and mutated at module load time when built-in renderers import — see
// renderers/index.ts. Last-registration-wins, which lets a consumer
// override a built-in if needed.
const renderers = new Map<ReferenceObject, ObjectRenderer>();

// The fallback renderer ships every method so callers can always reach
// into a renderer without null-checking. It is also the source of every
// default behaviour the registry layers on top of partially-populated
// custom renderers (see `getObjectRenderer`).
export const fallbackObjectRenderer: Required<ObjectRenderer> = {
  object: "*",
  formatTitle: (ref) => ref.label ?? ref.ref_id,
  formatBadge: () => null,
  resolveUrl: (ref) => ref.url,
};

export function registerObjectRenderer(renderer: ObjectRenderer): void {
  renderers.set(renderer.object, renderer);
}

// Resolve a renderer for an object kind. Always returns a renderer with
// every method populated — fields the caller didn't supply fall through
// to `fallbackObjectRenderer`, so consumers never need to null-check the
// returned methods.
export function getObjectRenderer(
  object: ReferenceObject,
): Required<ObjectRenderer> {
  const registered = renderers.get(object);
  if (!registered) return { ...fallbackObjectRenderer, object };
  return {
    object: registered.object,
    formatTitle: registered.formatTitle ?? fallbackObjectRenderer.formatTitle,
    formatBadge: registered.formatBadge ?? fallbackObjectRenderer.formatBadge,
    resolveUrl: registered.resolveUrl ?? fallbackObjectRenderer.resolveUrl,
  };
}

export function listRegisteredObjects(): ReferenceObject[] {
  return Array.from(renderers.keys());
}

// Test-only escape hatch. Not exported from `core/index.ts` because we
// don't want production code reaching into the registry's internals.
export function __resetObjectRenderers(): void {
  renderers.clear();
}

// Convenience helper for consumers that just want the resolved fields for
// a single reference — equivalent to calling getObjectRenderer + the
// three methods.
export function renderReference(ref: IssueReference): {
  title: string;
  badge: string | null;
  url: string | null;
} {
  const r = getObjectRenderer(ref.object);
  return {
    title: r.formatTitle(ref),
    badge: r.formatBadge(ref),
    url: r.resolveUrl(ref),
  };
}
