"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import { useCerebroTasksStore } from "../../core/store";

interface TasksPaginationProps {
  total: number;
  loadedCount: number;
}

export function TasksPagination({ total, loadedCount }: TasksPaginationProps) {
  const limit = useCerebroTasksStore((s) => s.limit);
  const offset = useCerebroTasksStore((s) => s.offset);
  const nextPage = useCerebroTasksStore((s) => s.nextPage);
  const prevPage = useCerebroTasksStore((s) => s.prevPage);

  const from = total === 0 ? 0 : offset + 1;
  const to = offset + loadedCount;
  const hasPrev = offset > 0;
  const hasNext = offset + limit < total;

  return (
    <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
      <span>
        {total === 0
          ? "0 tasks"
          : `${from.toLocaleString("da-DK")}–${to.toLocaleString("da-DK")} af ${total.toLocaleString("da-DK")}`}
      </span>
      <div className="flex items-center gap-1">
        <PageButton onClick={prevPage} disabled={!hasPrev} ariaLabel="Forrige side">
          <ChevronLeft className="size-3.5" />
          <span>Forrige</span>
        </PageButton>
        <PageButton onClick={nextPage} disabled={!hasNext} ariaLabel="Næste side">
          <span>Næste</span>
          <ChevronRight className="size-3.5" />
        </PageButton>
      </div>
    </div>
  );
}

interface PageButtonProps {
  onClick: () => void;
  disabled: boolean;
  ariaLabel: string;
  children: React.ReactNode;
}

function PageButton({ onClick, disabled, ariaLabel, children }: PageButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={ariaLabel}
      className="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-1 text-xs text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
    >
      {children}
    </button>
  );
}
