"use client";

import { useEffect, useState } from "react";
import { Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  useCustomPricingStore,
  type CustomModelPricing,
} from "@multica/core/runtimes/custom-pricing-store";
import { useT } from "../../i18n";

// Per-million-token rate fields. Stored as strings during editing so the
// user can briefly hold an empty field without us defaulting to 0 and
// hiding their work.
type DraftRow = {
  input: string;
  output: string;
  cacheRead: string;
  cacheWrite: string;
};

const EMPTY_DRAFT: DraftRow = { input: "", output: "", cacheRead: "", cacheWrite: "" };

function toDraft(p: CustomModelPricing | undefined): DraftRow {
  if (!p) return EMPTY_DRAFT;
  return {
    input: String(p.input),
    output: String(p.output),
    cacheRead: String(p.cacheRead),
    cacheWrite: String(p.cacheWrite),
  };
}

function parseRow(draft: DraftRow): CustomModelPricing | null {
  const values = [draft.input, draft.output, draft.cacheRead, draft.cacheWrite].map(
    (s) => Number(s.trim()),
  );
  if (values.some((n) => !Number.isFinite(n) || n < 0)) return null;
  const [input, output, cacheRead, cacheWrite] = values as [
    number,
    number,
    number,
    number,
  ];
  return { input, output, cacheRead, cacheWrite };
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Models flagged as unmapped right now — these always appear in the form
  // so users can price the model they came here to fix. Models that are
  // already in the custom-pricing store also appear (so existing entries
  // can be edited / removed), even if they're no longer "unmapped".
  unmappedModels: readonly string[];
}

export function CustomPricingDialog({ open, onOpenChange, unmappedModels }: Props) {
  const { t } = useT("runtimes");
  const pricings = useCustomPricingStore((s) => s.pricings);
  const setCustomPricing = useCustomPricingStore((s) => s.setCustomPricing);
  const removeCustomPricing = useCustomPricingStore((s) => s.removeCustomPricing);

  // Show every unmapped model plus everything already in the store, so a
  // user revisiting the dialog after saving can still see / tweak / remove
  // their prior entries.
  const rows = Array.from(
    new Set([...unmappedModels, ...Object.keys(pricings)]),
  ).sort();

  const [drafts, setDrafts] = useState<Record<string, DraftRow>>({});

  // Reset drafts whenever the dialog opens (or the visible row-set changes
  // while it's open) so stale half-typed values from a previous session
  // don't persist into a fresh edit.
  useEffect(() => {
    if (!open) return;
    const fresh: Record<string, DraftRow> = {};
    for (const model of rows) {
      fresh[model] = toDraft(pricings[model]);
    }
    setDrafts(fresh);
    // We intentionally don't depend on `rows` (a new array each render) —
    // depending on `open` + the joined model list is enough and avoids
    // resetting drafts on every parent re-render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, rows.join("\n")]);

  const updateField = (model: string, field: keyof DraftRow, value: string) => {
    setDrafts((d) => ({
      ...d,
      [model]: { ...(d[model] ?? EMPTY_DRAFT), [field]: value },
    }));
  };

  const handleSave = () => {
    for (const model of rows) {
      const draft = drafts[model] ?? EMPTY_DRAFT;
      const parsed = parseRow(draft);
      const allEmpty =
        draft.input.trim() === "" &&
        draft.output.trim() === "" &&
        draft.cacheRead.trim() === "" &&
        draft.cacheWrite.trim() === "";
      if (allEmpty) {
        // Treat clearing every field as "remove this override".
        if (pricings[model]) removeCustomPricing(model);
        continue;
      }
      if (parsed) setCustomPricing(model, parsed);
    }
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.usage.custom_pricing.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.usage.custom_pricing.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[60vh] space-y-4 overflow-y-auto">
          {rows.length === 0 ? (
            <p className="py-4 text-center text-xs text-muted-foreground">
              {t(($) => $.usage.custom_pricing.empty)}
            </p>
          ) : (
            rows.map((model) => {
              const draft = drafts[model] ?? EMPTY_DRAFT;
              const hasOverride = Boolean(pricings[model]);
              return (
                <div key={model} className="space-y-2 rounded-md border p-3">
                  <div className="flex items-center justify-between gap-2">
                    <code className="truncate font-mono text-xs">{model}</code>
                    {hasOverride && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => removeCustomPricing(model)}
                        aria-label={t(($) => $.usage.custom_pricing.remove_aria)}
                        title={t(($) => $.usage.custom_pricing.remove_aria)}
                      >
                        <Trash2 />
                      </Button>
                    )}
                  </div>
                  <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                    <PriceField
                      label={t(($) => $.usage.custom_pricing.field_input)}
                      value={draft.input}
                      onChange={(v) => updateField(model, "input", v)}
                    />
                    <PriceField
                      label={t(($) => $.usage.custom_pricing.field_output)}
                      value={draft.output}
                      onChange={(v) => updateField(model, "output", v)}
                    />
                    <PriceField
                      label={t(($) => $.usage.custom_pricing.field_cache_read)}
                      value={draft.cacheRead}
                      onChange={(v) => updateField(model, "cacheRead", v)}
                    />
                    <PriceField
                      label={t(($) => $.usage.custom_pricing.field_cache_write)}
                      value={draft.cacheWrite}
                      onChange={(v) => updateField(model, "cacheWrite", v)}
                    />
                  </div>
                </div>
              );
            })
          )}
          <p className="text-[11px] text-muted-foreground">
            {t(($) => $.usage.custom_pricing.unit_hint)}
          </p>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.usage.custom_pricing.cancel)}
          </Button>
          <Button onClick={handleSave}>
            {t(($) => $.usage.custom_pricing.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PriceField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="space-y-1">
      <Label className="text-[11px] text-muted-foreground">{label}</Label>
      <Input
        type="number"
        inputMode="decimal"
        min="0"
        step="0.01"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="0.00"
      />
    </div>
  );
}
