import { useQuery } from "@tanstack/react-query";
import type { AutomationTemplate } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { hasStoredSessionToken } from "@/features/auth/queries";
import { useWorkspaceStore } from "@/features/workspace";

/** Returns all built-in automation templates with workspace-specific enablement state. */
export function useAutomationTemplates() {
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id);

  return useQuery<AutomationTemplate[]>({
    queryKey: workspaceId
      ? queryKeys.automation.templates(workspaceId)
      : ["automation", "templates", "__no_workspace__"],
    queryFn: () => api.listAutomationTemplates(),
    staleTime: 30 * 1000,
    enabled: Boolean(workspaceId) && hasStoredSessionToken(),
  });
}
