"use client";

import type { ReactNode } from "react";
import { FileText } from "lucide-react";
import type { RuntimeLocalSkillSummary } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";

export function RuntimeLocalSkillRow({
  skill,
  selected = false,
  onSelect,
  action,
}: {
  skill: RuntimeLocalSkillSummary;
  selected?: boolean;
  onSelect?: () => void;
  action?: ReactNode;
}) {
  const content = (
    <>
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-muted">
        <FileText className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{skill.name}</span>
          <Badge variant="secondary">{skill.provider}</Badge>
        </div>
        {skill.description && (
          <p className="mt-1 text-xs text-muted-foreground">{skill.description}</p>
        )}
        <p className="mt-1 truncate font-mono text-[11px] text-muted-foreground">
          {skill.source_path}
        </p>
      </div>
      {action ?? (
        <Badge variant="outline">
          {skill.file_count} file{skill.file_count === 1 ? "" : "s"}
        </Badge>
      )}
    </>
  );

  if (onSelect) {
    return (
      <button
        type="button"
        onClick={onSelect}
        className={`flex w-full items-start gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
          selected ? "border-primary bg-primary/5" : "hover:bg-accent/50"
        }`}
      >
        {content}
      </button>
    );
  }

  return <div className="flex items-start gap-3 rounded-lg border px-4 py-3">{content}</div>;
}
