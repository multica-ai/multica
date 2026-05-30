"use client";

import { useMemo, useState } from "react";
import { Check, Hash, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useWorkspaceLabelsQuery } from "@/features/issues/queries";
import { cn } from "@/lib/utils";
import type { LabelFilterMode } from "@/features/issues/stores/view-store";
import type { IssueLabel } from "@/shared/types";

interface IssueLabelFilterProps {
  selectedIds: string[];
  mode: LabelFilterMode;
  onToggle: (labelId: string) => void;
  onModeChange: (mode: LabelFilterMode) => void;
  onClear: () => void;
}

function sortLabels(labels: IssueLabel[], selectedIds: string[], query: string) {
  // 搜索结果优先展示已选标签，减少长列表下重复滚动的成本。
  const normalizedQuery = query.trim().toLowerCase();
  const selectedIdSet = new Set(selectedIds);

  return labels
    .filter((label) => normalizedQuery.length === 0 || label.name.toLowerCase().includes(normalizedQuery))
    .sort((left, right) => {
      const selectedDelta = Number(selectedIdSet.has(right.id)) - Number(selectedIdSet.has(left.id));
      if (selectedDelta !== 0) return selectedDelta;
      return left.name.localeCompare(right.name);
    });
}

export function IssueLabelFilter({
  selectedIds,
  mode,
  onToggle,
  onModeChange,
  onClear,
}: IssueLabelFilterProps) {
  const { data: labels = [], isLoading } = useWorkspaceLabelsQuery();
  const [query, setQuery] = useState("");
  const filteredLabels = useMemo(() => sortLabels(labels, selectedIds, query), [labels, query, selectedIds]);
  const selectedCount = selectedIds.length;

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            className={cn("gap-2", selectedCount > 0 && "border-primary/50 text-foreground")}
          >
            <Hash className="h-3.5 w-3.5" />
            <span>{selectedCount > 0 ? `Labels (${selectedCount})` : "Labels"}</span>
          </Button>
        }
      />
      <PopoverContent align="start" className="w-80 p-3">
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div>
              <p className="text-sm font-medium">Filter by labels</p>
              <p className="text-xs text-muted-foreground">Select one or more labels for the current issue list.</p>
            </div>
            {selectedCount > 0 ? (
              <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={onClear}>
                <X className="mr-1 h-3 w-3" />
                Clear
              </Button>
            ) : null}
          </div>

          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Search labels"
              className="h-8 pl-8"
            />
          </div>

          <div className="flex items-center gap-1 rounded-md border p-1">
            <Button
              type="button"
              variant={mode === "any" ? "secondary" : "ghost"}
              size="sm"
              className="h-7 flex-1"
              onClick={() => onModeChange("any")}
              disabled={selectedCount === 0}
            >
              Match any
            </Button>
            <Button
              type="button"
              variant={mode === "all" ? "secondary" : "ghost"}
              size="sm"
              className="h-7 flex-1"
              onClick={() => onModeChange("all")}
              disabled={selectedCount === 0}
            >
              Match all
            </Button>
          </div>

          <div className="max-h-64 overflow-y-auto rounded-md border">
            {isLoading ? (
              <div className="p-3 text-sm text-muted-foreground">Loading labels…</div>
            ) : filteredLabels.length === 0 ? (
              <div className="p-3 text-sm text-muted-foreground">
                {labels.length === 0 ? "No labels available in this workspace." : "No labels match your search."}
              </div>
            ) : (
              <div className="divide-y">
                {filteredLabels.map((label) => {
                  const selected = selectedIds.includes(label.id);

                  return (
                    <button
                      key={label.id}
                      type="button"
                      onClick={() => onToggle(label.id)}
                      className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left transition-colors hover:bg-muted/50"
                    >
                      <span
                        className="inline-flex h-5 items-center rounded-full border px-2 text-[11px] font-medium"
                        style={{ borderColor: label.color, color: label.color }}
                      >
                        {label.name}
                      </span>
                      <span className="flex h-4 w-4 items-center justify-center text-primary">
                        {selected ? <Check className="h-3.5 w-3.5" /> : null}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
