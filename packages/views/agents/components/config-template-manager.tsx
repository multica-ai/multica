"use client";

import { useState, useCallback } from "react";
import { Loader2, Plus, Settings2, Trash2, Star, StarOff } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type {
  AgentConfigTemplate,
  CreateAgentConfigTemplateRequest,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Badge } from "@multica/ui/components/ui/badge";

// ─── Query keys ──────────────────────────────────────────────────────────────

export const configTemplateKeys = {
  all: (workspaceId: string) =>
    ["agent-config-templates", workspaceId] as const,
  list: (workspaceId: string, scope?: string) =>
    [...configTemplateKeys.all(workspaceId), "list", scope] as const,
  detail: (workspaceId: string, templateId: string) =>
    [...configTemplateKeys.all(workspaceId), "detail", templateId] as const,
};

// ─── Template list component ─────────────────────────────────────────────────

interface ConfigTemplateManagerProps {
  scope: "system" | "personal";
  title: string;
  description?: string;
}

export function ConfigTemplateManager({
  scope,
  title,
  description,
}: ConfigTemplateManagerProps) {
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<AgentConfigTemplate | null>(
    null,
  );

  const { data: templates, isLoading } = useQuery({
    queryKey: configTemplateKeys.list(workspaceId, scope),
    queryFn: () => api.listAgentConfigTemplates(workspaceId, scope),
  });

  const handleCreated = useCallback(() => {
    queryClient.invalidateQueries({
      queryKey: configTemplateKeys.all(workspaceId),
    });
    setShowCreate(false);
  }, [queryClient, workspaceId]);

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return;
    try {
      await api.deleteAgentConfigTemplate(workspaceId, deleteTarget.id);
      queryClient.invalidateQueries({
        queryKey: configTemplateKeys.all(workspaceId),
      });
      toast.success("模板已删除");
    } catch (err) {
      toast.error("删除失败", {
        description: err instanceof Error ? err.message : "未知错误",
      });
    } finally {
      setDeleteTarget(null);
    }
  }, [deleteTarget, queryClient, workspaceId]);

  const handleToggleDefault = useCallback(
    async (tpl: AgentConfigTemplate) => {
      try {
        await api.updateAgentConfigTemplate(workspaceId, tpl.id, {
          is_default: !tpl.is_default,
        });
        queryClient.invalidateQueries({
          queryKey: configTemplateKeys.all(workspaceId),
        });
        toast.success(tpl.is_default ? "已取消默认" : "已设为默认");
      } catch (err) {
        toast.error("操作失败", {
          description: err instanceof Error ? err.message : "未知错误",
        });
      }
    },
    [queryClient, workspaceId],
  );

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">{title}</h3>
          {description && (
            <p className="text-xs text-muted-foreground">{description}</p>
          )}
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => setShowCreate(true)}
        >
          <Plus className="mr-1 h-3.5 w-3.5" />
          新建模板
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-8">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : templates && templates.length > 0 ? (
        <div className="space-y-2">
          {templates.map((tpl) => (
            <TemplateCard
              key={tpl.id}
              template={tpl}
              onDelete={() => setDeleteTarget(tpl)}
              onToggleDefault={() => handleToggleDefault(tpl)}
            />
          ))}
        </div>
      ) : (
        <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
          暂无模板
        </div>
      )}

      {/* Create dialog */}
      <CreateTemplateDialog
        open={showCreate}
        scope={scope}
        onClose={() => setShowCreate(false)}
        onCreated={handleCreated}
      />

      {/* Delete confirmation */}
      <AlertDialog
        open={!!deleteTarget}
        onOpenChange={() => setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除模板「{deleteTarget?.name}」吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// ─── Template card ───────────────────────────────────────────────────────────

function TemplateCard({
  template,
  onDelete,
  onToggleDefault,
}: {
  template: AgentConfigTemplate;
  onDelete: () => void;
  onToggleDefault: () => void;
}) {
  return (
    <div className="flex items-center gap-3 rounded-lg border p-3">
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-medium text-sm truncate">{template.name}</span>
          {template.is_default && (
            <Badge variant="secondary" className="text-xs">
              默认
            </Badge>
          )}
        </div>
        {template.description && (
          <p className="text-xs text-muted-foreground truncate mt-0.5">
            {template.description}
          </p>
        )}
      </div>
      <div className="flex items-center gap-1">
        <Button
          size="icon"
          variant="ghost"
          className="h-7 w-7"
          onClick={onToggleDefault}
          title={template.is_default ? "取消默认" : "设为默认"}
        >
          {template.is_default ? (
            <Star className="h-3.5 w-3.5 fill-yellow-400 text-yellow-400" />
          ) : (
            <StarOff className="h-3.5 w-3.5 text-muted-foreground" />
          )}
        </Button>
        <Button
          size="icon"
          variant="ghost"
          className="h-7 w-7 text-destructive"
          onClick={onDelete}
          title="删除"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      </div>
    </div>
  );
}

// ─── Create dialog ───────────────────────────────────────────────────────────

function CreateTemplateDialog({
  open,
  scope,
  onClose,
  onCreated,
}: {
  open: boolean;
  scope: "system" | "personal";
  onClose: () => void;
  onCreated: () => void;
}) {
  const workspaceId = useWorkspaceId();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [config, setConfig] = useState("{}");
  const [isDefault, setIsDefault] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = useCallback(async () => {
    if (!name.trim()) {
      setError("名称不能为空");
      return;
    }
    let parsedConfig: Record<string, unknown>;
    try {
      parsedConfig = JSON.parse(config);
    } catch {
      setError("配置 JSON 格式错误");
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const req: CreateAgentConfigTemplateRequest = {
        scope,
        name: name.trim(),
        description: description.trim(),
        config: parsedConfig,
        is_default: isDefault,
      };
      await api.createAgentConfigTemplate(workspaceId, req);
      toast.success("模板已创建");
      onCreated();
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建失败");
    } finally {
      setSubmitting(false);
    }
  }, [name, description, config, isDefault, scope, workspaceId, onCreated]);

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            新建{scope === "system" ? "系统" : "个人"}配置模板
          </DialogTitle>
          <DialogDescription>
            创建一个可复用的 Agent 配置模板。
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="tpl-name">名称</Label>
            <Input
              id="tpl-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="如：前端开发模板"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="tpl-desc">描述</Label>
            <Input
              id="tpl-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="可选"
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="tpl-config">配置 (JSON)</Label>
            <Textarea
              id="tpl-config"
              value={config}
              onChange={(e) => setConfig(e.target.value)}
              rows={6}
              className="font-mono text-xs"
              placeholder='{"instructions": "...", "model": "claude-sonnet-4-6"}'
            />
          </div>

          {error && (
            <p className="text-sm text-destructive">{error}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={handleSubmit} disabled={submitting}>
            {submitting && <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />}
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
