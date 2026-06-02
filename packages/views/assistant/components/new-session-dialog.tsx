"use client";

import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { ActorAvatar } from "../../common/actor-avatar";
import type { Agent, MemberWithUser, RuntimeDevice } from "@multica/core/types";

export function NewSessionDialog({
  open,
  onOpenChange,
  agents,
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  onCreateSession,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agents: Agent[];
  runtimes: RuntimeDevice[];
  runtimesLoading: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  onCreateSession: (agentId: string, runtimeId: string) => Promise<void>;
}) {
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const [isCreating, setIsCreating] = useState(false);

  // 当对话框打开时，重置选择
  useEffect(() => {
    if (open) {
      // 默认选择第一个可用的 agent
      const firstAgent = agents.find((a) => !a.archived_at);
      setSelectedAgentId(firstAgent?.id ?? "");
      setSelectedRuntimeId("");
    }
  }, [open, agents]);

  const handleCreate = async () => {
    if (!selectedAgentId || !selectedRuntimeId) return;

    setIsCreating(true);
    try {
      await onCreateSession(selectedAgentId, selectedRuntimeId);
    } finally {
      setIsCreating(false);
    }
  };

  const selectedAgent = agents.find((a) => a.id === selectedAgentId);
  const canCreate = selectedAgentId && selectedRuntimeId && !isCreating;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>新建对话</DialogTitle>
          <DialogDescription>
            选择一个智能体和运行时来开始对话
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* 智能体选择 */}
          <div className="space-y-2">
            <Label>智能体</Label>
            <div className="space-y-2">
              {agents.filter((a) => !a.archived_at).map((agent) => (
                <button
                  key={agent.id}
                  type="button"
                  onClick={() => setSelectedAgentId(agent.id)}
                  className={`flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors ${
                    selectedAgentId === agent.id
                      ? "border-primary bg-accent"
                      : "border-border hover:bg-accent/50"
                  }`}
                >
                  <ActorAvatar
                    actorType="agent"
                    actorId={agent.id}
                    size={32}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="font-medium truncate">{agent.name}</div>
                    {agent.instructions && (
                      <div className="text-xs text-muted-foreground truncate">
                        {agent.instructions}
                      </div>
                    )}
                  </div>
                  {selectedAgentId === agent.id && (
                    <div className="h-2 w-2 rounded-full bg-primary" />
                  )}
                </button>
              ))}
              {agents.filter((a) => !a.archived_at).length === 0 && (
                <div className="text-sm text-muted-foreground text-center py-8">
                  暂无可用的智能体
                </div>
              )}
            </div>
          </div>

          {/* 运行时选择 */}
          {selectedAgentId && (
            <RuntimePicker
              runtimes={runtimes}
              runtimesLoading={runtimesLoading}
              members={members}
              currentUserId={currentUserId}
              selectedRuntimeId={selectedRuntimeId}
              onSelect={setSelectedRuntimeId}
            />
          )}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isCreating}
          >
            取消
          </Button>
          <Button
            onClick={handleCreate}
            disabled={!canCreate}
          >
            {isCreating ? "创建中..." : "开始对话"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
