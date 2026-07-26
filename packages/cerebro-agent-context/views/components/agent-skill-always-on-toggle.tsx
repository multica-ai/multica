"use client";

// FIR-3805 — the "Always on" checkbox on one row of the agent Skills tab.
//
// A bound skill normally reaches the agent as one line in its brief (name +
// description) and is opened on demand. That is right for task recipes ("how to
// deploy"), but wrong for rules that must hold on every single response —
// writing style, check-before-you-send gates. Those were never applied, because
// the agent had no reason to open the skill before it started writing.
//
// Checking this box pastes the skill's full text into the agent's instructions
// on every run instead. The flag lives on the BINDING, not the skill: the same
// skill can be always on for one agent and optional for another.
//
// The control only edits the tab's local draft. It reaches the agent through
// the same propose → approve flow as adding or removing a skill, because
// turning a skill always-on is the stronger of the two actions.

import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Label } from "@multica/ui/components/ui/label";
import { useFlagValue } from "@multica/cerebro-feature-flags";

interface Props {
  /** Skill id this row renders — used to keep the label's htmlFor unique. */
  skillId: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
}

export function AgentSkillAlwaysOnToggle({
  skillId,
  checked,
  onCheckedChange,
  disabled = false,
}: Props) {
  const enabled = useFlagValue("cerebro_agent_skill_always_on");
  // Flag off hides the control, but never hides the STATE: a skill already
  // marked always on keeps behaving that way, so silently dropping the
  // indicator would misrepresent what the agent actually reads.
  if (!enabled && !checked) return null;

  const id = `always-on-${skillId}`;
  return (
    <div className="flex shrink-0 items-center gap-1.5">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={(v) => onCheckedChange(v === true)}
        disabled={disabled || !enabled}
      />
      <Label
        htmlFor={id}
        className="cursor-pointer text-xs font-normal text-muted-foreground"
        title="Paste this skill's full text into the agent's instructions on every run, instead of listing it for the agent to open on demand."
      >
        Always on
      </Label>
    </div>
  );
}
