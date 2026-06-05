"use client";

import { Globe, Unplug, Zap } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";

interface PromoteConfirmDialogProps {
  agentName: string;
  onConfirm: () => void;
  onClose: () => void;
}

export function PromoteConfirmDialog({
  agentName,
  onConfirm,
  onClose,
}: PromoteConfirmDialogProps) {
  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-amber-500/10">
            <Zap className="h-5 w-5 text-amber-500" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-sm font-semibold">
              提升为内置 Agent
            </DialogTitle>
            <DialogDescription className="text-xs">
              此操作将使"{agentName}"全局可见，并移除其运行时绑定。
            </DialogDescription>
          </DialogHeader>
        </div>

        <div className="flex flex-col gap-2 rounded-md border bg-muted/30 px-3 py-2.5 text-xs text-muted-foreground">
          <div className="flex items-center gap-2">
            <Globe className="h-3.5 w-3.5 shrink-0" />
            <span>全局可见 — 在所有工作区中作为模板显示</span>
          </div>
          <div className="flex items-center gap-2">
            <Unplug className="h-3.5 w-3.5 shrink-0" />
            <span>移除运行时绑定 — 内置 Agent 不与特定运行时绑定</span>
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          此操作仅管理员可执行，提升后不可撤销。
        </p>

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button variant="default" size="sm" onClick={onConfirm}>
            确认提升
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
