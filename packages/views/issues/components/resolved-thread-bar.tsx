import { CheckCircle2, ChevronRight } from "lucide-react";
import { useActorName } from "@multica/core/workspace/hooks";
import type { TimelineEntry } from "@multica/core/types";
import { useT } from "../../i18n";

interface ResolvedCommentBarProps {
  /** The resolved comment represented by this folded row. */
  entry: TimelineEntry;
  onExpand: () => void;
}

const MAX_NAMED_AUTHORS = 2;

export function ResolvedCommentBar({ entry, onExpand }: ResolvedCommentBarProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();

  const authorKeys = new Set<string>();
  const authors: Array<{ type: string; id: string }> = [];
  const key = `${entry.actor_type}:${entry.actor_id}`;
  if (!authorKeys.has(key)) {
    authorKeys.add(key);
    authors.push({ type: entry.actor_type, id: entry.actor_id });
  }

  let authorsLabel: string;
  if (authors.length <= MAX_NAMED_AUTHORS) {
    authorsLabel = authors.map((a) => getActorName(a.type, a.id)).join(", ");
  } else {
    const named = authors.slice(0, MAX_NAMED_AUTHORS).map((a) => getActorName(a.type, a.id)).join(", ");
    const remaining = authors.length - MAX_NAMED_AUTHORS;
    authorsLabel = t(($) => $.comment.resolve.bar_authors_more, { names: named, count: remaining });
  }

  return (
    <button
      type="button"
      onClick={onExpand}
      className="flex w-full items-center justify-between rounded-md bg-muted/45 px-3 py-2.5 text-left transition-colors hover:bg-muted"
    >
      <span className="flex min-w-0 items-center gap-2.5 text-sm text-muted-foreground">
        <CheckCircle2 className="h-4 w-4 shrink-0" />
        <span className="truncate">
          {t(($) => $.comment.resolve.bar, { count: 1, authors: authorsLabel })}
        </span>
      </span>
      <ChevronRight className="h-3.5 w-3.5 rotate-90 shrink-0 text-muted-foreground" />
    </button>
  );
}
