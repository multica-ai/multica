"use client";

// Cerebro workflow v2a (FIR-1550) — create/edit a status model.
//
// One model = an ordered list of custom statuses, each bound to one of the 7
// upstream base statuses. The editor enforces one custom status per base
// status (the base select disables already-used bases), which keeps v2a out
// of duplicate-base/sub-status territory (FIR-1553).

import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { BASE_STATUSES } from "@multica/cerebro-status-models/core";
import type {
  CerebroStatusModel,
  StatusModelWriteInput,
} from "@multica/cerebro-status-models/core";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";

const MIN_STATUSES = 3;

const BASE_STATUS_LABELS: Record<string, string> = {
  backlog: "Backlog",
  todo: "To-do",
  in_progress: "I gang",
  in_review: "I review",
  done: "Færdig",
  blocked: "Blokeret",
  cancelled: "Annulleret",
};

const PRESET_COLORS = [
  "#64748b",
  "#6366f1",
  "#0ea5e9",
  "#10b981",
  "#f59e0b",
  "#ef4444",
  "#ec4899",
  "#8b5cf6",
];

interface EditorRow {
  uid: string;
  label: string;
  color: string;
  base_status: string;
}

function seedRows(model?: CerebroStatusModel): EditorRow[] {
  if (model && model.statuses.length > 0) {
    return [...model.statuses]
      .sort((a, b) => a.position - b.position)
      .map((s, i) => ({
        uid: `${s.key}-${i}`,
        label: s.label,
        color: s.color || PRESET_COLORS[i % PRESET_COLORS.length]!,
        base_status: s.base_status,
      }));
  }
  // A fresh model starts on the most common plan-first shape.
  return [
    { uid: "r1", label: "Plan", color: PRESET_COLORS[1]!, base_status: "todo" },
    { uid: "r2", label: "I gang", color: PRESET_COLORS[4]!, base_status: "in_progress" },
    { uid: "r3", label: "Færdig", color: PRESET_COLORS[3]!, base_status: "done" },
  ];
}

export function StatusModelEditor({
  open,
  onOpenChange,
  model,
  onSubmit,
  saving,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** Undefined → create mode; present → edit mode. */
  model?: CerebroStatusModel;
  onSubmit: (input: StatusModelWriteInput) => Promise<unknown>;
  saving: boolean;
}) {
  const [name, setName] = useState(model?.name ?? "");
  const [description, setDescription] = useState(model?.description ?? "");
  const [rows, setRows] = useState<EditorRow[]>(() => seedRows(model));

  const usedBases = useMemo(
    () => new Set(rows.map((r) => r.base_status)),
    [rows],
  );
  const availableBase = BASE_STATUSES.find((b) => !usedBases.has(b));

  const updateRow = (uid: string, patch: Partial<EditorRow>) =>
    setRows((prev) => prev.map((r) => (r.uid === uid ? { ...r, ...patch } : r)));

  const moveRow = (index: number, delta: number) =>
    setRows((prev) => {
      const next = [...prev];
      const target = index + delta;
      if (target < 0 || target >= next.length) return prev;
      [next[index], next[target]] = [next[target]!, next[index]!];
      return next;
    });

  const addRow = () => {
    if (!availableBase) return;
    setRows((prev) => [
      ...prev,
      {
        uid: `r-${Date.now()}`,
        label: BASE_STATUS_LABELS[availableBase] ?? availableBase,
        color: PRESET_COLORS[prev.length % PRESET_COLORS.length]!,
        base_status: availableBase,
      },
    ]);
  };

  const removeRow = (uid: string) =>
    setRows((prev) => prev.filter((r) => r.uid !== uid));

  const canSubmit =
    name.trim().length > 0 &&
    rows.length >= MIN_STATUSES &&
    rows.every((r) => r.label.trim().length > 0);

  const handleSubmit = async () => {
    if (!canSubmit) {
      toast.error(`En model kræver et navn og mindst ${MIN_STATUSES} statusser.`);
      return;
    }
    const input: StatusModelWriteInput = {
      name: name.trim(),
      description: description.trim() || undefined,
      // base_status is unique per row, so it doubles as a stable status key.
      statuses: rows.map((r) => ({
        key: r.base_status,
        label: r.label.trim(),
        color: r.color,
        base_status: r.base_status,
      })),
    };
    try {
      await onSubmit(input);
      onOpenChange(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Kunne ikke gemme modellen.");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{model ? "Rediger statusmodel" : "Ny statusmodel"}</DialogTitle>
          <DialogDescription>
            Byg en pipeline af egne statusser. Hver status knyttes til en
            grundtilstand, så indbakke, rapporter og agenter forstår den uændret.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="sm-name">Navn</Label>
            <Input
              id="sm-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="fx Plan-først pipeline"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sm-desc">Beskrivelse (valgfri)</Label>
            <Textarea
              id="sm-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
            />
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label>Statusser</Label>
              <span className="text-xs text-muted-foreground">
                {rows.length}/{BASE_STATUSES.length} · min. {MIN_STATUSES}
              </span>
            </div>

            <div className="space-y-2">
              {rows.map((row, index) => (
                <div
                  key={row.uid}
                  className="flex items-center gap-2 rounded-lg border p-2"
                >
                  <div className="flex flex-col">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={index === 0}
                      onClick={() => moveRow(index, -1)}
                    >
                      <ArrowUp className="size-3.5" />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={index === rows.length - 1}
                      onClick={() => moveRow(index, 1)}
                    >
                      <ArrowDown className="size-3.5" />
                    </Button>
                  </div>

                  <input
                    type="color"
                    aria-label="Farve"
                    value={row.color}
                    onChange={(e) => updateRow(row.uid, { color: e.target.value })}
                    className="size-7 shrink-0 cursor-pointer rounded-md border bg-transparent p-0"
                  />

                  <Input
                    value={row.label}
                    onChange={(e) => updateRow(row.uid, { label: e.target.value })}
                    placeholder="Status-navn"
                    className="flex-1"
                  />

                  <NativeSelect
                    value={row.base_status}
                    onChange={(e) =>
                      updateRow(row.uid, { base_status: e.target.value })
                    }
                    className="w-32 shrink-0"
                  >
                    {BASE_STATUSES.map((b) => (
                      <NativeSelectOption
                        key={b}
                        value={b}
                        disabled={b !== row.base_status && usedBases.has(b)}
                      >
                        {BASE_STATUS_LABELS[b] ?? b}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>

                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    disabled={rows.length <= MIN_STATUSES}
                    onClick={() => removeRow(row.uid)}
                  >
                    <Trash2 className="size-3.5" />
                  </Button>
                </div>
              ))}
            </div>

            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={addRow}
              disabled={!availableBase}
            >
              <Plus className="mr-1.5 size-3.5" />
              Tilføj status
            </Button>
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            <X className="mr-1.5 size-3.5" />
            Annullér
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit || saving}>
            {saving ? "Gemmer…" : model ? "Gem ændringer" : "Opret model"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
