import type { TimelineEntry } from "@multica/core/types";

/** Collapse the middle when a thread exceeds this many replies. */
export const THREAD_REPLY_COLLAPSE_THRESHOLD = 7;

/** Replies kept at the top of a collapsed thread. */
export const THREAD_REPLY_HEAD_VISIBLE = 2;

/** Replies kept at the bottom of a collapsed thread (near the composer). */
export const THREAD_REPLY_TAIL_VISIBLE = 2;

export type ThreadReplyDisplay =
  | { kind: "all"; replies: TimelineEntry[] }
  | {
      kind: "collapsed";
      head: TimelineEntry[];
      tail: TimelineEntry[];
      hidden: TimelineEntry[];
      hiddenCount: number;
    };

/**
 * Walks the parent_id graph rooted at `rootId` and returns every descendant in
 * traversal order. Shared between CommentCard (which renders the expanded
 * thread) and ResolvedThreadBar (which displays the collapsed count + author
 * list) so the two views stay in sync — direct-children-only counts diverge
 * once nested replies exist (see Emacs review on PR #2300).
 */
export function collectThreadReplies(
  rootId: string,
  repliesByParent: Map<string, TimelineEntry[]>,
): TimelineEntry[] {
  const out: TimelineEntry[] = [];
  const walk = (id: string) => {
    const children = repliesByParent.get(id) ?? [];
    for (const child of children) {
      out.push(child);
      walk(child.id);
    }
  };
  walk(rootId);
  return out;
}

/**
 * GitHub-style reply windowing: when a thread is long, show the first and
 * last few replies and fold everything in between behind a "Show N hidden"
 * affordance.
 */
export function partitionThreadReplies(
  replies: TimelineEntry[],
  expanded: boolean,
  options?: {
    enabled?: boolean;
    threshold?: number;
    headCount?: number;
    tailCount?: number;
  },
): ThreadReplyDisplay {
  const enabled = options?.enabled !== false;
  const threshold = options?.threshold ?? THREAD_REPLY_COLLAPSE_THRESHOLD;
  const headCount = options?.headCount ?? THREAD_REPLY_HEAD_VISIBLE;
  const tailCount = options?.tailCount ?? THREAD_REPLY_TAIL_VISIBLE;

  if (!enabled || expanded || replies.length <= threshold) {
    return { kind: "all", replies };
  }

  const head = replies.slice(0, headCount);
  const tail = replies.slice(replies.length - tailCount);
  const hidden = replies.slice(headCount, replies.length - tailCount);

  return {
    kind: "collapsed",
    head,
    tail,
    hidden,
    hiddenCount: hidden.length,
  };
}
