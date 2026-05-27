"use client";

import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { Button } from "@multica/ui/components/ui/button";
import { ButtonGroup } from "@multica/ui/components/ui/button-group";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import {
  COST_SAVINGS,
  type CostSavingDefinition,
  type CostSavingMode,
} from "../registry";
import { useSavingMode } from "../store";
import { useCostOptimizationQuery, useSetSavingModeMutation } from "./api";
import { CostOptimizationDashboard } from "./dashboard";

/**
 * The three states a saving can be in, in display order. Labels mirror the
 * three-state model in the registry: off runs as today, shadow ("measure only")
 * computes the would-have-saved with zero behavior change, on applies the
 * saving and measures the actual result against a baseline.
 */
const MODE_OPTIONS: { mode: CostSavingMode; label: string }[] = [
  { mode: "off", label: "Off" },
  { mode: "shadow", label: "Measure only" },
  { mode: "on", label: "On" },
];

function SavingRow({
  def,
  canManage,
}: {
  def: CostSavingDefinition;
  canManage: boolean;
}) {
  const mode = useSavingMode(def.key);
  const mutation = useSetSavingModeMutation();

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

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="space-y-1">
          <Label className="text-sm font-medium">{def.label}</Label>
          <p className="text-xs text-muted-foreground">{def.description}</p>
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
      </CardContent>
    </Card>
  );
}

export function CostOptimizationSettingsTab() {
  const workspace = useCurrentWorkspace();
  const { role } = useCurrentMember(workspace?.id ?? "");
  const canManage = role === "owner" || role === "admin";
  const query = useCostOptimizationQuery();

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Cost optimization</h2>
        <p className="text-xs text-muted-foreground">
          Each agent saving has three states. Off runs exactly as today. Measure
          only computes what the saving would have saved, with zero behavior
          change. On applies the saving and measures what it actually saved
          against a baseline.
        </p>
      </div>

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
        {COST_SAVINGS.map((def) => (
          <SavingRow key={def.key} def={def} canManage={canManage} />
        ))}
      </div>

      <CostOptimizationDashboard />
    </section>
  );
}
