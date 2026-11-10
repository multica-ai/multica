"use client";

import { queryOptions } from "@tanstack/react-query";
import { create } from "zustand";
import { api } from "../api";
import { issueKeys } from "../issues/queries";
import { useAuthStore } from "../auth";
import type { User } from "../types";

const DAY_MS = 24 * 60 * 60 * 1000;

// Mirrors server/internal/handler/star_prompt.go, which has the final say.
// The client checks first only so it does not ask for a showing it cannot get.
export const STAR_PROMPT_MAX_SHOWINGS = 2;
export const STAR_PROMPT_COOLDOWN_MS = 30 * DAY_MS;
export const STAR_PROMPT_MIN_ACCOUNT_AGE_MS = 3 * DAY_MS;
/** Done issues the agent needs before its work is worth celebrating. */
export const STAR_PROMPT_MIN_AGENT_DONE_ISSUES = 3;

type StarPromptUser = Pick<
  User,
  | "created_at"
  | "star_prompt_shown_count"
  | "star_prompt_last_shown_at"
  | "star_prompt_starred_at"
>;

/**
 * Whether the user may be shown the star prompt now, by account age and the
 * showing history. False when the server cannot record showings
 * (`star_prompt_shown_count` undefined): a prompt it cannot count would come
 * back forever.
 */
export function starPromptDue(user: StarPromptUser | null, now: number): boolean {
  if (!user || user.star_prompt_shown_count === undefined) return false;
  if (user.star_prompt_starred_at) return false;
  if (user.star_prompt_shown_count >= STAR_PROMPT_MAX_SHOWINGS) return false;
  const createdAt = Date.parse(user.created_at);
  if (!Number.isFinite(createdAt) || now - createdAt < STAR_PROMPT_MIN_ACCOUNT_AGE_MS) {
    return false;
  }
  if (user.star_prompt_last_shown_at) {
    const lastShown = Date.parse(user.star_prompt_last_shown_at);
    if (!Number.isFinite(lastShown) || now - lastShown < STAR_PROMPT_COOLDOWN_MS) return false;
  }
  return true;
}

/** The current user just moved an agent's issue to a done status. */
export interface StarPromptMoment {
  wsId: string;
  agentId: string;
  issueId: string;
}

interface StarPromptStore {
  moment: StarPromptMoment | null;
  reportAgentIssueDone: (moment: StarPromptMoment) => void;
  clearMoment: () => void;
}

export const useStarPromptStore = create<StarPromptStore>((set) => ({
  moment: null,
  reportAgentIssueDone: (moment) => set({ moment }),
  clearMoment: () => set({ moment: null }),
}));

/** Issues assigned to one agent in the workspace whose status is in the done category. */
export function agentDoneIssueCountOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: [...issueKeys.all(wsId), "done-count-by-agent", agentId] as const,
    queryFn: async () => {
      const res = await api.listIssues({
        workspace_id: wsId,
        status_categories: ["done"],
        assignee_filters: [{ type: "agent", id: agentId }],
        limit: 1,
      });
      return res.total;
    },
    staleTime: 0,
  });
}

function applyUser(requesterId: string, user: User) {
  const current = useAuthStore.getState().user;
  // The response can land after a logout or an account switch.
  if (current?.id !== requesterId || user.id !== requesterId) return;
  useAuthStore.getState().setUser({
    ...current,
    star_prompt_shown_count: user.star_prompt_shown_count,
    star_prompt_last_shown_at: user.star_prompt_last_shown_at,
    star_prompt_starred_at: user.star_prompt_starred_at,
  });
}

/**
 * Asks the server for one showing. True means show the prompt now; false means
 * another tab or device took it, the deployment turned the prompt off, or the
 * request failed.
 */
export async function claimStarPromptShowing(): Promise<boolean> {
  const requester = useAuthStore.getState().user;
  if (!requester) return false;
  try {
    applyUser(requester.id, await api.claimStarPromptShowing());
    return true;
  } catch {
    return false;
  }
}

/** Retires the prompt for good once the user clicks through to GitHub. */
export async function markStarPromptStarred(): Promise<void> {
  const requester = useAuthStore.getState().user;
  if (!requester) return;
  applyUser(requester.id, { ...requester, star_prompt_starred_at: new Date().toISOString() });
  applyUser(requester.id, await api.markStarPromptStarred());
}
