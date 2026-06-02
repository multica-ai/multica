import type { ProjectUpdate } from "@multica/core/types/project";
import { ActorAvatar } from "../../common/actor-avatar";
import { useTimeAgo } from "../../i18n/use-time-ago";
import { HealthPill } from "./health-pill";

interface ProjectUpdateCardProps {
  update: ProjectUpdate;
  canModerate?: boolean;
  onDelete?: (updateId: string) => void;
}

export function ProjectUpdateCard({ update, canModerate, onDelete }: ProjectUpdateCardProps) {
  const timeAgo = useTimeAgo();
  return (
    <article className="rounded-lg border border-border bg-card p-4">
      <header className="flex items-center gap-2">
        <ActorAvatar actorType={update.author_type} actorId={update.author_id} size={20} enableHoverCard />
        <span className="text-xs text-muted-foreground">{timeAgo(update.created_at)}</span>
        <span className="ml-auto">
          <HealthPill health={update.health} />
        </span>
        {canModerate && onDelete && (
          <button
            type="button"
            onClick={() => onDelete(update.id)}
            className="text-xs text-muted-foreground hover:text-destructive"
            aria-label="Delete update"
          >
            Delete
          </button>
        )}
      </header>
      <div className="mt-3 whitespace-pre-wrap text-sm text-foreground">{update.body}</div>
    </article>
  );
}
