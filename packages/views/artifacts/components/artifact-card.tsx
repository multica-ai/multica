"use client";

import { cn } from "@multica/ui/lib/utils";
import { Badge } from "@multica/ui/components/ui/badge";
import type { Artifact } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { KindIcon, KIND_LABELS } from "./kind-icon";

function excerpt(body: string, maxLen = 220): string {
  const stripped = body
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#*_`>~|-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (stripped.length <= maxLen) return stripped;
  return stripped.slice(0, maxLen).trimEnd() + "…";
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function ArtifactCard({
  artifact,
  onClick,
  className,
}: {
  artifact: Artifact;
  onClick?: (artifact: Artifact) => void;
  className?: string;
}) {
  const { getActorName } = useActorName();
  const authorName = getActorName(artifact.author_type, artifact.author_id);
  const Wrapper = onClick ? "button" : "div";

  return (
    <Wrapper
      type={onClick ? "button" : undefined}
      onClick={onClick ? () => onClick(artifact) : undefined}
      className={cn(
        "flex w-full flex-col gap-2 rounded-lg border border-border bg-card px-4 py-3 text-left transition-colors",
        onClick && "hover:bg-muted/40 focus:outline-none focus:ring-2 focus:ring-ring",
        className,
      )}
    >
      <div className="flex items-center gap-2">
        <KindIcon kind={artifact.kind} className="size-4 shrink-0 text-muted-foreground" />
        <Badge variant="secondary" className="text-xs font-normal">
          {KIND_LABELS[artifact.kind]}
        </Badge>
        <span className="ml-auto truncate text-xs text-muted-foreground">
          {formatDate(artifact.updated_at)}
        </span>
      </div>
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{artifact.title}</p>
        {artifact.body && (
          <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
            {excerpt(artifact.body)}
          </p>
        )}
      </div>
      <div className="flex items-center gap-1 text-xs text-muted-foreground">
        <span>{authorName}</span>
        {artifact.author_type === "agent" && (
          <Badge variant="outline" className="ml-1 px-1 py-0 text-[10px]">
            agent
          </Badge>
        )}
      </div>
    </Wrapper>
  );
}
