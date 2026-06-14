"use client";

import { useMemo, useState } from "react";
import { ChevronDown, Hash, Plus } from "lucide-react";
import type { TimeEntryLabel } from "@/shared/types";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "@/features/issues/components/pickers/property-picker";

interface TimeEntryLabelPickerProps {
  labels: TimeEntryLabel[];
  selectedIds: string[];
  onAdd: (input: { labelId?: string; name?: string }) => Promise<void>;
  onRemove: (labelId: string) => Promise<void>;
  align?: "start" | "center" | "end";
}

/**
 * TimeEntryLabelPicker provides searchable multi-select for workspace time-entry labels.
 * It supports selecting existing labels and quick-creating by name.
 */
export function TimeEntryLabelPicker({
  labels,
  selectedIds,
  onAdd,
  onRemove,
  align,
}: TimeEntryLabelPickerProps) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");

  const selectedIdSet = useMemo(() => new Set(selectedIds), [selectedIds]);

  const filteredLabels = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return labels;
    return labels.filter((label) => label.name.toLowerCase().includes(query));
  }, [labels, filter]);

  const sortedFilteredLabels = useMemo(() => {
    return [...filteredLabels].sort((a, b) => {
      const aSelected = selectedIdSet.has(a.id) ? 0 : 1;
      const bSelected = selectedIdSet.has(b.id) ? 0 : 1;
      if (aSelected !== bSelected) return aSelected - bSelected;
      return a.name.localeCompare(b.name);
    });
  }, [filteredLabels, selectedIdSet]);

  const normalizedFilter = filter.trim().toLowerCase();
  const exactMatch = labels.find((label) => label.name.toLowerCase() === normalizedFilter) ?? null;
  const selectedLabels = labels.filter((label) => selectedIdSet.has(label.id));

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) {
          setFilter("");
        }
      }}
      width="w-72"
      align={align ?? "end"}
      searchable
      searchPlaceholder="Search or create label..."
      onSearchChange={setFilter}
      triggerRender={
        <button
          type="button"
          className="flex min-h-9 w-full items-center justify-between gap-2 rounded-md border bg-background px-3 py-2 text-left text-sm transition-colors hover:bg-accent/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
        />
      }
      trigger={selectedLabels.length > 0 ? (
        <>
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
            {selectedLabels.map((label) => (
              <span
                key={label.id}
                className="inline-flex max-w-full items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
                style={{ borderColor: label.color, color: label.color }}
              >
                <Hash className="h-3 w-3 shrink-0" />
                <span className="truncate">{label.name}</span>
              </span>
            ))}
          </div>
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
        </>
      ) : (
        <>
          <span className="text-muted-foreground">No labels</span>
          <ChevronDown className="size-4 shrink-0 text-muted-foreground" />
        </>
      )}
    >
      {sortedFilteredLabels.map((label) => {
        const selected = selectedIdSet.has(label.id);
        return (
          <PickerItem
            key={label.id}
            selected={selected}
            onClick={async () => {
              if (selected) {
                await onRemove(label.id);
              } else {
                await onAdd({ labelId: label.id });
              }
            }}
          >
            <span
              className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
              style={{ borderColor: label.color, color: label.color }}
            >
              <Hash className="h-3 w-3 shrink-0" />
              <span>{label.name}</span>
            </span>
          </PickerItem>
        );
      })}

      {normalizedFilter && !exactMatch ? (
        <PickerItem
          selected={false}
          onClick={async () => {
            const name = filter.trim();
            if (!name) return;
            await onAdd({ name });
            setFilter("");
          }}
        >
          <Plus className="h-3.5 w-3.5 text-muted-foreground" />
          <span>Create "{filter.trim()}"</span>
        </PickerItem>
      ) : null}

      {sortedFilteredLabels.length === 0 && (!normalizedFilter || exactMatch) ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}
