"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentBuilderSessionListOptions } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import type { RuntimeDevice } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { BuilderSetupPanel } from "./builder-setup-panel";
import { BuilderWorkspace } from "./builder-workspace";
import { AgentCreateChip, AgentCreateShell } from "./create-shell";
import { createPathWithParams } from "./squad-param";

/**
 * Conversational agent creation.
 *
 * The route holds the conversation list and the identity of the open one; each
 * conversation's own state lives in {@link BuilderWorkspace}, mounted under a
 * `key` so switching is a remount rather than a reset every future field would
 * have to remember to join.
 *
 * `?session=` is what makes a conversation addressable — a refresh, a
 * back/forward, and a reopened tab all land back in the same chat instead of
 * silently starting a new one.
 */
export function AiCreateAgentPage() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const squadId = navigation.searchParams.get("squad");
  const sessionId = navigation.searchParams.get("session") ?? "";

  const builderSessions = useQuery(agentBuilderSessionListOptions(wsId));
  const sessions = builderSessions.data ?? [];
  const sessionSettled = builderSessions.isSuccess || builderSessions.isError;
  const openSession = sessions.find(
    (session) => session.session_id === sessionId,
  );

  // The chip has to say something before the list answers, and only the open
  // pane knows which runtime it settled on.
  const [runtimeLabel, setRuntimeLabel] = useState<string | null>(null);
  // The runtime a just-started conversation was created with. It cannot be read
  // back yet — a conversation joins the list only once it has a message — and
  // without it the first turn would render against the first runtime in the
  // list instead of the one the user picked.
  const [startedRuntimeId, setStartedRuntimeId] = useState("");

  const handleSetupRuntimeLabel = useCallback(
    (runtime: RuntimeDevice | null) =>
      setRuntimeLabel(runtime ? runtimeDisplayLabel(runtime) : null),
    [],
  );

  const open = useCallback(
    (nextSessionId: string) =>
      navigation.replace(
        createPathWithParams(paths.newAgentAi(), {
          squad: squadId,
          session: nextSessionId,
        }),
      ),
    [navigation, paths, squadId],
  );

  const closeSession = useCallback(
    () =>
      navigation.replace(
        createPathWithParams(paths.newAgentAi(), { squad: squadId }),
      ),
    [navigation, paths, squadId],
  );

  return (
    <AgentCreateShell
      title={
        squadId
          ? t(($) => $.creation_studio.squad_title)
          : t(($) => $.creation_studio.title)
      }
      step={t(($) => $.creation_studio.step_ai)}
      onBack={() => navigation.push(paths.newAgent())}
      chips={
        <>
          <AgentCreateChip>
            {t(($) => $.creation_studio.modes.ai.title)}
          </AgentCreateChip>
          {runtimeLabel ? (
            <AgentCreateChip>{runtimeLabel}</AgentCreateChip>
          ) : null}
        </>
      }
    >
      {sessionId ? (
        <BuilderWorkspace
          // Switching conversations remounts everything below: the draft, the
          // applied-message marker and the composer all belong to one
          // conversation, and a remount is the only reset that cannot forget a
          // field.
          key={sessionId}
          sessionId={sessionId}
          squadId={squadId}
          session={openSession}
          sessionSettled={sessionSettled}
          fallbackRuntimeId={startedRuntimeId}
          onDiscarded={closeSession}
          onRuntimeLabel={setRuntimeLabel}
        />
      ) : (
        <BuilderSetupPanel
          sessions={sessions}
          onResume={(nextSessionId) => {
            setStartedRuntimeId("");
            open(nextSessionId);
          }}
          onStarted={(nextSessionId, runtimeId) => {
            setStartedRuntimeId(runtimeId);
            open(nextSessionId);
          }}
          onRuntimeLabel={handleSetupRuntimeLabel}
        />
      )}
    </AgentCreateShell>
  );
}
