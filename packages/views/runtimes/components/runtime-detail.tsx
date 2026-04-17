"use client";

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useDeleteRuntime } from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
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
import { ActorAvatar } from "../../common/actor-avatar";
import { formatLastSeen } from "../utils";
import { StatusBadge, InfoField } from "./shared";
import { ProviderLogo } from "./provider-logo";
import { PingSection } from "./ping-section";
import { UpdateSection } from "./update-section";
import { UsageSection } from "./usage-section";

function getCliVersion(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.cli_version === "string" &&
    metadata.cli_version
  ) {
    return metadata.cli_version;
  }
  return null;
}

function getLaunchedBy(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.launched_by === "string" &&
    metadata.launched_by
  ) {
    return metadata.launched_by;
  }
  return null;
}

export function RuntimeDetail({ runtime }: { runtime: AgentRuntime }) {
  const cliVersion =
    runtime.runtime_mode === "local" ? getCliVersion(runtime.metadata) : null;
  const launchedBy =
    runtime.runtime_mode === "local" ? getLaunchedBy(runtime.metadata) : null;

  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const deleteMutation = useDeleteRuntime(wsId);

  const [deleteOpen, setDeleteOpen] = useState(false);

  // Resolve owner info
  const ownerMember = runtime.owner_id
    ? members.find((m) => m.user_id === runtime.owner_id) ?? null
    : null;

  // Permission check for delete
  const currentMember = user
    ? members.find((m) => m.user_id === user.id)
    : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;
  const isRuntimeOwner = user && runtime.owner_id === user.id;
  const canDelete = isAdmin || isRuntimeOwner;

  const handleDelete = () => {
    deleteMutation.mutate(runtime.id, {
      onSuccess: () => {
        toast.success("运行时已删除");
        setDeleteOpen(false);
      },
      onError: (e) => {
        toast.error(e instanceof Error ? e.message : "删除运行时失败");
      },
    });
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex h-7 w-7 shrink-0 items-center justify-center">
            <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h2 className="text-sm font-semibold truncate">{runtime.name}</h2>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge status={runtime.status} />
          {canDelete && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-destructive"
              onClick={() => setDeleteOpen(true)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6 space-y-6">
        {/* Info grid */}
        <div className="grid grid-cols-2 gap-4">
          <InfoField label="运行模式" value={runtime.runtime_mode} />
          <InfoField label="提供商" value={runtime.provider} />
          <InfoField label="状态" value={runtime.status} />
          <InfoField
            label="最后在线"
            value={formatLastSeen(runtime.last_seen_at)}
          />
          {ownerMember && (
            <div>
              <div className="text-xs text-muted-foreground mb-1">所有者</div>
              <div className="flex items-center gap-2">
                <ActorAvatar
                  actorType="member"
                  actorId={ownerMember.user_id}
                  size={20}
                />
                <span className="text-sm">{ownerMember.name}</span>
              </div>
            </div>
          )}
          {runtime.device_info && (
            <InfoField label="设备" value={runtime.device_info} />
          )}
          {runtime.daemon_id && (
            <InfoField label="守护进程 ID" value={runtime.daemon_id} mono />
          )}
        </div>

        {/* CLI Version & Update */}
        {runtime.runtime_mode === "local" && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-3">
              CLI 版本
            </h3>
            <UpdateSection
              runtimeId={runtime.id}
              currentVersion={cliVersion}
              isOnline={runtime.status === "online"}
              launchedBy={launchedBy}
            />
          </div>
        )}

        {/* Connection Test */}
        <div>
          <h3 className="text-xs font-medium text-muted-foreground mb-3">
            连接测试
          </h3>
          <PingSection runtimeId={runtime.id} />
        </div>

        {/* Usage */}
        <div>
          <h3 className="text-xs font-medium text-muted-foreground mb-3">
            Token 用量
          </h3>
          <UsageSection runtimeId={runtime.id} />
        </div>

        {/* Metadata */}
        {runtime.metadata && Object.keys(runtime.metadata).length > 0 && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-2">
              元数据
            </h3>
            <div className="rounded-lg border bg-muted/30 p-3">
              <pre className="text-xs font-mono whitespace-pre-wrap break-all">
                {JSON.stringify(runtime.metadata, null, 2)}
              </pre>
            </div>
          </div>
        )}

        {/* Timestamps */}
        <div className="grid grid-cols-2 gap-4 border-t pt-4">
          <InfoField
            label="创建时间"
            value={new Date(runtime.created_at).toLocaleString("zh-CN")}
          />
          <InfoField
            label="更新时间"
            value={new Date(runtime.updated_at).toLocaleString("zh-CN")}
          />
        </div>
      </div>

      {/* Delete confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={(v) => { if (!v) setDeleteOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除运行时</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除「{runtime.name}」吗？此操作无法撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? "删除中..." : "删除"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
