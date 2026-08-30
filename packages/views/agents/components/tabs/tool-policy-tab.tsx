import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentToolPolicyOptions,
  operationalControlKeys,
  type ReplaceAgentToolPolicyRequest,
} from "@multica/core/operational-controls";
import { useT } from "../../../i18n";
import { ToolPolicyEditor } from "../inspector/tool-policy-editor";

export function ToolPolicyTab({ agent, canEdit }: { agent: Agent; canEdit: boolean }) {
  const { t } = useT("operations");
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  const policy = useQuery(agentToolPolicyOptions(workspaceId, agent.id));
  const save = useMutation({
    mutationFn: (request: ReplaceAgentToolPolicyRequest) => api.replaceAgentToolPolicy(agent.id, request),
    onSuccess: (next) => queryClient.setQueryData(operationalControlKeys.policy(workspaceId, agent.id), next),
  });

  if (policy.isPending) return <div className="h-24 animate-pulse rounded-lg bg-muted" />;
  if (!policy.data) return <p className="text-body text-destructive">{t(($) => $.policy.load_failed)}</p>;

  return <ToolPolicyEditor policy={policy.data} canEdit={canEdit} onSave={(request) => save.mutateAsync(request).then(() => undefined)} />;
}
