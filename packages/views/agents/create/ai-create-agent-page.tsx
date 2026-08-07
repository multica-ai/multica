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
import { AgentCreateChip, AgentCreateShell } from "./create-shell";
import { createPathWithParams } from "./squad-param";

/**
 * Starting a conversational agent creation: pick where it will run.
 *
 * This route is the ACTION — one screen, one job. The conversation it produces
 * is a durable object and lives at its own address (ai-builder-session-page),
 * because it is left and resumed rather than completed in one sitting. Both
 * used to share this route, switched on a `?session=` param, which made the
 * route render two unrelated screens and left the conversation — the thing
 * with a lifetime — as a modifier on the thing without one.
 *
 * Entering a conversation REPLACES this entry. The runtime choice is consumed
 * the moment the session exists — it is frozen onto the hidden carrier agent
 * and cannot be revisited from here — so leaving it on the history stack would
 * only offer to start a second conversation on the way back.
 */
export function AiCreateAgentPage() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const squadId = navigation.searchParams.get("squad");

  const builderSessions = useQuery(agentBuilderSessionListOptions(wsId));
  const sessions = builderSessions.data ?? [];

  // The chip has to say something before a runtime is settled on.
  const [runtimeLabel, setRuntimeLabel] = useState<string | null>(null);
  const handleRuntimeLabel = useCallback(
    (runtime: RuntimeDevice | null) =>
      setRuntimeLabel(runtime ? runtimeDisplayLabel(runtime) : null),
    [],
  );

  const open = useCallback(
    (sessionId: string, runtimeId: string | null) =>
      navigation.replace(
        createPathWithParams(paths.newAgentAiSession(sessionId), {
          squad: squadId,
          runtime: runtimeId,
        }),
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
      <BuilderSetupPanel
        sessions={sessions}
        // Resuming carries no runtime seed: a listed conversation reports its
        // own, which is the truthful answer after a runtime switch.
        onResume={(sessionId) => open(sessionId, null)}
        onStarted={(sessionId, runtimeId) => open(sessionId, runtimeId)}
        onRuntimeLabel={handleRuntimeLabel}
      />
    </AgentCreateShell>
  );
}
