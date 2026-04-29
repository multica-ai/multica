"use client";

import { useMemo, useState } from "react";
import { Hash, Plus } from "lucide-react";
import type { IssueLabel } from "@/shared/types";
import { useWorkspaceLabelsQuery } from "@/features/issues/queries";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "./property-picker";

const DEFAULT_LABEL_COLORS = [
  "#2563eb",
  "#7c3aed",
  "#db2777",
  "#ea580c",
  "#059669",
  "#0891b2",
];

function colorForLabelName(name: string): string {
  let hash = 0;
  for (let index = 0; index < name.length; index += 1) {
    hash = ((hash << 5) - hash) + name.charCodeAt(index);
    hash |= 0;
  }
  return DEFAULT_LABEL_COLORS[Math.abs(hash) % DEFAULT_LABEL_COLORS.length] ?? DEFAULT_LABEL_COLORS[0];
}

export function LabelPicker({
  labels,
  onAdd,
  onRemove,
  align,
}: {
  labels: IssueLabel[];
  onAdd: (input: { labelId?: string; name?: string; color?: string }) => Promise<unknown>;
  onRemove: (labelId: string) => Promise<unknown>;
  align?: "start" | "center" | "end";
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const { data: workspaceLabels = [] } = useWorkspaceLabelsQuery();
  const selectedIds = useMemo(() => new Set(labels.map((label) => label.id)), [labels]);

  const filteredLabels = useMemo(() => {
    const query = filter.trim().toLowerCase();
    return workspaceLabels.filter((label) => {
      if (!query) return true;
      return label.name.toLowerCase().includes(query);
    });
  }, [filter, workspaceLabels]);

  const normalizedFilter = filter.trim().toLowerCase();
  const exactMatch = workspaceLabels.find((label) => label.name.toLowerCase() === normalizedFilter) ?? null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen);
        if (!nextOpen) setFilter("");
      }}
      width="w-72"
      align={align ?? "end"}
      searchable
      searchPlaceholder="Search or create label..."
      onSearchChange={setFilter}
      trigger={labels.length > 0 ? (
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          {labels.map((label) => (
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
      ) : (
        <span className="text-muted-foreground">No labels</span>
      )}
    >
      {filteredLabels.map((label) => {
        const selected = selectedIds.has(label.id);
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
              setOpen(false);
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
            await onAdd({
              name: filter.trim(),
              color: colorForLabelName(filter.trim()),
            });
            setOpen(false);
          }}
        >
          <Plus className="h-3.5 w-3.5 text-muted-foreground" />
          <span>Create “{filter.trim()}”</span>
        </PickerItem>
      ) : null}

      {filteredLabels.length === 0 && (!normalizedFilter || exactMatch) ? <PickerEmpty /> : null}
    </PropertyPicker>
  );
}
