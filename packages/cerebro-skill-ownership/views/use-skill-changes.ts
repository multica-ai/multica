"use client";

// FIR-2742 — workspace-wide pending change requests, joined to the skills the
// caller already has and split into "mine" (skills I own/approve) vs "all".
// Powers the Skills-page alert count and the cross-skill review sheet.

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import type { SkillSummary } from "@multica/core/types";
import { pendingSkillChangeRequestsOptions } from "../core/queries";
import {
  enrichSkillChanges,
  type EnrichedSkillChange,
} from "../core/select-changes";

export interface SkillChangesData {
  /** Every pending change request on a skill visible to this user. */
  all: EnrichedSkillChange[];
  /** Pending changes on skills the current user owns or approves. */
  mine: EnrichedSkillChange[];
  isLoading: boolean;
}

export function useSkillChanges(skills: SkillSummary[]): SkillChangesData {
  const enabled = useFeatureFlag("cerebro_skill_ownership");
  const userId = useAuthStore((s) => s.user?.id ?? null);

  const { data = [], isLoading } = useQuery({
    ...pendingSkillChangeRequestsOptions(),
    enabled,
  });

  const all = useMemo(
    () => enrichSkillChanges(data, skills, userId),
    [data, skills, userId],
  );
  const mine = useMemo(() => all.filter((c) => c.mine), [all]);

  return { all, mine, isLoading: enabled ? isLoading : false };
}
