"use client";

// Agent Office (FIR-1775) — field-by-field diff between two context snapshots.
// Replaces the old instructions-only diff so a reviewer can see EXACTLY which
// part of the bundle changed (model, skills, sandbox, config, …), not just the
// instructions text. Reuses the line-level AgentContextDiffView per field.

import type { AgentContextSnapshot } from "@multica/core/types";
import { snapshotToFields } from "../../core/snapshot-fields";
import { AgentContextDiffView } from "./agent-context-diff-view";
import { useSkillNameResolver } from "../use-skill-name-resolver";

interface Props {
  base: AgentContextSnapshot;
  proposed: AgentContextSnapshot;
  baseLabel?: string;
  proposedLabel?: string;
  /**
   * When set, only these snapshot keys are considered — used by the Skills and
   * MCP tabs to scope the diff to the one field that tab edits, so a reviewer
   * sees just that change instead of the whole bundle.
   */
  onlyKeys?: string[];
}

export function AgentContextFieldDiff({
  base,
  proposed,
  baseLabel = "base",
  proposedLabel = "proposed",
  onlyKeys,
}: Props) {
  const resolveSkill = useSkillNameResolver();
  const baseFields = snapshotToFields(base, { resolveSkill });
  const proposedFields = snapshotToFields(proposed, { resolveSkill });

  const rows = baseFields
    .map((bf, i) => ({
      key: bf.key,
      label: bf.label,
      prev: bf.value,
      next: proposedFields[i]?.value ?? "",
    }))
    .filter((r) => !onlyKeys || onlyKeys.includes(r.key));

  const changed = rows.filter((r) => r.prev !== r.next);
  const unchanged = rows.filter((r) => r.prev === r.next);

  if (changed.length === 0) {
    return (
      <div className="rounded-md border border-dashed px-3 py-4 text-center text-xs text-muted-foreground">
        No field differences between {baseLabel} and {proposedLabel}.
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="text-xs text-muted-foreground">
        <span className="font-medium text-foreground">
          {changed.length} of {rows.length}
        </span>{" "}
        fields changed · {baseLabel} → {proposedLabel}
      </div>

      {changed.map((r) => (
        <div key={r.key} className="space-y-1">
          <div className="text-[11px] font-semibold uppercase tracking-wide text-foreground">
            {r.label}
          </div>
          <AgentContextDiffView
            base={r.prev || "(not set)"}
            proposed={r.next || "(not set)"}
            baseLabel={baseLabel}
            proposedLabel={proposedLabel}
          />
        </div>
      ))}

      {unchanged.length > 0 && (
        <div className="text-[11px] text-muted-foreground">
          Unchanged: {unchanged.map((r) => r.label).join(", ")}
        </div>
      )}
    </div>
  );
}
