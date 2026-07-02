"use client";

// Agent Office (FIR-1775) — resolves the skill UUIDs stored in a context
// snapshot to human-readable skill names for display. The snapshot only holds
// opaque ids; the workspace skill list carries the names, so the view layer
// loads it once and hands snapshotToFields a resolver. Unknown ids (a skill
// deleted since the version was cut) pass through unchanged so the diff still
// shows what is actually persisted.

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { skillListOptions } from "@multica/core/workspace/queries";

export function useSkillNameResolver(): (id: string) => string {
  const wsId = useWorkspaceId();
  const { data: skills = [] } = useQuery(skillListOptions(wsId));

  return useCallback(
    (id: string) => skills.find((s) => s.id === id)?.name ?? id,
    [skills],
  );
}
