"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { getCurrentWsId } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { Agent, ChatSession, ChatWorkingDirectory } from "@multica/core/types";

export function useChatWorkingDirectory(
  wsId: string,
  agent: Agent | null,
  session: ChatSession | null | undefined,
) {
  const userId = useAuthStore((s) => s.user?.id);
  const draft = useChatStore((s) => s.workingDirectoryDraft);
  const { data: runtimes = [], isSuccess } = useQuery(runtimeListOptions(wsId));
  const boundRuntime = runtimes.find((r) => r.id === agent?.runtime_id);
  const capabilities = boundRuntime?.metadata.capabilities;
  const runtime = boundRuntime && boundRuntime.runtime_mode === "local" &&
    boundRuntime.status === "online" && boundRuntime.daemon_id &&
    boundRuntime.owner_id === userId && Array.isArray(capabilities) &&
    capabilities.includes("chat-working-directory-v1") &&
    !(boundRuntime.provider === "openclaw" && agent?.runtime_config?.mode === "gateway")
    ? boundRuntime : null;
  const draftMatchesAgent = draft?.agentId === agent?.id && draft?.runtimeId === agent?.runtime_id;
  const draftMatchesMachine = !isSuccess || boundRuntime?.daemon_id === draft?.directory.daemon_id;

  useEffect(() => {
    if (getCurrentWsId() !== wsId) return;
    if (draft && (!draftMatchesAgent || !draftMatchesMachine)) {
      useChatStore.getState().setWorkingDirectoryDraft(null);
    }
  }, [wsId, draft, draftMatchesAgent, draftMatchesMachine]);

  const workingDirectory = session
    ? session.working_directory ?? null
    : draftMatchesAgent && draftMatchesMachine ? draft?.directory ?? null : null;

  const onChange = (directory: ChatWorkingDirectory | null) => {
    if (!agent || !runtime) return;
    if (directory && directory.daemon_id !== runtime.daemon_id) return;
    if (directory?.path === workingDirectory?.path && directory?.daemon_id === workingDirectory?.daemon_id) return;
    const state = useChatStore.getState();
    if (state.activeSessionId && !session) return;
    if (session) {
      state.setSelectedAgentId(session.agent_id);
      state.setSelectedProjectId(session.project_id ?? null);
      state.setActiveSession(null);
    }
    state.setWorkingDirectoryDraft(directory ? {
      agentId: agent.id, runtimeId: runtime.id, directory,
    } : null);
  };

  const carryToDraft = () => {
    useChatStore.getState().setWorkingDirectoryDraft(
      workingDirectory && agent?.runtime_id
        ? { agentId: agent.id, runtimeId: agent.runtime_id, directory: workingDirectory }
        : null,
    );
  };

  return { runtime, workingDirectory, onChange, carryToDraft };
}
