"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core";
import { projectListOptions } from "@multica/core/projects/queries";
import { setVoiceContextWords } from "@multica/core/voice";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";

/**
 * Offer the workspace's agent, member and project names to the speech model
 * as hotwords. Mount once inside a resolved workspace, next to
 * WorkspacePresencePrefetch.
 */
export function VoiceWorkspaceWords() {
  const wsId = useWorkspaceId();
  const { data: agents } = useQuery(agentListOptions(wsId));
  const { data: members } = useQuery(memberListOptions(wsId));
  const { data: projects } = useQuery(projectListOptions(wsId));

  useEffect(() => {
    setVoiceContextWords([
      ...(agents ?? []).filter((agent) => !agent.archived_at).map((agent) => agent.name),
      ...(members ?? []).map((member) => member.name),
      ...(projects ?? []).map((project) => project.title),
    ]);
  }, [agents, members, projects]);

  useEffect(() => () => setVoiceContextWords([]), [wsId]);

  return null;
}
