"use client";

import { useEffect } from "react";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  AgentBuilderSessionSummary,
  RuntimeDevice,
} from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { BuilderSetup } from "./builder-conversation";
import { UnfinishedDraftsBanner } from "./unfinished-drafts";
import { useBuilderSession } from "./use-builder-session";
import { useCreateAgentForm } from "./use-create-agent-form";

/**
 * The step before a conversation exists: choose where it will run.
 *
 * The runtime cannot be deferred — it is frozen onto the hidden carrier agent
 * when the session is created, and it becomes the new agent's default runtime.
 * This pane owns a throwaway form for exactly those two fields; the real draft
 * belongs to the conversation and is created with it.
 */
export function BuilderSetupPanel({
  sessions,
  onResume,
  onStarted,
  onRuntimeLabel,
}: {
  /** Unfinished conversations, so this screen is not a dead end when some
   *  exist: the picker is the only way back to them from here. */
  sessions: AgentBuilderSessionSummary[];
  onResume: (sessionId: string) => void;
  /** Hands the new conversation's id and runtime back so the route can open it. */
  onStarted: (sessionId: string, runtimeId: string) => void;
  onRuntimeLabel: (runtime: RuntimeDevice | null) => void;
}) {
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const form = useCreateAgentForm();
  const { draft, setDraft, selectedRuntime } = form;

  const builder = useBuilderSession({
    sessionId: "",
    // Nothing is sent from this screen; the encoder is only reachable once a
    // conversation exists.
    encodeInput: (text) => text,
  });

  useEffect(() => {
    onRuntimeLabel(selectedRuntime);
  }, [onRuntimeLabel, selectedRuntime]);

  const startConversation = async () => {
    if (selectedRuntime?.status !== "online") return;
    const startedId = await builder.start(selectedRuntime.id, draft.model);
    if (startedId) onStarted(startedId, selectedRuntime.id);
  };

  return (
    <BuilderSetup
      banner={
        <UnfinishedDraftsBanner sessions={sessions} onResume={onResume} />
      }
      draft={draft}
      onChange={setDraft}
      runtimes={form.runtimes}
      runtimesLoading={form.runtimesLoading}
      members={form.members}
      currentUserId={form.currentUserId}
      selectedRuntime={selectedRuntime}
      starting={builder.starting}
      error={builder.error}
      onStart={() => void startConversation()}
      onConnectRuntime={() => navigation.push(paths.runtimes())}
    />
  );
}
