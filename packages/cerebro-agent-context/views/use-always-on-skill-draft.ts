"use client";

// FIR-3805 — draft state for the per-binding "always on" flag on the agent
// Skills tab.
//
// The tab already keeps a draft of WHICH skills are bound and proposes it
// through the versioned change-request flow. Always-on is a second dimension of
// that same draft, so it lives here rather than in the upstream tab: the tab
// gets one hook call and a checkbox, and every rule about how the two lists stay
// consistent is in one place.

import { useEffect, useMemo, useState } from "react";
import type { Agent } from "@multica/core/types";

export interface AlwaysOnSkillDraft {
  /** Ids currently flagged always-on in the draft. */
  alwaysOnIds: string[];
  /** Whether this skill id is flagged in the draft. */
  isAlwaysOn: (skillId: string) => boolean;
  /** Flag or unflag one skill in the draft. */
  setAlwaysOn: (skillId: string, alwaysOn: boolean) => void;
  /** Drop a skill from the draft entirely (called when the skill is unbound). */
  forget: (skillId: string) => void;
  /** True when the draft differs from what the agent has live. */
  dirty: boolean;
  /** Reset the draft back to the agent's live state. */
  discard: () => void;
  /**
   * The value to send as `always_on_skill_ids`, narrowed to the skills that are
   * still in the proposed binding set. An id that is being removed in the same
   * proposal must not survive as a flag.
   */
  proposalValue: (draftSkillIds: string[]) => string[];
}

export function useAlwaysOnSkillDraft(agent: Agent): AlwaysOnSkillDraft {
  const liveIds = useMemo(
    () =>
      agent.skills
        .filter((s) => s.always_on === true)
        .map((s) => s.id)
        .sort(),
    [agent.skills],
  );

  const [draft, setDraft] = useState<string[]>(liveIds);

  // Re-sync to live whenever it changes underneath us (a proposal approved
  // elsewhere) and the user has no pending local edits — same rule the tab
  // already applies to the bound-skill draft.
  useEffect(() => {
    setDraft((cur) => (sameSet(cur, liveIds) ? liveIds : cur));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [liveIds.join("|")]);

  return {
    alwaysOnIds: draft,
    isAlwaysOn: (skillId) => draft.includes(skillId),
    setAlwaysOn: (skillId, alwaysOn) =>
      setDraft((cur) =>
        alwaysOn
          ? cur.includes(skillId)
            ? cur
            : [...cur, skillId]
          : cur.filter((x) => x !== skillId),
      ),
    forget: (skillId) => setDraft((cur) => cur.filter((x) => x !== skillId)),
    dirty: !sameSet(draft, liveIds),
    discard: () => setDraft(liveIds),
    proposalValue: (draftSkillIds) =>
      draftSkillIds.filter((id) => draft.includes(id)),
  };
}

// Order-insensitive set comparison.
function sameSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sb = new Set(b);
  return a.every((x) => sb.has(x));
}
