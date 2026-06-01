"use client";

// Cerebro workflow v2b (FIR-1550) — create/edit a status model.
//
// One model = an ordered list of custom statuses. Each custom status is bound
// to one of the 7 upstream base statuses and is a real sub-status under that
// base (v2b promise): two custom statuses CAN share the same base_status,
// and the board renders them as distinct columns. Per-status description is
// required — that is what agents read to understand what the status means.
// Triggers (assign-to, fire-agent) are optional automation hooks; the
// trigger engine that fires them ships in the v2b follow-up PR.

import { useEffect, useMemo, useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2, X } from "lucide-react";
import { toast } from "sonner";
import { BASE_STATUSES } from "@multica/cerebro-status-models/core";
import type {
  CerebroStatusModel,
  StatusModelWriteInput,
  StatusTriggers,
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
  /** Stable status key — derived from label on first save when blank. */
  key: string;
  label: string;
  color: string;
  base_status: string;
  description: string;
  triggers: EditorTriggers;
}

interface EditorTriggers {
  assign_enabled: boolean;
  assign_to_type: "member" | "agent";
  assign_to_id: string;
  fire_enabled: boolean;
  fire_agent_id: string;
}

const EMPTY_TRIGGERS: EditorTriggers = {
  assign_enabled: false,
  assign_to_type: "member",
  assign_to_id: "",
  fire_enabled: false,
  fire_agent_id: "",
};

function triggersFromEntry(t: StatusTriggers | undefined): EditorTriggers {
  if (!t) return { ...EMPTY_TRIGGERS };
  return {
    assign_enabled: Boolean(t.assign_to_id && t.assign_to_type),
    assign_to_type: (t.assign_to_type as "member" | "agent") ?? "member",
    assign_to_id: t.assign_to_id ?? "",
    fire_enabled: Boolean(t.fire_agent_id),
    fire_agent_id: t.fire_agent_id ?? "",
  };
}

function triggersToPayload(t: EditorTriggers): StatusTriggers | undefined {
  const payload: StatusTriggers = {};
  if (t.assign_enabled && t.assign_to_id.trim()) {
    payload.assign_to_id = t.assign_to_id.trim();
    payload.assign_to_type = t.assign_to_type;
  }
  if (t.fire_enabled && t.fire_agent_id.trim()) {
    payload.fire_agent_id = t.fire_agent_id.trim();
  }
  return Object.keys(payload).length > 0 ? payload : undefined;
}

function seedRows(model?: CerebroStatusModel): EditorRow[] {
  if (model && model.statuses.length > 0) {
    return [...model.statuses]
      .sort((a, b) => a.position - b.position)
      .map((s, i) => ({
        uid: `${s.key}-${i}`,
        key: s.key,
        label: s.label,
        color: s.color || PRESET_COLORS[i % PRESET_COLORS.length]!,
        base_status: s.base_status,
        description: s.description ?? "",
        triggers: triggersFromEntry(s.triggers),
      }));
  }
  // A fresh model starts on the most common plan-first shape, but with
  // empty descriptions so the editor still forces the admin to write them.
  return [
    {
      uid: "r1",
      key: "plan",
      label: "Plan",
      color: PRESET_COLORS[1]!,
      base_status: "todo",
      description: "",
      triggers: { ...EMPTY_TRIGGERS },
    },
    {
      uid: "r2",
      key: "i_gang",
      label: "I gang",
      color: PRESET_COLORS[4]!,
      base_status: "in_progress",
      description: "",
      triggers: { ...EMPTY_TRIGGERS },
    },
    {
      uid: "r3",
      key: "faerdig",
      label: "Færdig",
      color: PRESET_COLORS[3]!,
      base_status: "done",
      description: "",
      triggers: { ...EMPTY_TRIGGERS },
    },
  ];
}

// Slugify a label into a stable status key. Same input → same key, so
// edits preserve the per-issue pin (which points at the key, not the label).
function deriveKey(label: string, fallbackIndex: number): string {
  const slug = label
    .toLowerCase()
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return slug || `status_${fallbackIndex + 1}`;
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

  // Re-seed when the dialog is reopened with a different model (or none).
  // Without this the editor re-mounts only on open/close, so switching
  // between models in quick succession would show stale state.
  useEffect(() => {
    if (!open) return;
    setName(model?.name ?? "");
    setDescription(model?.description ?? "");
    setRows(seedRows(model));
  }, [open, model]);

  // v2b: bases CAN be reused, so we no longer disable used ones. Just track
  // them for the "tilføj status"-default base picker.
  const usedBases = useMemo(
    () => new Set(rows.map((r) => r.base_status)),
    [rows],
  );
  const defaultNewBase = BASE_STATUSES.find((b) => !usedBases.has(b)) ?? "todo";

  const updateRow = (uid: string, patch: Partial<EditorRow>) =>
    setRows((prev) => prev.map((r) => (r.uid === uid ? { ...r, ...patch } : r)));

  const updateTriggers = (uid: string, patch: Partial<EditorTriggers>) =>
    setRows((prev) =>
      prev.map((r) => (r.uid === uid ? { ...r, triggers: { ...r.triggers, ...patch } } : r)),
    );

  const moveRow = (index: number, delta: number) =>
    setRows((prev) => {
      const next = [...prev];
      const target = index + delta;
      if (target < 0 || target >= next.length) return prev;
      [next[index], next[target]] = [next[target]!, next[index]!];
      return next;
    });

  const addRow = () => {
    setRows((prev) => [
      ...prev,
      {
        uid: `r-${prev.length}-${prev.length}`,
        key: "",
        label: BASE_STATUS_LABELS[defaultNewBase] ?? defaultNewBase,
        color: PRESET_COLORS[prev.length % PRESET_COLORS.length]!,
        base_status: defaultNewBase,
        description: "",
        triggers: { ...EMPTY_TRIGGERS },
      },
    ]);
  };

  const removeRow = (uid: string) =>
    setRows((prev) => prev.filter((r) => r.uid !== uid));

  const canSubmit =
    name.trim().length > 0 &&
    rows.length >= MIN_STATUSES &&
    rows.every(
      (r) => r.label.trim().length > 0 && r.description.trim().length > 0,
    );

  const handleSubmit = async () => {
    if (!canSubmit) {
      toast.error(
        `En model kræver et navn, mindst ${MIN_STATUSES} statusser, og en beskrivelse på hver status (agenter læser dem).`,
      );
      return;
    }
    // Derive unique keys: prefer the existing key, else slugify the label,
    // else add a numeric suffix on collision so two "Plan-review" rows
    // become "plan_review" and "plan_review_2".
    const usedKeys = new Set<string>();
    const statuses = rows.map((r, i) => {
      let key = r.key.trim() || deriveKey(r.label, i);
      if (usedKeys.has(key)) {
        let n = 2;
        while (usedKeys.has(`${key}_${n}`)) n++;
        key = `${key}_${n}`;
      }
      usedKeys.add(key);
      const triggers = triggersToPayload(r.triggers);
      const out: StatusModelWriteInput["statuses"][number] = {
        key,
        label: r.label.trim(),
        color: r.color,
        base_status: r.base_status,
        description: r.description.trim(),
      };
      if (triggers) out.triggers = triggers;
      return out;
    });
    const input: StatusModelWriteInput = {
      name: name.trim(),
      description: description.trim() || undefined,
      statuses,
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
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{model ? "Rediger statusmodel" : "Ny statusmodel"}</DialogTitle>
          <DialogDescription>
            Byg en pipeline af egne statusser som lever under de 7 grundtilstande.
            To statusser kan dele samme grundtilstand — de bliver til separate
            kolonner på boardet. Beskrivelsen er det agenter læser for at vide
            hvad statussen betyder.
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
                {rows.length} · min. {MIN_STATUSES}
              </span>
            </div>

            <div className="space-y-2">
              {rows.map((row, index) => (
                <div
                  key={row.uid}
                  className="rounded-lg border p-3 space-y-2"
                >
                  <div className="flex items-center gap-2">
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
                      className="w-36 shrink-0"
                    >
                      {BASE_STATUSES.map((b) => (
                        <NativeSelectOption key={b} value={b}>
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

                  <div className="space-y-1.5">
                    <Label
                      htmlFor={`desc-${row.uid}`}
                      className="text-xs text-muted-foreground"
                    >
                      Beskrivelse — agenter læser denne
                    </Label>
                    <Textarea
                      id={`desc-${row.uid}`}
                      value={row.description}
                      onChange={(e) =>
                        updateRow(row.uid, { description: e.target.value })
                      }
                      rows={2}
                      placeholder="fx Issue er klar til review fra Tine — agent må sætte sig selv på"
                    />
                  </div>

                  <details className="text-sm">
                    <summary className="cursor-pointer text-xs text-muted-foreground">
                      Automatisering — valgfri
                    </summary>
                    <div className="mt-2 space-y-2 pl-2">
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`assign-${row.uid}`}
                          checked={row.triggers.assign_enabled}
                          onChange={(e) =>
                            updateTriggers(row.uid, {
                              assign_enabled: e.target.checked,
                            })
                          }
                        />
                        <Label
                          htmlFor={`assign-${row.uid}`}
                          className="text-xs font-normal"
                        >
                          Tildel automatisk når issue lander her
                        </Label>
                      </div>
                      {row.triggers.assign_enabled && (
                        <div className="flex items-center gap-2 pl-6">
                          <NativeSelect
                            value={row.triggers.assign_to_type}
                            onChange={(e) =>
                              updateTriggers(row.uid, {
                                assign_to_type: e.target.value as "member" | "agent",
                              })
                            }
                            className="w-28"
                          >
                            <NativeSelectOption value="member">
                              Person
                            </NativeSelectOption>
                            <NativeSelectOption value="agent">
                              Agent
                            </NativeSelectOption>
                          </NativeSelect>
                          <Input
                            value={row.triggers.assign_to_id}
                            onChange={(e) =>
                              updateTriggers(row.uid, {
                                assign_to_id: e.target.value,
                              })
                            }
                            placeholder="ID på person eller agent"
                            className="flex-1"
                          />
                        </div>
                      )}
                      <div className="flex items-center gap-2">
                        <input
                          type="checkbox"
                          id={`fire-${row.uid}`}
                          checked={row.triggers.fire_enabled}
                          onChange={(e) =>
                            updateTriggers(row.uid, {
                              fire_enabled: e.target.checked,
                            })
                          }
                        />
                        <Label
                          htmlFor={`fire-${row.uid}`}
                          className="text-xs font-normal"
                        >
                          Fyr en agent når issue lander her
                        </Label>
                      </div>
                      {row.triggers.fire_enabled && (
                        <div className="pl-6">
                          <Input
                            value={row.triggers.fire_agent_id}
                            onChange={(e) =>
                              updateTriggers(row.uid, {
                                fire_agent_id: e.target.value,
                              })
                            }
                            placeholder="Agent-ID der skal kaldes"
                          />
                        </div>
                      )}
                    </div>
                  </details>
                </div>
              ))}
            </div>

            <Button type="button" variant="outline" size="sm" onClick={addRow}>
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
