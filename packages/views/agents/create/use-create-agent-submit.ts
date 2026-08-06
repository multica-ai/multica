"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { buildCreateAgentRequest, type AgentDraft } from "@multica/core/agents";
import { api, ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { Agent } from "@multica/core/types";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

interface AgentCreateErrors {
  nameError: string | null;
  formError: string | null;
}

export function classifyAgentCreateError(
  error: unknown,
  fallbackMessage: string,
  conflictMessage: string,
): AgentCreateErrors {
  const message =
    error instanceof Error && error.message ? error.message : fallbackMessage;
  return error instanceof ApiError && error.status === 409
    ? { nameError: conflictMessage, formError: null }
    : { nameError: null, formError: message };
}

/**
 * Commits the draft and leaves the creation flow.
 *
 * Deliberately not optimistic: the flow navigates away on success, so the agent
 * has to exist before the destination renders. A failed squad join is reported
 * as a warning instead of failing the create — the agent is already committed
 * at that point and a retry would duplicate it.
 */
export function useCreateAgentSubmit(options: {
  draft: AgentDraft;
  runtimeId: string | null;
  squadId: string | null;
  /** Attribution for the `agent_created` event, not a template create. */
  template?: string;
  duplicateSource?: Agent | null;
  /** Runs after the agent is committed, before navigation. */
  onCreated?: (agent: Agent) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();

  const [creating, setCreating] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const { draft, runtimeId, squadId, template, duplicateSource, onCreated } =
    options;

  const create = async () => {
    if (!runtimeId || creating) return;
    setCreating(true);
    setNameError(null);
    setFormError(null);
    try {
      const agent = await api.createAgent(
        buildCreateAgentRequest({
          draft,
          runtimeId,
          template,
          duplicateSource,
        }),
      );
      if (!agent.id) throw new Error(t(($) => $.creation_studio.create_failed));

      if (squadId) {
        try {
          await api.addSquadMember(squadId, {
            member_type: "agent",
            member_id: agent.id,
          });
          await Promise.all([
            qc.invalidateQueries({
              queryKey: [...workspaceKeys.squads(wsId), squadId, "members"],
            }),
            qc.invalidateQueries({
              queryKey: [...workspaceKeys.squads(wsId), squadId],
            }),
          ]);
        } catch (error) {
          toast.warning(
            t(($) => $.create_dialog.squad_join_failed_toast, {
              name: agent.name || draft.name.trim(),
              error: error instanceof Error ? error.message : "unknown error",
            }),
          );
        }
      }

      await onCreated?.(agent);
      await qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(
        t(($) => $.creation_studio.created, {
          name: agent.name || draft.name.trim(),
        }),
      );
      navigation.push(
        squadId ? paths.squadDetail(squadId) : paths.agentDetail(agent.id),
      );
    } catch (error) {
      const nextErrors = classifyAgentCreateError(
        error,
        t(($) => $.creation_studio.create_failed),
        t(($) => $.creation_studio.name_conflict),
      );
      setNameError(nextErrors.nameError);
      setFormError(nextErrors.formError);
      setCreating(false);
    }
  };

  return {
    create,
    creating,
    nameError,
    formError,
    clearNameError: () => setNameError(null),
  };
}
