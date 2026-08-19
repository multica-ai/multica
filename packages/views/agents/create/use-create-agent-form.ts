"use client";

import {
  useEffect,
  useMemo,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  EMPTY_AGENT_DRAFT,
  isDraftDescriptionWithinLimit,
  type AgentDraft,
} from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { isRuntimeUsableForUser, runtimeListOptions } from "@multica/core/runtimes";
import type {
  MemberWithUser,
  RuntimeDevice,
  SkillSummary,
} from "@multica/core/types";
import {
  memberListOptions,
  skillListOptions,
} from "@multica/core/workspace/queries";

interface CreateAgentForm {
  draft: AgentDraft;
  setDraft: Dispatch<SetStateAction<AgentDraft>>;
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  /** The runtime query answered — either data or an error. */
  runtimesSettled: boolean;
  /** Online runtimes this member is allowed to execute on. */
  usableRuntimes: RuntimeDevice[];
  selectedRuntime: RuntimeDevice | null;
  members: MemberWithUser[];
  workspaceSkills: SkillSummary[];
  currentUserId: string | null;
  /** Every non-name precondition the create button depends on. */
  draftReady: boolean;
}

/**
 * The state for manual agent creation: one draft plus the workspace catalogs
 * the form renders from. Draft persistence belongs to the manual route.
 */
export function useCreateAgentForm(options?: {
  /** Workspace skills are hidden in hosts where skills belong to the local runtime. */
  includeWorkspaceSkills?: boolean;
}): CreateAgentForm {
  const wsId = useWorkspaceId();
  const currentUser = useAuthStore((state) => state.user);
  const currentUserId = currentUser?.id ?? null;

  const {
    data: runtimes = [],
    isLoading: runtimesLoading,
    isSuccess: runtimesLoaded,
    isError: runtimesFailed,
  } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: workspaceSkills = [] } = useQuery({
    ...skillListOptions(wsId),
    enabled: options?.includeWorkspaceSkills !== false,
  });

  const [draft, setDraft] = useState<AgentDraft>(EMPTY_AGENT_DRAFT);

  const usableRuntimes = useMemo(
    () =>
      runtimes.filter(
        (runtime) =>
          runtime.status === "online" &&
          isRuntimeUsableForUser(runtime, currentUserId),
      ),
    [currentUserId, runtimes],
  );
  const selectedRuntime =
    runtimes.find((runtime) => runtime.id === draft.runtimeId) ?? null;

  // Seed the first usable runtime so a new draft is submittable without an
  // extra selection. Once the draft names a runtime, user or duplicate state
  // owns it and this effect leaves it alone.
  useEffect(() => {
    if (draft.runtimeId) return;
    const next = usableRuntimes[0]?.id || "";
    if (!next) return;
    setDraft((current) => ({ ...current, runtimeId: next }));
  }, [draft.runtimeId, usableRuntimes]);

  const accessInvalid =
    draft.permissionScope === "members" &&
    draft.memberIds.size === 0 &&
    draft.teamIds.size === 0;

  return {
    draft,
    setDraft,
    runtimes,
    runtimesLoading,
    runtimesSettled: runtimesLoaded || runtimesFailed,
    usableRuntimes,
    selectedRuntime,
    members,
    workspaceSkills,
    currentUserId,
    draftReady:
      selectedRuntime != null &&
      isRuntimeUsableForUser(selectedRuntime, currentUserId) &&
      isDraftDescriptionWithinLimit(draft.description) &&
      !accessInvalid,
  };
}
