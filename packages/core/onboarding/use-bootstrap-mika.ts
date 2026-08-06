import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { chatKeys } from "../chat/queries";
import type { ChatPendingTask, ChatSession, MikaBootstrapResponse } from "../types";
import { workspaceKeys } from "../workspace/queries";

export type MikaOnboardingLanguage = "en" | "zh" | "ko" | "ja";

export interface BootstrapMikaInput {
  /**
   * Slug of the workspace being provisioned. Every call below sends it
   * explicitly instead of relying on the global current-workspace singleton:
   * this flow acts on a workspace the app has not navigated to yet, and on
   * desktop the tab system writes that singleton too — it reclaimed it
   * mid-flow and Mika was created in the previously-active workspace.
   */
  workspaceSlug: string;
  runtimeId: string;
  /** Runtime model for Mika. Empty falls back to the runtime's own default. */
  model?: string;
  /** Localized title for the opening conversation. */
  title: string;
  language: MikaOnboardingLanguage;
  /** Set when the member has onboarded in another workspace before, so Mika
   *  opens with one line instead of re-explaining the product. */
  returning?: boolean;
}

export interface BootstrapMikaResult {
  agent: MikaBootstrapResponse;
  chatSession: ChatSession;
}

/**
 * Creates or reuses the workspace's Mika and starts its opening conversation.
 *
 * Every step is idempotent server-side, so a retry, a double-submit, or two
 * clients racing the same workspace converge on one agent, one session, and
 * one opening turn — the agent and session under their own advisory locks, the
 * opening turn under the session lock. Nothing about Mika's configuration is
 * decided here; the server owns it, which is why this can no longer drift from
 * the product definition the way a client-built payload could.
 */
export async function bootstrapMika(
  input: BootstrapMikaInput,
): Promise<
  BootstrapMikaResult & {
    kickoff: Awaited<ReturnType<typeof api.startMikaOnboarding>>;
  }
> {
  const agent = await api.createMikaAgent(
    {
      runtime_id: input.runtimeId,
      language: input.language,
      model: input.model,
      session_title: input.title,
    },
    input.workspaceSlug,
  );

  // The server resolves agent and session together. This used to list the
  // member's sessions and create one when it saw no title match — a
  // check-then-insert with nothing serializing it, so two tabs each opened
  // their own onboarding conversation, and switching language between a
  // failed attempt and its retry opened another (the title is localized).
  const chatSession = agent.onboarding_session;
  if (!chatSession) {
    throw new Error("Mika onboarding session was not returned");
  }

  const kickoff = await api.startMikaOnboarding(
    chatSession.id,
    {
      language: input.language,
      returning: input.returning ?? false,
    },
    input.workspaceSlug,
  );

  return { agent, chatSession, kickoff };
}

export function useBootstrapMika(workspaceId: string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: BootstrapMikaInput) => bootstrapMika(input),
    onSuccess: ({ chatSession, kickoff }) => {
      if (kickoff.started && kickoff.task_id) {
        queryClient.setQueryData<ChatPendingTask>(
          chatKeys.pendingTask(chatSession.id),
          {
            task_id: kickoff.task_id,
            status: "queued",
            created_at: kickoff.created_at,
          },
        );
      }
      return Promise.all([
        queryClient.invalidateQueries({
          queryKey: workspaceKeys.agents(workspaceId),
        }),
        queryClient.invalidateQueries({
          queryKey: chatKeys.sessions(workspaceId),
        }),
        queryClient.invalidateQueries({
          queryKey: chatKeys.pendingTask(chatSession.id),
        }),
      ]);
    },
  });
}
