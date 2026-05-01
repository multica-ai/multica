"use client";

import { useMemo, useState } from "react";
import { Hash, Plus, ChevronRight } from "lucide-react";
import type { IssueLabel } from "@/shared/types";
import { useWorkspaceLabelsQuery } from "@/features/issues/queries";
import { LABEL_PRESET_COLORS } from "@/features/issues/components/label-color-picker";
import {
  PropertyPicker,
  PickerEmpty,
  PickerItem,
} from "./property-picker";

const DEFAULT_NEW_COLOR = LABEL_PRESET_COLORS[5]?.hex ?? "#3b82f6"; // Blue

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
  const [pendingCreate, setPendingCreate] = useState<string | null>(null);
  const [pendingColor, setPendingColor] = useState(DEFAULT_NEW_COLOR);
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
        if (!nextOpen) {
          setFilter("");
          setPendingCreate(null);
          setPendingColor(DEFAULT_NEW_COLOR);
        }
      }}
      width="w-72"
      align={align ?? "end"}
      searchable={!pendingCreate}
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
      {/* Color selection step when creating a new label */}
      {pendingCreate ? (
        <div className="p-2 space-y-2">
          <div className="flex items-center gap-2 mb-1">
            <span
              className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
              style={{ borderColor: pendingColor, color: pendingColor }}
            >
              <Hash className="h-3 w-3" />
              {pendingCreate}
            </span>
            <span className="text-xs text-muted-foreground">Pick a color</span>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {LABEL_PRESET_COLORS.map(({ hex, label }) => (
              <button
                key={hex}
                type="button"
                title={label}
                onClick={() => setPendingColor(hex)}
                className={`h-5 w-5 rounded-full border-2 transition-transform hover:scale-110 ${
                  pendingColor === hex ? "border-foreground scale-110" : "border-transparent"
                }`}
                style={{ backgroundColor: hex }}
              />
            ))}
          </div>
          <div className="flex gap-1.5 pt-1">
            <button
              type="button"
              onClick={async () => {
                await onAdd({ name: pendingCreate, color: pendingColor });
                setPendingCreate(null);
                setFilter("");
                setOpen(false);
              }}
              className="flex-1 rounded bg-primary px-2 py-1 text-xs font-medium text-primary-foreground hover:bg-primary/90"
            >
              Create
            </button>
            <button
              type="button"
              onClick={() => { setPendingCreate(null); setPendingColor(DEFAULT_NEW_COLOR); }}
              className="rounded border px-2 py-1 text-xs hover:bg-accent"
            >
              Back
            </button>
          </div>
        </div>
      ) : (
        <>
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
              onClick={() => {
                setPendingCreate(filter.trim());
                setPendingColor(DEFAULT_NEW_COLOR);
              }}
            >
              <Plus className="h-3.5 w-3.5 text-muted-foreground" />
              <span>Create "{filter.trim()}"</span>
              <ChevronRight className="ml-auto h-3.5 w-3.5 text-muted-foreground" />
            </PickerItem>
          ) : null}

          {filteredLabels.length === 0 && (!normalizedFilter || exactMatch) ? <PickerEmpty /> : null}
        </>
      )}
    </PropertyPicker>
  );
}
