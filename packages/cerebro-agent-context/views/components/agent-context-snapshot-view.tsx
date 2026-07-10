"use client";

// Agent Office (FIR-1775) — read-only field-by-field view of a context
// snapshot. Lets the user SEE the whole versioned bundle (so it's clear what a
// version contains and what a proposal carries over unchanged), instead of
// only the instructions blob.

import type { AgentContextSnapshot } from "@multica/core/types";
import { snapshotToFields } from "../../core/snapshot-fields";
import { useSkillNameResolver } from "../use-skill-name-resolver";

interface Props {
  snapshot: AgentContextSnapshot;
  /** Field keys to hide (e.g. ones already shown as editable inputs). */
  exclude?: string[];
}

export function AgentContextSnapshotView({ snapshot, exclude = [] }: Props) {
  const resolveSkill = useSkillNameResolver();
  const fields = snapshotToFields(snapshot, { resolveSkill }).filter(
    (f) => !exclude.includes(f.key),
  );

  return (
    <div className="space-y-2.5">
      {fields.map((f) => (
        <div key={f.key}>
          <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            {f.label}
          </div>
          {f.value ? (
            <pre
              className={`mt-0.5 max-h-32 overflow-auto whitespace-pre-wrap break-words rounded border bg-muted/20 px-2 py-1 text-xs ${
                f.mono ? "font-mono" : ""
              }`}
            >
              {f.value}
            </pre>
          ) : (
            <div className="mt-0.5 text-xs italic text-muted-foreground">
              not set
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
