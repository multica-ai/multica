"use client";

import { ChevronDown, X } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { useT } from "../../i18n";
import type { DagBusSelectionRow } from "./dag-scene-flow";

/** One original dependency running through a clicked bus segment. */
export type DagBusSelectionItem = DagBusSelectionRow;

/** The selection zone's bus state (OL-182's aggregate dropdown, adapted to
 *  bus painting): a summary button listing every original dependency as
 *  "source → target + locate". Redundant retained edges list like any other. */
export function DagBusSelection({
  items,
  onLocate,
  onOpenIssue,
}: {
  items: DagBusSelectionItem[];
  onLocate: (dependencyId: string) => void;
  onOpenIssue: (issueId: string) => void;
}) {
  const { t } = useT("dag");
  const storeApi = useViewStoreApi();
  return (
    <div data-dag-zone className="flex min-w-0 flex-1 items-center gap-1">
      <Popover>
        <PopoverTrigger
          render={<Button size="sm" variant="ghost" className="gap-1" />}
        >
          <span data-dag-label>{t(($) => $.edge_aggregate_summary, { count: items.length })}</span>
          <ChevronDown className="size-3.5" />
        </PopoverTrigger>
        <PopoverContent align="start" className="w-[26rem] max-w-[80vw] p-2">
          <ul className="flex max-h-72 flex-col gap-0.5 overflow-y-auto">
            {items.map((item) => (
              <li
                key={item.id}
                className="flex items-center gap-1.5 rounded-md px-1 py-0.5 text-caption hover:bg-muted/60"
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 truncate text-left text-muted-foreground hover:text-foreground"
                  onClick={() => onOpenIssue(item.sourceId)}
                  title={item.sourceTitle}
                >
                  {item.sourceLabel}
                </button>
                <span aria-hidden className="shrink-0 text-faint-foreground">
                  →
                </span>
                <button
                  type="button"
                  className="min-w-0 flex-1 truncate text-left text-muted-foreground hover:text-foreground"
                  onClick={() => onOpenIssue(item.targetId)}
                  title={item.targetTitle}
                >
                  {item.targetLabel}
                </button>
                <Button
                  size="sm"
                  variant="ghost"
                  className="shrink-0"
                  onClick={() => onLocate(item.id)}
                >
                  {t(($) => $.edge_locate)}
                </Button>
              </li>
            ))}
          </ul>
        </PopoverContent>
      </Popover>
      <span className="min-w-0 flex-1" />
      <Button
        size="icon-sm"
        variant="ghost"
        className="shrink-0"
        aria-label={t(($) => $.clear_selection)}
        onClick={() => storeApi.getState().setDagSelectedDependencyIds(null)}
      >
        <X className="size-3.5" />
      </Button>
    </div>
  );
}
