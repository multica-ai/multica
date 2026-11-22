import type { QueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { chatKeys } from "../chat/queries";
import type { Agent, ChatSession } from "../types";

/**
 * The agent onboarding gives each member: an ordinary private agent they own.
 * Nothing marks it — any agent the member owns that is not archived and not a
 * system agent (Mika) counts, so a member who already made one is not asked
 * to make another.
 */
export function findPersonalAgent<T extends Pick<Agent, "owner_id" | "archived_at" | "system_key">>(
  agents: T[],
  userId: string,
): T | undefined {
  return agents.find(
    (agent) => agent.owner_id === userId && !agent.archived_at && !agent.system_key,
  );
}

/**
 * Agent names are unique per workspace (archived agents included), so the
 * default name gets a number when another member — or an earlier agent of
 * this one — already uses it.
 */
export function uniqueAgentName(base: string, takenNames: Iterable<string>): string {
  const taken = new Set([...takenNames].map((name) => name.trim().toLowerCase()));
  const trimmed = base.trim();
  if (!taken.has(trimmed.toLowerCase())) return trimmed;
  for (let n = 2; ; n++) {
    const candidate = `${trimmed} ${n}`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
}

export interface CreatePersonalAgentInput {
  /** Every call names its workspace: onboarding acts on one the app has not switched to. */
  workspaceSlug: string;
  workspaceId: string;
  /** Receives the new chat, so the chat page opening next does not drop it as unknown. */
  queryClient: QueryClient;
  runtimeId: string;
  name: string;
  description: string;
  /** Empty falls back to the runtime's default model. */
  model?: string;
}

const MAX_NAME_ATTEMPTS = 5;

function isNameConflict(err: unknown): boolean {
  return err instanceof ApiError && err.status === 409;
}

/**
 * Creates the member's private agent on the given runtime and opens a chat
 * with it. A name taken between loading the form and submitting it (another
 * member onboarding at the same moment) is retried with the next free number
 * instead of failing the step.
 */
export async function createPersonalAgent(
  input: CreatePersonalAgentInput,
): Promise<{ agent: Agent; chatSession: ChatSession }> {
  let name = input.name.trim();
  const tried: string[] = [];
  let agent: Agent | null = null;
  for (let attempt = 0; attempt < MAX_NAME_ATTEMPTS && !agent; attempt++) {
    try {
      agent = await api.createAgent(
        {
          name,
          description: input.description,
          runtime_id: input.runtimeId,
          visibility: "private",
          ...(input.model ? { model: input.model } : {}),
          template: "onboarding_personal",
        },
        input.workspaceSlug,
      );
    } catch (err) {
      if (!isNameConflict(err) || attempt === MAX_NAME_ATTEMPTS - 1) throw err;
      tried.push(name);
      name = uniqueAgentName(input.name, tried);
    }
  }
  if (!agent) throw new Error("Could not create the agent");
  const chatSession = await api.createChatSession({ agent_id: agent.id }, input.workspaceSlug);
  // The sessions list never goes stale on its own, and the chat page clears an
  // active session it cannot find there.
  const sessionsKey = chatKeys.sessions(input.workspaceId);
  input.queryClient.setQueryData<ChatSession[]>(sessionsKey, (current) =>
    current ? [chatSession, ...current.filter((session) => session.id !== chatSession.id)] : current,
  );
  void input.queryClient.invalidateQueries({ queryKey: sessionsKey });
  return { agent, chatSession };
}
