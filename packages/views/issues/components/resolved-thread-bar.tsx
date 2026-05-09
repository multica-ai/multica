import { CheckCircle2, ChevronRight } from "lucide-react";
import { useActorName } from "@multica/core/workspace/hooks";
import { Card } from "@multica/ui/components/ui/card";
import type { TimelineEntry } from "@multica/core/types";
import { collectThreadReplies } from "./thread-utils";
import { useT } from "../../i18n";

interface ResolvedThreadBarProps {
  /** The resolved root comment. */
  entry: TimelineEntry;
  /**
   * Full reply graph keyed by parent_id. The bar walks the graph recursively
   * so the count + author list match what CommentCard would render in the
   * expanded view (direct-children-only would undercount nested replies).
   */
  repliesByParent: Map<string, TimelineEntry[]>;
  onExpand: () => void;
}

const MAX_NAMED_AUTHORS = 2;

export function ResolvedThreadBar({ entry, repliesByParent, onExpand }: ResolvedThreadBarProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();

  const replies = collectThreadReplies(entry.id, repliesByParent);

  const authorKeys = new Set<string>();
  const authors: Array<{ type: string; id: string }> = [];
  for (const e of [entry, ...replies]) {
    const key = `${e.actor_type}:${e.actor_id}`;
    if (authorKeys.has(key)) continue;
    authorKeys.add(key);
    authors.push({ type: e.actor_type, id: e.actor_id });
  }
  const count = 1 + replies.length;

  let authorsLabel: string;
  if (authors.length <= MAX_NAMED_AUTHORS) {
    authorsLabel = authors.map((a) => getActorName(a.type, a.id)).join(", ");
  } else {
    const named = authors.slice(0, MAX_NAMED_AUTHORS).map((a) => getActorName(a.type, a.id)).join(", ");
    const remaining = authors.length - MAX_NAMED_AUTHORS;
    authorsLabel = t(($) => $.comment.resolve.bar_authors_more, { names: named, count: remaining });
  }

  return (
    <Card className="!py-0 !gap-0 overflow-hidden">
      <button
        type="button"
        onClick={onExpand}
        className="flex w-full items-center justify-between px-4 py-3 text-left hover:bg-muted/50 transition-colors"
      >
        <span className="flex items-center gap-2.5 text-sm text-muted-foreground">
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
