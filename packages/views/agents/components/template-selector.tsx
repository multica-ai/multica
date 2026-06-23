"use client";

import { useCallback } from "react";
import { Loader2, Settings2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentConfigTemplate, AgentTemplateBinding } from "@multica/core/types";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { configTemplateKeys } from "./config-template-manager";

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
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();

  const { data: systemTemplates, isLoading: loadingSystem } = useQuery({
    queryKey: configTemplateKeys.list(workspaceId, "system"),
    queryFn: () => api.listAgentConfigTemplates(workspaceId, "system"),
  });

  const { data: personalTemplates, isLoading: loadingPersonal } = useQuery({
    queryKey: configTemplateKeys.list(workspaceId, "personal"),
    queryFn: () => api.listAgentConfigTemplates(workspaceId, "personal"),
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

      api.updateAgentTemplateBinding(workspaceId, agentId, {
        system_template_id: isDefault ? null : isSkip ? null : value,
        personal_template_id: undefined, // no change
        skip_system_template: isSkip ? true : isDefault ? false : false,
      }).then((newBinding) => {
        onBindingChange(newBinding);
        queryClient.invalidateQueries({
          queryKey: configTemplateKeys.all(workspaceId),
        });
        toast.success(isSkip ? "已跳过系统模板" : "系统模板已更新");
      }).catch((err) => {
        toast.error("更新失败", {
          description: err instanceof Error ? err.message : "未知错误",
        });
      });
    },
    [workspaceId, agentId, onBindingChange, queryClient],
  );

  const handlePersonalChange = useCallback(
    (value: string | null) => {
      if (value === null) return;

      const isSkip = value === "__skip__";
      const isDefault = value === "__default__";

      api.updateAgentTemplateBinding(workspaceId, agentId, {
        system_template_id: undefined, // no change
        personal_template_id: isDefault ? null : isSkip ? null : value,
        skip_personal_template: isSkip ? true : isDefault ? false : false,
      }).then((newBinding) => {
        onBindingChange(newBinding);
        queryClient.invalidateQueries({
          queryKey: configTemplateKeys.all(workspaceId),
        });
        toast.success(isSkip ? "已跳过个人模板" : "个人模板已更新");
      }).catch((err) => {
        toast.error("更新失败", {
          description: err instanceof Error ? err.message : "未知错误",
        });
      });
    },
    [workspaceId, agentId, onBindingChange, queryClient],
  );

  const isLoading = loadingSystem || loadingPersonal;

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        加载模板...
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <Settings2 className="h-4 w-4 text-muted-foreground" />
        <span className="text-sm font-medium">配置模板</span>
      </div>

      <div className="space-y-3">
        {/* System template selector */}
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">系统模板</Label>
          <Select
            value={systemValue}
            onValueChange={handleSystemChange}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__default__">跟随默认</SelectItem>
              <SelectItem value="__skip__">不使用模板</SelectItem>
              {systemTemplates?.map((tpl) => (
                <SelectItem key={tpl.id} value={tpl.id}>
                  {tpl.name}
                  {tpl.is_default && " (默认)"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* Personal template selector */}
        <div className="space-y-1.5">
          <Label className="text-xs text-muted-foreground">个人模板</Label>
          <Select
            value={personalValue}
            onValueChange={handlePersonalChange}
          >
            <SelectTrigger className="h-8 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__default__">跟随默认</SelectItem>
              <SelectItem value="__skip__">不使用模板</SelectItem>
              {personalTemplates?.map((tpl) => (
                <SelectItem key={tpl.id} value={tpl.id}>
                  {tpl.name}
                  {tpl.is_default && " (默认)"}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>
  );
}
