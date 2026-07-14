import type { CommitRoot } from "./install";
import type { ReactCommitEvidence } from "./types";

// React fiber tag for a <Profiler> node. Stable across React 17–19.
const PROFILER_TAG = 12;
// Hard cap on fibers visited per commit — bounds cost and guards against
// pathological trees / self-observation loops.
const MAX_FIBERS_PER_COMMIT = 4000;

/**
 * Minimal structural view of a fiber. We only ever touch these fields, and on a
 * fiber we only read `memoizedProps.id` when the fiber is a <Profiler> (that
 * `id` is the boundary registration key, not user content). No other prop,
 * state, context, or DOM text is ever read (MUL-4466 §8.2, §12).
 */
interface FiberLike {
  tag?: number;
  actualDuration?: number | null;
  alternate?: FiberLike | null;
  child?: FiberLike | null;
  sibling?: FiberLike | null;
  memoizedProps?: unknown;
}

function phaseOf(fiber: FiberLike): ReactCommitEvidence["phase"] {
  if (fiber.alternate == null) return "mount";
  return "update";
}

function readProfilerId(fiber: FiberLike): string | null {
  if (fiber.tag !== PROFILER_TAG) return null;
  const props = fiber.memoizedProps;
  if (props && typeof props === "object" && "id" in props) {
    const id = (props as { id?: unknown }).id;
    return typeof id === "string" ? id : null;
  }
  return null;
}

export interface CommitExtract {
  /** Total commit duration read off the root fiber, when available. */
  commitActualDurationMs: number | null;
  phase: ReactCommitEvidence["phase"];
  /** Per-boundary evidence, only for host-registered Profiler ids. */
  boundaries: ReactCommitEvidence[];
}

/**
 * Extract commit timing from a committed fiber root. Pure and synchronous so it
 * can be unit-tested against synthetic fibers without a live React tree.
 */
export function extractCommitEvidence(
  root: CommitRoot,
  boundaryAllowlist: ReadonlySet<string>,
): CommitExtract {
  const rootFiber = (root.current ?? null) as FiberLike | null;
  const commitActualDurationMs =
    rootFiber && typeof rootFiber.actualDuration === "number" ? rootFiber.actualDuration : null;
  const phase = rootFiber ? phaseOf(rootFiber) : "unknown";

  const boundaries: ReactCommitEvidence[] = [];
  if (rootFiber && boundaryAllowlist.size > 0) {
    let visited = 0;
    // Iterative depth-first walk over child/sibling pointers.
    const stack: FiberLike[] = [rootFiber];
    while (stack.length > 0 && visited < MAX_FIBERS_PER_COMMIT) {
      const fiber = stack.pop()!;
      visited++;
      const id = readProfilerId(fiber);
      if (id !== null && boundaryAllowlist.has(id)) {
        boundaries.push({
          boundaryId: id,
          phase: phaseOf(fiber),
          actualDurationMs: typeof fiber.actualDuration === "number" ? fiber.actualDuration : 0,
        });
      }
      if (fiber.child) stack.push(fiber.child);
      if (fiber.sibling) stack.push(fiber.sibling);
    }
  }

  return { commitActualDurationMs, phase, boundaries };
}
