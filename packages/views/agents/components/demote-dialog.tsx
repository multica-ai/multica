"use client";

import { ArrowDown, EyeOff, Plug } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";

interface DemoteConfirmDialogProps {
  agentName: string;
  onConfirm: () => void;
  onClose: () => void;
}

export function DemoteConfirmDialog({
  agentName,
  onConfirm,
  onClose,
}: DemoteConfirmDialogProps) {
  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="max-w-sm" showCloseButton={false}>
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-destructive/10">
            <ArrowDown className="h-5 w-5 text-destructive" />
          </div>
          <DialogHeader className="flex-1 gap-1">
            <DialogTitle className="text-sm font-semibold">
              降级为普通 Agent
            </DialogTitle>
            <DialogDescription className="text-xs">
              此操作将使"{agentName}"失去内置 Agent 的全局可见性。
            </DialogDescription>
          </DialogHeader>
        </div>

        <div className="flex flex-col gap-2 rounded-md border bg-muted/30 px-3 py-2.5 text-xs text-muted-foreground">
          <div className="flex items-center gap-2">
            <EyeOff className="h-3.5 w-3.5 shrink-0" />
            <span>失去全局可见性 — 仅当前工作区可见</span>
          </div>
          <div className="flex items-center gap-2">
            <Plug className="h-3.5 w-3.5 shrink-0" />
            <span>需要绑定运行时 — 降级后需手动分配运行时</span>
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          当前正在使用此模板的工作区中的副本不受影响。
        </p>

        <DialogFooter>
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button variant="destructive" size="sm" onClick={onConfirm}>
            确认降级
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
