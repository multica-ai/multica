"use client";

import { useCallback } from "react";
import { Loader2, Settings2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentTemplateBinding } from "@multica/core/types";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { configTemplateKeys } from "./config-template-keys";
import { useT } from "../../i18n";

// ─── Template selector for agent detail ──────────────────────────────────────

interface TemplateSelectorProps {
  agentId: string;
  binding: AgentTemplateBinding | undefined;
  onBindingChange: (binding: AgentTemplateBinding) => void;
}

export function TemplateSelector({
  agentId,
  binding,
  onBindingChange,
}: TemplateSelectorProps) {
  const { t } = useT("agents");
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();

  const { data: systemTemplates, isLoading: loadingSystem } = useQuery({
    queryKey: configTemplateKeys.list(workspaceId, "system"),
    queryFn: () => api.listAgentConfigTemplates("system"),
  });

  const { data: personalTemplates, isLoading: loadingPersonal } = useQuery({
    queryKey: configTemplateKeys.list(workspaceId, "personal"),
    queryFn: () => api.listAgentConfigTemplates("personal"),
  });

  // Determine current system value
  const systemValue = binding?.skip_system_template
    ? "__skip__"
    : binding?.system_template_id || "__default__";

  // Determine current personal value
  const personalValue = binding?.skip_personal_template
    ? "__skip__"
    : binding?.personal_template_id || "__default__";

  const handleSystemChange = useCallback(
    (value: string | null) => {
      if (value === null) return;

      const isSkip = value === "__skip__";
      const isDefault = value === "__default__";

      api.updateAgentTemplateBinding(agentId, {
        system_template_id: isDefault ? null : isSkip ? null : value,
        personal_template_id: undefined, // no change
        skip_system_template: isSkip ? true : isDefault ? false : false,
      }).then((newBinding) => {
        onBindingChange(newBinding);
        queryClient.invalidateQueries({
          queryKey: configTemplateKeys.all(workspaceId),
        });
        toast.success(isSkip ? t(($) => $.template.skipped_system) : t(($) => $.template.updated_system));
      }).catch((err) => {
        toast.error(t(($) => $.template.update_failed), {
          description: err instanceof Error ? err.message : String(err),
        });
      });
    },
    [workspaceId, agentId, onBindingChange, queryClient, t],
  );

  const handlePersonalChange = useCallback(
    (value: string | null) => {
      if (value === null) return;

      const isSkip = value === "__skip__";
      const isDefault = value === "__default__";

      api.updateAgentTemplateBinding(agentId, {
        system_template_id: undefined, // no change
        personal_template_id: isDefault ? null : isSkip ? null : value,
        skip_personal_template: isSkip ? true : isDefault ? false : false,
      }).then((newBinding) => {
        onBindingChange(newBinding);
        queryClient.invalidateQueries({
          queryKey: configTemplateKeys.all(workspaceId),
        });
        toast.success(isSkip ? t(($) => $.template.skipped_personal) : t(($) => $.template.updated_personal));
      }).catch((err) => {
        toast.error(t(($) => $.template.update_failed), {
          description: err instanceof Error ? err.message : String(err),
        });
      });
    },
    [workspaceId, agentId, onBindingChange, queryClient, t],
  );

  const isLoading = loadingSystem || loadingPersonal;

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        {t(($) => $.template.loading)}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Settings2 className="h-4 w-4 text-muted-foreground" />
        <span className="text-sm font-medium">{t(($) => $.template.section_title)}</span>
      </div>

      <div className="space-y-3">
        {/* System template selector */}
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t(($) => $.template.system_label)}</Label>
          <Select
            value={systemValue}
            onValueChange={handleSystemChange}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__default__">{t(($) => $.template.follow_default)}</SelectItem>
              <SelectItem value="__skip__">{t(($) => $.template.skip)}</SelectItem>
              {systemTemplates?.map((tpl) => (
                <SelectItem key={tpl.id} value={tpl.id}>
                  {tpl.name}
                  {tpl.is_default && ` (${t(($) => $.template.default_badge)})`}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Personal template selector */}
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">{t(($) => $.template.personal_label)}</Label>
          <Select
            value={personalValue}
            onValueChange={handlePersonalChange}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__default__">{t(($) => $.template.follow_default)}</SelectItem>
              <SelectItem value="__skip__">{t(($) => $.template.skip)}</SelectItem>
              {personalTemplates?.map((tpl) => (
                <SelectItem key={tpl.id} value={tpl.id}>
                  {tpl.name}
                  {tpl.is_default && ` (${t(($) => $.template.default_badge)})`}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>
  );
}
