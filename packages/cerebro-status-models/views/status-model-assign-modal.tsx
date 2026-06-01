"use client";

// Cerebro workflow v2b (FIR-1550) — mapping confirmation modal.
//
// Shown when an admin picks a status model for a project. Surfaces the
// per-base custom_status mapping so the admin sees exactly where current
// issues will land — no silent omplacering. The modal lets the admin pick a
// specific custom_status under each base when the model offers multiple
// (default: first under each base, matching the server-side auto-resolver).
//
// On confirm: the project's model is set. Per-issue migration happens
// lazily — the next UpdateIssue on any issue routes through the upstream
// path and the cerebro resolver pins the sidecar to the chosen mapping.
// Issues that don't receive any update keep showing the suggested label
// via the cerebro presentation seam (custom_status enricher returns the
// first matching entry on read) until they're explicitly touched.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { useWorkspaceId } from "@multica/core/hooks";
import { ALL_STATUSES } from "@multica/core/issues/config";
import type { IssueStatus } from "@multica/core/types";
import {
  cerebroStatusModelDetailOptions,
  useAssignProjectStatusModelMutation,
} from "../core";
import type { CerebroStatusModel } from "../core";

export interface StatusModelAssignModalProps {
  open: boolean;
  projectId: string;
  projectName: string;
  modelId: string;
  onClose: () => void;
  onApplied?: () => void;
}

export function StatusModelAssignModal({
  open,
  projectId,
  projectName,
  modelId,
  onClose,
  onApplied,
}: StatusModelAssignModalProps) {
  const wsId = useWorkspaceId();
  const { data: model } = useQuery({
    ...cerebroStatusModelDetailOptions(wsId, modelId),
    enabled: open && !!modelId,
  });

  // Per-base candidate custom statuses (the model can define many under one
  // base). ALL_STATUSES drives row order so the modal is deterministic.
  const candidatesByBase = useMemo(() => {
    const map = new Map<IssueStatus, CerebroStatusModel["statuses"]>();
    if (!model) return map;
    const sorted = [...model.statuses].sort((a, b) => a.position - b.position);
    for (const s of sorted) {
      const list = map.get(s.base_status as IssueStatus) ?? [];
      list.push(s);
      map.set(s.base_status as IssueStatus, list);
    }
    return map;
  }, [model]);

  // Per-row override: first candidate under each base by default.
  const [mapping, setMapping] = useState<Record<string, string>>({});

  const assignMutation = useAssignProjectStatusModelMutation(wsId);

  if (!open) return null;

  const handleConfirm = async () => {
    try {
      // Build the full per-base mapping the admin confirmed (explicit override
      // where they changed the dropdown, first candidate otherwise) and send it
      // so the server pins existing issues right away — no silent omplacering.
      const resolvedMapping: Record<string, string> = {};
      for (const base of visibleRows) {
        const candidates = candidatesByBase.get(base) ?? [];
        const key = mapping[base] ?? candidates[0]?.key;
        if (key) resolvedMapping[base] = key;
      }
      await assignMutation.mutateAsync({
        projectId,
        statusModelId: modelId,
        mapping: resolvedMapping,
      });
      toast.success(
        `Statusmodel anvendt på ${projectName}. Eksisterende issues er flyttet til den valgte status.`,
      );
      onApplied?.();
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Kunne ikke anvende statusmodellen.",
      );
    }
  };

  const visibleRows = ALL_STATUSES.filter((s) => candidatesByBase.has(s));

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
      role="dialog"
      aria-modal="true"
      aria-label={`Anvend statusmodel på ${projectName}`}
    >
      <div
        className="w-full max-w-2xl rounded-xl border bg-card p-6 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4">
          <h3 className="text-base font-semibold">
            Anvend statusmodel på {projectName}
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Eksisterende issues placeres i den valgte custom-status under hver
            grundtilstand. Du kan justere mappings før du bekræfter.
          </p>
        </div>

        {visibleRows.length === 0 ? (
          <p className="rounded-lg border bg-muted/30 px-4 py-3 text-sm text-muted-foreground">
            Modellen indeholder endnu ingen custom statusser at mappe til.
            Tilføj statusser før du tildeler modellen.
          </p>
        ) : (
          <div className="rounded-lg border">
            <div className="grid grid-cols-[1fr_2fr] items-center gap-3 border-b bg-muted/30 px-4 py-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              <span>Grundstatus</span>
              <span>Lander i</span>
            </div>
            {visibleRows.map((base) => {
              const candidates = candidatesByBase.get(base) ?? [];
              const value = mapping[base] ?? candidates[0]?.key ?? "";
              return (
                <div
                  key={base}
                  className="grid grid-cols-[1fr_2fr] items-center gap-3 border-b px-4 py-2 text-sm last:border-b-0"
                >
                  <span className="font-medium">{base}</span>
                  {candidates.length === 0 ? (
                    <span className="text-muted-foreground">
                      Ingen custom status — falder tilbage til grundstatus
                    </span>
                  ) : (
                    <NativeSelect
                      value={value}
                      onChange={(e) =>
                        setMapping((prev) => ({ ...prev, [base]: e.target.value }))
                      }
                    >
                      {candidates.map((c) => (
                        <NativeSelectOption key={c.key} value={c.key}>
                          {c.label}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  )}
                </div>
              );
            })}
          </div>
        )}

        <div className="mt-6 flex items-center justify-end gap-2">
          <Button
            variant="outline"
            onClick={onClose}
            disabled={assignMutation.isPending}
          >
            Annullér
          </Button>
          <Button
            onClick={handleConfirm}
            disabled={
              assignMutation.isPending || !model || visibleRows.length === 0
            }
          >
            {assignMutation.isPending ? "Anvender…" : "Anvend statusmodel"}
          </Button>
        </div>
      </div>
    </div>
  );
}
