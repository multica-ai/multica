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
  AGENT_STARTER_PROMPT_LABEL_MAX_LENGTH,
  AGENT_STARTER_PROMPT_MAX_LENGTH,
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

interface RuntimeSeed {
  /**
   * False while the caller is still resolving which runtime this draft belongs
   * to. The form seeds nothing until it flips, so the list-order fallback can
   * never win a race against the authoritative answer.
   */
  ready: boolean;
  /** The runtime to adopt; empty falls back to the first usable one. */
  runtimeId: string;
}

const IMMEDIATE_RUNTIME_SEED: RuntimeSeed = { ready: true, runtimeId: "" };

/**
 * The state every agent-creation route shares: one draft plus the workspace
 * catalogs the form renders from.
 *
 * The draft itself is plain component state here. Making it survive navigation
 * is the caller's job, because the two routes answer it differently: the AI
 * route's configuration belongs to a conversation and lives on the server, the
 * manual route's has nothing to hang off and persists locally.
 */
export function useCreateAgentForm(options?: {
  /**
   * Where the runtime comes from for a draft that already exists somewhere. A
   * resumed builder conversation runs on the runtime its carrier is bound to,
   * and the picker must show THAT — showing the first usable one instead is how
   * MUL-5163 presented runtime A while every message ran on B.
   */
  runtimeSeed?: RuntimeSeed;
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
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));

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

  // Seeds the picker so a draft is submittable without a manual selection.
  // Only fills an empty slot: once the draft names a runtime — chosen by the
  // user, copied from a duplicate, resumed from the server — nothing may move
  // it.
  //
  // One effect decides, not two: the authoritative seed and the list-order
  // fallback resolve at different times, and letting them race is how the
  // picker ends up on a runtime that executes nothing.
  const seed = options?.runtimeSeed ?? IMMEDIATE_RUNTIME_SEED;
  const seedReady = seed.ready;
  const seedRuntimeId = seed.runtimeId;
  useEffect(() => {
    if (draft.runtimeId || !seedReady) return;
    const next = seedRuntimeId || usableRuntimes[0]?.id || "";
    if (!next) return;
    setDraft((current) => ({ ...current, runtimeId: next }));
  }, [draft.runtimeId, seedReady, seedRuntimeId, usableRuntimes]);

  const accessInvalid =
    draft.permissionScope === "members" &&
    draft.memberIds.size === 0 &&
    draft.teamIds.size === 0;

  const starterPromptsInvalid = draft.starterPrompts.some(
    (item) =>
      item.label.trim().length === 0 ||
      item.prompt.trim().length === 0 ||
      [...item.label].length > AGENT_STARTER_PROMPT_LABEL_MAX_LENGTH ||
      [...item.prompt].length > AGENT_STARTER_PROMPT_MAX_LENGTH,
  );

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
      !accessInvalid &&
      !starterPromptsInvalid,
  };
}
