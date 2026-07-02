"use client";

// FIR-2283 — human-friendly skill picker for the Issue workflow form.
// Jesper's review ("Dødssynd 1: ingen mennesker kender UUID") required
// replacing the raw skill text input with a real picker. Agent and member
// selection reuse the shared AgentPicker / AssigneePicker directly (imported
// in the form); this file only adds the one selector the app did not already
// expose standalone: a single-skill picker keyed by the skill NAME (the loop
// spec keys skills by name, not id). Built on the same NativeSelect the rest
// of the form uses so it stays inside the existing surface.

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";
import { NativeSelect } from "@multica/ui/components/ui/native-select";

export function SkillNamePicker({
  value,
  onChange,
  placeholder = "Select a skill",
}: {
  value: string;
  onChange: (name: string) => void;
  placeholder?: string;
}) {
  const wsId = useWorkspaceId();
  const { data: skills = [] } = useQuery(skillListOptions(wsId));
  const known = skills.some((s) => s.name === value);

  return (
    <NativeSelect value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">{placeholder}</option>
      {/* Preserve a previously-saved skill whose name is no longer in the
          workspace list, so editing an old recipe never silently drops it. */}
      {value && !known ? <option value={value}>{value}</option> : null}
      {skills.map((s) => (
        <option key={s.id} value={s.name}>
          {s.name}
        </option>
      ))}
    </NativeSelect>
  );
}
