"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { ButtonGroup } from "@multica/ui/components/ui/button-group";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  clampHoldoutPct,
  COST_SAVING_DEFAULTS,
  COST_SAVINGS,
  type CostSavingDefinition,
  type CostSavingKey,
  type CostSavingMode,
  DEFAULT_COST_SAVING_HOLDOUT_PCT,
  isFullRollout,
  runtimeScopeBadge,
  runtimeScopeNote,
  savingSupportsHoldout,
} from "../registry";
import { useCostOptimizationStore, useSavingMode } from "../store";
import {
  useCostOptimizationHoldoutQuery,
  useCostOptimizationQuery,
  useSetHoldoutMutation,
  useSetSavingModeMutation,
} from "./api";

const MODE_OPTIONS: { mode: CostSavingMode; label: string }[] = [
  { mode: "off", label: "Off" },
  { mode: "shadow", label: "Measure only" },
  { mode: "on", label: "On" },
];

export function SavingHoldoutControl({
  label,
  override,
  canManage,
  pending,
  onApply,
}: {
  label: string;
  override: number | undefined;
  canManage: boolean;
  pending: boolean;
  onApply: (pct: number | null) => void;
}) {
  const isOverride = override !== undefined;
  const effective = override ?? DEFAULT_COST_SAVING_HOLDOUT_PCT;
  const rollout = 100 - effective;

  const [draft, setDraft] = useState<string>("");
  const draftValue = draft === "" ? String(effective) : draft;
  const inputId = `holdout-${label.replace(/\s+/g, "-").toLowerCase()}`;

  const apply = (pct: number | null) => {
    setDraft("");
    onApply(pct);
  };

  const saveDraft = () => {
    const parsed = Number.parseInt(draftValue, 10);
    if (Number.isNaN(parsed)) {
      toast.error("Enter a number between 0 and 100");
      return;
    }
    apply(clampHoldoutPct(parsed));
  };

  const disabled = !canManage || pending;

  return (
    <div className="space-y-2 rounded-md border bg-muted/30 p-3">
      <p className="text-xs text-muted-foreground">
        Rollout vs. holdout for this saving. Holdout is the share of its runs
        kept on the old behavior as a control group so the real saving can be
        measured; rollout is the rest. Full rollout means no control group.
      </p>
      <p className="text-xs text-muted-foreground">
        Currently <span className="font-medium">{rollout}% rollout</span> /{" "}
        <span className="font-medium">{effective}% holdout</span>
        {isOverride
          ? " (set for this workspace)."
          : ` (server default: ${DEFAULT_COST_SAVING_HOLDOUT_PCT}% holdout).`}
      </p>
      <div className="flex flex-wrap items-end gap-3">
        <Button
          type="button"
          size="sm"
          variant={isFullRollout(effective) ? "default" : "outline"}
          aria-pressed={isFullRollout(effective)}
          disabled={disabled || isFullRollout(effective)}
          onClick={() => apply(0)}
        >
          Full rollout (100%)
        </Button>
        <div className="flex items-end gap-2">
          <div className="space-y-1">
            <Label htmlFor={inputId} className="text-xs text-muted-foreground">
              Holdout %
            </Label>
            <Input
              id={inputId}
              type="number"
              min={0}
              max={100}
              className="w-24"
              value={draftValue}
              disabled={disabled}
              onChange={(e) => setDraft(e.target.value)}
            />
          </div>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={disabled}
            onClick={saveDraft}
          >
            Save
          </Button>
        </div>
        {isOverride && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={disabled}
            onClick={() => apply(null)}
          >
            Reset to default
          </Button>
        )}
      </div>
    </div>
  );
}

function SavingRow({
  def,
  canManage,
  holdoutOverride,
}: {
  def: CostSavingDefinition;
  canManage: boolean;
  holdoutOverride: number | undefined;
}) {
  const mode = useSavingMode(def.key);
  const mutation = useSetSavingModeMutation();
  const holdoutMutation = useSetHoldoutMutation();
  const scopeBadge = runtimeScopeBadge(def.runtimeScope);
  const scopeNote = runtimeScopeNote(def.runtimeScope);

  const handleSelect = (next: CostSavingMode) => {
    if (next === mode) return;
    mutation.mutate(
      { key: def.key, mode: next },
      {
        onError: (err) => {
          toast.error(
            err instanceof Error ? err.message : `Failed to update ${def.label}`,
          );
        },
      },
    );
  };

  const showHoldout = savingSupportsHoldout(def.key) && mode === "on";

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <Label className="text-sm font-medium">{def.label}</Label>
            {scopeBadge && <Badge variant="secondary">{scopeBadge}</Badge>}
          </div>
          <p className="text-xs text-muted-foreground">{def.description}</p>
          {scopeNote && (
            <p className="text-xs text-muted-foreground">{scopeNote}</p>
          )}
          <p className="text-xs text-muted-foreground italic">{def.estimateNote}</p>
        </div>
        <ButtonGroup aria-label={`${def.label} mode`}>
          {MODE_OPTIONS.map((option) => {
            const active = mode === option.mode;
            return (
              <Button
                key={option.mode}
                type="button"
                size="sm"
                variant={active ? "default" : "outline"}
                aria-pressed={active}
                disabled={!canManage || mutation.isPending}
                onClick={() => handleSelect(option.mode)}
              >
                {option.label}
              </Button>
            );
          })}
        </ButtonGroup>
        {showHoldout && (
          <SavingHoldoutControl
            label={def.label}
            override={holdoutOverride}
            canManage={canManage}
            pending={holdoutMutation.isPending}
            onApply={(pct) =>
              holdoutMutation.mutate(
                { key: def.key, pct },
                {
                  onError: (err) =>
                    toast.error(
                      err instanceof Error
                        ? err.message
                        : `Failed to update ${def.label} holdout`,
                    ),
                },
              )
            }
          />
        )}
      </CardContent>
    </Card>
  );
}

export function CostOptimizationSettingsPanel() {
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const canManage = role === "owner" || role === "admin";
  const query = useCostOptimizationQuery();
  const holdoutQuery = useCostOptimizationHoldoutQuery();
  const holdoutOverrides = holdoutQuery.data ?? {};
  // Reactive overrides map used to evaluate dependsOn visibility conditions.
  const overrides = useCostOptimizationStore((s) => s.overrides);
  const resolveMode = (key: CostSavingKey): CostSavingMode =>
    overrides[key] ?? COST_SAVING_DEFAULTS[key];

  return (
    <section className="space-y-4">
      <p className="text-xs text-muted-foreground">
        Each agent saving has three states. Off runs exactly as today. Measure
        only computes what the saving would have saved, with zero behavior change.
        On applies the saving and measures what it actually saved against a
        baseline. For cloud savings you can also choose per saving whether it runs
        at full rollout or keeps a holdout control group.
      </p>

      {!canManage && (
        <p className="text-xs text-muted-foreground">
          Only workspace owners and admins can change these settings.
        </p>
      )}

      {query.isError && (
        <p className="text-xs text-destructive">
          Failed to load cost optimization settings:{" "}
          {query.error instanceof Error ? query.error.message : "unknown error"}
        </p>
      )}

      <div className="space-y-3">
        {COST_SAVINGS.map((def) => {
          if (def.dependsOn && resolveMode(def.dependsOn) !== "on") return null;
          return (
            <SavingRow
              key={def.key}
              def={def}
              canManage={canManage}
              holdoutOverride={holdoutOverrides[def.key as CostSavingKey]}
            />
          );
        })}
      </div>
    </section>
  );
}
