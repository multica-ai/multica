"use client";

import { useState } from "react";
import { Check, Pencil, Plus, Trash2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  useCustomPricingStore,
  type CustomModelPricing,
} from "@multica/core/runtimes/custom-pricing-store";
import { MODEL_PRICING } from "../../runtimes/utils";
import { useT } from "../../i18n";

const builtinModels = Object.keys(MODEL_PRICING).sort();

type Draft = {
  model: string;
  input: string;
  output: string;
  cacheRead: string;
  cacheWrite: string;
};

const EMPTY_DRAFT: Draft = {
  model: "",
  input: "",
  output: "",
  cacheRead: "",
  cacheWrite: "",
};

function hasAnyRateValue(d: Draft): boolean {
  return [d.input, d.output, d.cacheRead, d.cacheWrite].some((s) => s.trim() !== "");
}

function parseDraft(d: Draft): { model: string; pricing: CustomModelPricing } | null {
  const model = d.model.trim();
  if (!model) return null;
  if ([d.input, d.output, d.cacheRead].some((s) => s.trim() === "")) return null;
  const input = Number(d.input.trim());
  const output = Number(d.output.trim());
  const cacheRead = Number(d.cacheRead.trim());
  const cacheWrite =
    d.cacheWrite.trim() === "" ? output : Number(d.cacheWrite.trim());
  if ([input, output, cacheRead, cacheWrite].some((n) => !Number.isFinite(n) || n < 0)) {
    return null;
  }
  return { model, pricing: { input, output, cacheRead, cacheWrite } };
}

function toDraft(model: string, p: CustomModelPricing): Draft {
  return {
    model,
    input: String(p.input),
    output: String(p.output),
    cacheRead: String(p.cacheRead),
    cacheWrite: String(p.cacheWrite),
  };
}

function getDraftValidationError(d: Draft): "validation_required" | null {
  const model = d.model.trim();
  if (!model && !hasAnyRateValue(d)) return null;
  return parseDraft(d) ? null : "validation_required";
}

export function ModelPricingTab() {
  const { t } = useT("settings");
  const pricings = useCustomPricingStore((s) => s.pricings);
  const setCustomPricing = useCustomPricingStore((s) => s.setCustomPricing);
  const removeCustomPricing = useCustomPricingStore((s) => s.removeCustomPricing);

  const [addingNew, setAddingNew] = useState(false);
  const [draft, setDraft] = useState<Draft>(EMPTY_DRAFT);
  const [editingModel, setEditingModel] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState<Draft>(EMPTY_DRAFT);
  const [confirmDeleteModel, setConfirmDeleteModel] = useState<string | null>(null);

  const customModels = Object.keys(pricings).sort();
  const addError = getDraftValidationError(draft);
  const editError = editingModel ? getDraftValidationError(editDraft) : null;

  const updateDraft = (field: keyof Draft, value: string) => {
    setDraft((d) => ({ ...d, [field]: value }));
  };

  const updateEditDraft = (field: keyof Draft, value: string) => {
    setEditDraft((d) => ({ ...d, [field]: value }));
  };

  const handleAdd = () => {
    const parsed = parseDraft(draft);
    if (parsed) {
      setCustomPricing(parsed.model, parsed.pricing);
      setDraft(EMPTY_DRAFT);
      setAddingNew(false);
    }
  };

  const handleEditSave = () => {
    if (!editingModel) return;
    const parsed = parseDraft(editDraft);
    if (parsed) {
      if (parsed.model !== editingModel) {
        removeCustomPricing(editingModel);
      }
      setCustomPricing(parsed.model, parsed.pricing);
      setEditingModel(null);
      setEditDraft(EMPTY_DRAFT);
    }
  };

  const startEdit = (model: string) => {
    setEditingModel(model);
    setEditDraft(toDraft(model, pricings[model]!));
    setAddingNew(false);
    setConfirmDeleteModel(null);
  };

  const cancelEdit = () => {
    setEditingModel(null);
    setEditDraft(EMPTY_DRAFT);
  };

  const cancelAdd = () => {
    setAddingNew(false);
    setDraft(EMPTY_DRAFT);
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">
            {t(($) => $.model_pricing.builtin_section)}
          </h2>
          <p className="text-xs text-muted-foreground mt-1">
            {t(($) => $.model_pricing.unit_hint)}
          </p>
        </div>
        <Card>
          <CardContent className="p-0">
            <div className="max-h-[400px] overflow-y-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t(($) => $.model_pricing.table_model)}</TableHead>
                    <TableHead className="text-right">
                      {t(($) => $.model_pricing.table_input)}
                    </TableHead>
                    <TableHead className="text-right">
                      {t(($) => $.model_pricing.table_output)}
                    </TableHead>
                    <TableHead className="text-right">
                      {t(($) => $.model_pricing.table_cache_read)}
                    </TableHead>
                    <TableHead className="text-right">
                      {t(($) => $.model_pricing.table_cache_write)}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {builtinModels.map((model) => {
                    const p = MODEL_PRICING[model]!;
                    return (
                      <TableRow key={model}>
                        <TableCell className="font-mono text-xs">
                          {model}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-xs">
                          ${p.input.toFixed(2)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-xs">
                          ${p.output.toFixed(2)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-xs">
                          ${p.cacheRead.toFixed(2)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums text-xs">
                          ${p.cacheWrite.toFixed(2)}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-sm font-semibold">
              {t(($) => $.model_pricing.custom_section)}
            </h2>
            <p className="text-xs text-muted-foreground mt-1">
              {t(($) => $.model_pricing.description)}
            </p>
          </div>
          {!addingNew && !editingModel && (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setAddingNew(true)}
            >
              <Plus className="h-3 w-3" />
              {t(($) => $.model_pricing.add_model)}
            </Button>
          )}
        </div>

        {addingNew && (
          <Card>
            <CardContent className="space-y-3">
              <Input
                value={draft.model}
                onChange={(e) => updateDraft("model", e.target.value)}
                placeholder={t(($) => $.model_pricing.model_name_placeholder)}
                className="font-mono text-xs"
              />
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <PriceField
                  label={t(($) => $.model_pricing.field_input)}
                  value={draft.input}
                  onChange={(v) => updateDraft("input", v)}
                />
                <PriceField
                  label={t(($) => $.model_pricing.field_output)}
                  value={draft.output}
                  onChange={(v) => updateDraft("output", v)}
                />
                <PriceField
                  label={t(($) => $.model_pricing.field_cache_read)}
                  value={draft.cacheRead}
                  onChange={(v) => updateDraft("cacheRead", v)}
                />
                <PriceField
                  label={t(($) => $.model_pricing.field_cache_write)}
                  value={draft.cacheWrite}
                  onChange={(v) => updateDraft("cacheWrite", v)}
                />
              </div>
              {addError && (
                <p className="text-xs text-warning">
                  {t(($) => $.model_pricing[addError])}
                </p>
              )}
              <div className="flex items-center gap-2 justify-end">
                <Button variant="outline" size="sm" onClick={cancelAdd}>
                  {t(($) => $.model_pricing.cancel)}
                </Button>
                <Button size="sm" onClick={handleAdd} disabled={addError !== null}>
                  {t(($) => $.model_pricing.save)}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}

        {customModels.length === 0 && !addingNew ? (
          <p className="text-xs text-muted-foreground py-4 text-center">
            {t(($) => $.model_pricing.no_custom)}
          </p>
        ) : (
          customModels.map((model) => {
            const isEditing = editingModel === model;
            return (
              <Card key={model}>
                <CardContent className="space-y-3">
                  {isEditing ? (
                    <>
                      <Input
                        value={editDraft.model}
                        onChange={(e) =>
                          updateEditDraft("model", e.target.value)
                        }
                        placeholder={t(
                          ($) => $.model_pricing.model_name_placeholder,
                        )}
                        className="font-mono text-xs"
                      />
                      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                        <PriceField
                          label={t(($) => $.model_pricing.field_input)}
                          value={editDraft.input}
                          onChange={(v) => updateEditDraft("input", v)}
                        />
                        <PriceField
                          label={t(($) => $.model_pricing.field_output)}
                          value={editDraft.output}
                          onChange={(v) => updateEditDraft("output", v)}
                        />
                        <PriceField
                          label={t(($) => $.model_pricing.field_cache_read)}
                          value={editDraft.cacheRead}
                          onChange={(v) => updateEditDraft("cacheRead", v)}
                        />
                        <PriceField
                          label={t(($) => $.model_pricing.field_cache_write)}
                          value={editDraft.cacheWrite}
                          onChange={(v) => updateEditDraft("cacheWrite", v)}
                        />
                      </div>
                      {editError && (
                        <p className="text-xs text-warning">
                          {t(($) => $.model_pricing[editError])}
                        </p>
                      )}
                      <div className="flex items-center gap-2 justify-end">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={cancelEdit}
                        >
                          {t(($) => $.model_pricing.cancel)}
                        </Button>
                        <Button
                          size="sm"
                          onClick={handleEditSave}
                          disabled={editError !== null}
                        >
                          {t(($) => $.model_pricing.save)}
                        </Button>
                      </div>
                    </>
                  ) : (
                    <div className="flex items-center justify-between gap-3">
                      <div className="flex-1 min-w-0">
                        <code className="font-mono text-xs">{model}</code>
                        <div className="mt-1 flex gap-4 text-xs text-muted-foreground tabular-nums">
                          <span>
                            {`${t(($) => $.model_pricing.field_input)}: $${pricings[model]!.input.toFixed(2)}`}
                          </span>
                          <span>
                            {`${t(($) => $.model_pricing.field_output)}: $${pricings[model]!.output.toFixed(2)}`}
                          </span>
                          <span>
                            {`${t(($) => $.model_pricing.field_cache_read)}: $${pricings[model]!.cacheRead.toFixed(2)}`}
                          </span>
                          <span>
                            {`${t(($) => $.model_pricing.field_cache_write)}: $${pricings[model]!.cacheWrite.toFixed(2)}`}
                          </span>
                        </div>
                      </div>
                      <div className="flex items-center shrink-0">
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          onClick={() => startEdit(model)}
                          aria-label={t(($) => $.model_pricing.edit_aria)}
                        >
                          <Pencil />
                        </Button>
                        <Popover
                          open={confirmDeleteModel === model}
                          onOpenChange={(open) =>
                            setConfirmDeleteModel(open ? model : null)
                          }
                        >
                          <PopoverTrigger
                            render={
                              <button
                                type="button"
                                className="inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring h-6 w-6"
                                aria-label={t(($) => $.model_pricing.delete_aria)}
                              >
                                <Trash2 className="h-3 w-3" />
                              </button>
                            }
                          />
                          <PopoverContent className="w-auto p-2" align="end">
                            <div className="flex items-center gap-1">
                              <button
                                type="button"
                                className="inline-flex items-center justify-center rounded-md h-6 w-6 text-sm hover:bg-accent text-destructive"
                                onClick={() => {
                                  removeCustomPricing(model);
                                  setConfirmDeleteModel(null);
                                }}
                              >
                                <Check className="h-3 w-3" />
                              </button>
                              <button
                                type="button"
                                className="inline-flex items-center justify-center rounded-md h-6 w-6 text-sm hover:bg-accent text-success"
                                onClick={() => setConfirmDeleteModel(null)}
                              >
                                <X className="h-3 w-3" />
                              </button>
                            </div>
                          </PopoverContent>
                        </Popover>
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>
            );
          })
        )}
      </section>
    </div>
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
