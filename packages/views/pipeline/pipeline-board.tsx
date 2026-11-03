"use client";

import type { Squad } from "@multica/core/types";
import { useT } from "../i18n";
import type { StageColumn } from "./pipeline-filters";
import { PipelineCard } from "./pipeline-card";

/** One column per stage, in pipeline order. Cards are placed by the server's stage, never by drag. */
export function PipelineBoard({
  columns,
  squads,
  selectedId,
  onSelect,
}: {
  columns: readonly StageColumn[];
  squads: readonly Squad[];
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("pipeline");
  const squadName = (id: string | null) => (id ? (squads.find((squad) => squad.id === id)?.name ?? null) : null);
  return (
    <div className="flex min-h-0 flex-1 items-start gap-3 overflow-x-auto px-4 pb-4">
      {columns.map((column) => {
        const title = column.stage ? column.stage.name || column.stage.key : t(($) => $.board.unknown_stage);
        const headingId = `pipeline-stage-${column.key}`;
        return (
          <section key={column.key} aria-labelledby={headingId} className="flex w-64 shrink-0 flex-col gap-2">
            <header className="flex items-baseline justify-between gap-2 px-1">
              <h2 id={headingId} className="min-w-0 truncate text-body font-medium">
                {title}
              </h2>
              <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
                {column.initiatives.length}
              </span>
            </header>
            {column.initiatives.length > 0 ? (
              <ul className="flex flex-col gap-2">
                {column.initiatives.map((initiative) => (
                  <li key={initiative.id}>
                    <PipelineCard
                      initiative={initiative}
                      squadName={squadName(initiative.owner_squad_id)}
                      selected={initiative.id === selectedId}
                      onSelect={() => onSelect(initiative.id)}
                    />
                  </li>
                ))}
              </ul>
            ) : (
              <p className="rounded-lg border border-dashed px-3 py-6 text-center text-caption text-muted-foreground">
                {t(($) => $.board.column_empty)}
              </p>
            )}
          </section>
        );
      })}
    </div>
  );
}
