"use client";

import { ChevronRight } from "lucide-react";
import { Card } from "@multica/ui/components/ui/card";
import type { Chapter } from "./types";

const statusLabel = {
  todo: "Todo",
  in_progress: "In progress",
  done: "Done",
} as const;

export function ChapterHeader({
  chapter,
  open,
  onToggle,
}: {
  chapter: Chapter;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <Card className="!py-0 !gap-0 overflow-hidden">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-center justify-between px-4 py-3 text-left hover:bg-muted/50 transition-colors"
      >
        <span className="flex min-w-0 items-center gap-2.5">
          <ChevronRight className={`h-3.5 w-3.5 shrink-0 transition-transform ${open ? "rotate-90" : ""}`} />
          <span className="truncate text-sm font-medium">{chapter.name}</span>
        </span>
        <span className="rounded-full border px-2 py-0.5 text-xs text-muted-foreground">
          {statusLabel[chapter.status]}
        </span>
      </button>
    </Card>
  );
}
