/**
 * Custom day layout algorithm for react-big-calendar.
 *
 * Matches Toggl Track behavior: uses RBC's default overlap algorithm for initial
 * top/height positioning, then re-computes column assignments with **strict** overlap
 * detection so that sequential entries (A ends exactly when B starts) stack vertically
 * instead of side-by-side.
 */
// @ts-expect-error — no type declarations for this internal RBC module
import overlapModule from "react-big-calendar/lib/utils/layout-algorithms/overlap";

type StyledEvent = {
  event: unknown;
  friends: StyledEvent[];
  idx: number;
  size: number;
  style: {
    height: number | string;
    left: number | string;
    top: number | string;
    width: number | string;
    xOffset: number | string;
  };
};

// Vite's CJS interop may double-wrap the default export — resolve it.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function resolveDefault(mod: any): (args: any) => StyledEvent[] {
  if (typeof mod === "function") return mod;
  if (typeof mod?.default === "function") return mod.default;
  throw new Error("react-big-calendar overlap layout module could not be resolved");
}
const getDefaultOverlapLayout = resolveDefault(overlapModule);

/** Strict overlap: value is strictly inside (0, rangeEnd). */
function strictlyInside(value: number, rangeEnd: number): boolean {
  return value < rangeEnd;
}

/** Recursively count the deepest chain of friends to determine column count. */
function maxFriendDepth(event: StyledEvent, depth: number, visited: StyledEvent[]): number {
  visited.push(event);
  let max = depth;
  for (const friend of event.friends) {
    if (!visited.includes(friend)) {
      const d = maxFriendDepth(friend, depth + 1, visited);
      if (d > max) max = d;
    }
  }
  return max;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function calendarDayLayout(args: any): StyledEvent[] {
  // Step 1: run RBC's default overlap algorithm for initial top/height.
  const items: StyledEvent[] = getDefaultOverlapLayout(args);

  // Step 2: sort by top position, then longest-duration first.
  items.sort((a, b) => {
    const aTop = Number(a.style.top);
    const bTop = Number(b.style.top);
    if (aTop !== bTop) return aTop > bTop ? 1 : -1;
    const aBottom = aTop + Number(a.style.height);
    const bBottom = bTop + Number(b.style.height);
    return aBottom < bBottom ? 1 : -1;
  });

  // Step 3: reset column assignments from the default algorithm.
  for (const item of items) {
    item.friends = [];
    delete (item.style as Record<string, unknown>).left;
    delete (item as Record<string, unknown>).idx;
    delete (item as Record<string, unknown>).size;
  }

  // Step 4: find "friends" (truly overlapping events) using strict comparison.
  for (let i = 0; i < items.length - 1; i++) {
    const a = items[i]!;
    const aTop = Number(a.style.top);
    const aBottom = aTop + Number(a.style.height);
    for (let j = i + 1; j < items.length; j++) {
      const b = items[j]!;
      const bTop = Number(b.style.top);
      const bBottom = bTop + Number(b.style.height);
      if (
        (bTop <= aTop && strictlyInside(aTop, bBottom)) ||
        (aTop <= bTop && strictlyInside(bTop, aBottom))
      ) {
        a.friends.push(b);
        b.friends.push(a);
      }
    }
  }

  // Step 5: greedily assign column indices.
  for (const item of items) {
    const taken = Array.from({ length: 100 }, () => 1);
    for (const friend of item.friends) {
      if (friend.idx !== undefined) {
        taken[friend.idx] = 0;
      }
    }
    item.idx = taken.indexOf(1);
  }

  // Step 6: calculate column sizes based on max friend depth.
  for (const item of items) {
    if (item.size) continue;
    const visited: StyledEvent[] = [];
    const size = 100 / (maxFriendDepth(item, 0, visited) + 1);
    item.size = size;
    for (const v of visited) {
      v.size = size;
    }
  }

  // Step 7: apply final width/left/xOffset styles.
  for (const item of items) {
    item.style.left = item.idx * item.size;

    // If this is the rightmost column among friends, extend to fill remaining space.
    let maxFriendIdx = 0;
    for (const friend of item.friends) {
      if (friend.idx > maxFriendIdx) maxFriendIdx = friend.idx;
    }
    if (maxFriendIdx <= item.idx) {
      item.size = 100 - item.idx * item.size;
    }

    const gap = item.idx === 0 ? 0 : 3;
    item.style.width = `calc(${item.size}% - ${gap}px)`;
    // Use max() to guarantee a minimum visual height (~4px in a typical day column).
    const heightPct = Number(item.style.height);
    const MIN_HEIGHT_PCT = 0.28;
    item.style.height = `calc(max(${heightPct}%, ${MIN_HEIGHT_PCT}%))`;
    item.style.xOffset = `calc(${item.style.left}% + ${gap}px)`;
  }

  return items;
}
