import { useState } from "react";
import { Alert } from "react-native";
import { StackActions, useNavigation } from "@react-navigation/native";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useCreateChatSession } from "@multica/core/chat/mutations";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import type { Agent } from "@multica/core/types";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useWorkspace } from "../../model/workspace-context";
import { useStrings } from "../../strings";
import { NewChatView } from "./NewChatView";

export function NewChatScreen() {
  const navigation = useNavigation();
  const strings = useStrings();
  const { workspace } = useWorkspace();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const agents = useQuery(agentListOptions(workspace.id));
  const members = useQuery(memberListOptions(workspace.id));
  const createSession = useCreateChatSession();
  const [creatingId, setCreatingId] = useState<string | null>(null);

  const role = members.data?.find((member) => member.user_id === userId)?.role;
  const context = { userId, role: role === "owner" || role === "admin" || role === "member" ? role : null };
  const usable = (agents.data ?? []).filter((agent: Agent) => !agent.archived_at && canAssignAgentToIssue(agent, context).allowed);

  return (
    <NewChatView
      state={agents.data ? "ready" : agents.isError ? "error" : "loading"}
      agents={usable.map((agent) => ({ id: agent.id, actor: { kind: "agent", name: agent.name, avatarUrl: resolvePublicFileUrl(agent.avatar_url) }, description: agent.description }))}
      creatingId={creatingId}
      onRetry={() => void agents.refetch()}
      onPick={async (agent) => {
        setCreatingId(agent.id);
        try {
          const session = await createSession.mutateAsync({ agent_id: agent.id });
          navigation.dispatch(StackActions.replace("Chat", { sessionId: session.id }));
        } catch {
          Alert.alert(strings.common.loadFailed);
          setCreatingId(null);
        }
      }}
    />
  );
}
