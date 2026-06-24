import { CheckCircle2, ChevronRight } from "lucide-react";
import { useActorName } from "@multica/core/workspace/hooks";
import { Card } from "@multica/ui/components/ui/card";
import type { TimelineEntry } from "@multica/core/types";
import { useT } from "../../i18n";

interface ResolvedThreadBarProps {
  /** The resolved root comment. */
  entry: TimelineEntry;
  /**
   * Flat list of every nested reply under this thread root. Precomputed by
   * `issue-detail.tsx`'s `timelineView` from the same walk that CommentCard
   * uses, so the count + author list match what the expanded view renders
   * (direct-children-only would undercount nested replies).
   */
  replies: TimelineEntry[];
  onExpand: () => void;
}

const MAX_NAMED_AUTHORS = 2;

// Distinct authors across `entries`, first-seen order, collapsed to a label
// ("Alice", "Alice, Bob", "Alice, Bob and 2 others"). Shared by both bars.
function useAuthorsLabel(entries: TimelineEntry[]): string {
  const { t } = useT("issues");
  const { getActorName } = useActorName();

  const seen = new Set<string>();
  const authors: Array<{ type: string; id: string }> = [];
  for (const e of entries) {
    const key = `${e.actor_type}:${e.actor_id}`;
    if (seen.has(key)) continue;
    seen.add(key);
    authors.push({ type: e.actor_type, id: e.actor_id });
  }

  if (authors.length <= MAX_NAMED_AUTHORS) {
    return authors.map((a) => getActorName(a.type, a.id)).join(", ");
  }
  const named = authors.slice(0, MAX_NAMED_AUTHORS).map((a) => getActorName(a.type, a.id)).join(", ");
  return t(($) => $.comment.resolve.bar_authors_more, {
    names: named,
    count: authors.length - MAX_NAMED_AUTHORS,
  });
}

/**
 * Whole-thread fold — the ROOT comment is resolved ("Resolve thread"). The
 * entire thread (root + every reply) collapses into this one bar.
 */
export function ResolvedThreadBar({ entry, replies, onExpand }: ResolvedThreadBarProps) {
  const { t } = useT("issues");
  const authorsLabel = useAuthorsLabel([entry, ...replies]);
  const count = 1 + replies.length;

  return (
    <Card className="!py-0 !gap-0 overflow-hidden">
      <button
        type="button"
        onClick={onExpand}
        className="flex w-full items-center justify-between px-4 py-3 text-left transition-colors cursor-pointer hover:bg-muted/50"
      >
        <span className="flex min-w-0 items-center gap-2.5 text-sm text-muted-foreground">
          <CheckCircle2 className="h-4 w-4 shrink-0" />
          <span className="truncate">
            {t(($) => $.comment.resolve.bar, { count, authors: authorsLabel })}
          </span>
        </span>
        <ChevronRight className="h-3.5 w-3.5 rotate-90 shrink-0 text-muted-foreground" />
      </button>
    </Card>
  );
}

interface CommentsFoldBarProps {
  /** The non-resolution replies folded behind this bar. */
  replies: TimelineEntry[];
  onExpand: () => void;
}

/**
 * Middle fold — a REPLY is the resolution ("Resolve thread with comment"). The
 * root and the resolution stay visible; the other replies fold behind this bar,
 * which sits between them.
 */
export function CommentsFoldBar({ replies, onExpand }: CommentsFoldBarProps) {
  const { t } = useT("issues");
  const authorsLabel = useAuthorsLabel(replies);

  return (
    <button
      type="button"
      onClick={onExpand}
      className="flex w-full items-center justify-between rounded-md bg-muted/45 px-3 py-2.5 text-left transition-colors cursor-pointer hover:bg-muted"
    >
      <span className="flex min-w-0 items-center gap-2.5 text-sm text-muted-foreground">
        <ChevronRight className="h-3.5 w-3.5 rotate-90 shrink-0" />
        <span className="truncate">
          {t(($) => $.comment.resolve.fold, { count: replies.length, authors: authorsLabel })}
        </span>
      </span>
    </button>
  );
}
