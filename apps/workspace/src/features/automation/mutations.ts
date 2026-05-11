import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { StandupSummaryResult } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

/** Provides enable, disable, and run mutations for automation templates. */
export function useAutomationMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id ?? null);

  function invalidateTemplates() {
    if (!workspaceId) return;
    queryClient.invalidateQueries({ queryKey: queryKeys.automation.templates(workspaceId) });
  }

  const enableMutation = useMutation({
    mutationFn: (templateId: string) => api.enableAutomationRule(templateId),
    onSuccess: invalidateTemplates,
  });

  const disableMutation = useMutation({
    mutationFn: (templateId: string) => api.disableAutomationRule(templateId),
    onSuccess: invalidateTemplates,
  });

  const runMutation = useMutation<StandupSummaryResult, Error, string>({
    mutationFn: (templateId: string) => api.runAutomationTemplate(templateId),
  });

  return {
    enableAutomation: (templateId: string) => enableMutation.mutateAsync(templateId),
    disableAutomation: (templateId: string) => disableMutation.mutateAsync(templateId),
    runAutomation: (templateId: string) => runMutation.mutateAsync(templateId),
    isEnabling: enableMutation.isPending,
    isDisabling: disableMutation.isPending,
    isRunning: runMutation.isPending,
    runResult: runMutation.data ?? null,
  };
}
