"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentBuilderSessionListOptions } from "@multica/core/agents";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useBackOrReplace, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { BuilderWorkspace } from "./builder-workspace";
import { AgentCreateChip, AgentCreateShell } from "./create-shell";
import { withSquadParam } from "./squad-param";

/**
 * One creation conversation, addressed by its own session id.
 *
 * The id is in the path, not in component state or a query param, because this
 * conversation outlives the screen that started it: leaving the studio no
 * longer deletes it (#6307), so a refresh, a back/forward, a reopened tab and a
 * direct link all have to land back in the same chat.
 */
export function AiBuilderSessionPage({ sessionId }: { sessionId: string }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const backOrReplace = useBackOrReplace();
  const squadId = navigation.searchParams.get("squad");
  // The runtime this conversation was just started on. It cannot be read back
  // for the first turn — a conversation joins the list only once it has a
  // message or a saved configuration — so the starting screen hands it over in
  // the URL. Absent on every later visit, by which point the list answers.
  const startedRuntimeId = navigation.searchParams.get("runtime") ?? "";

  const builderSessions = useQuery(agentBuilderSessionListOptions(wsId));
  const sessions = builderSessions.data ?? [];
  const sessionSettled = builderSessions.isSuccess || builderSessions.isError;
  const session = sessions.find((row) => row.session_id === sessionId);

  // Only the open conversation knows which runtime it settled on.
  const [runtimeLabel, setRuntimeLabel] = useState<string | null>(null);

  // The conversation is gone — discarded here, deleted from another tab, or a
  // link that outlived it. Replace rather than push: the address no longer
  // resolves, so it must not stay on the stack for a back to land on.
  const leave = useCallback(
    () => navigation.replace(withSquadParam(paths.newAgentAi(), squadId)),
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
      onBack={() => backOrReplace(paths.newAgent())}
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
      <BuilderWorkspace
        // Switching conversations remounts everything below: the draft, the
        // applied-message marker and the composer all belong to one
        // conversation, and a remount is the only reset that cannot forget a
        // field.
        key={sessionId}
        sessionId={sessionId}
        squadId={squadId}
        session={session}
        sessionSettled={sessionSettled}
        fallbackRuntimeId={startedRuntimeId}
        onDiscarded={leave}
        onRuntimeLabel={setRuntimeLabel}
      />
    </AgentCreateShell>
  );
}
