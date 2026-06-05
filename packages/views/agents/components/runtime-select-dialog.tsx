"use client";

import { useState } from "react";
import { Monitor } from "lucide-react";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

interface RuntimeSelectDialogProps {
  agentName: string;
  runtimes: AgentRuntime[];
  loading: boolean;
  onConfirm: (runtimeId: string) => void;
  onClose: () => void;
}

export function RuntimeSelectDialog({
  agentName,
  runtimes,
  loading,
  onConfirm,
  onClose,
}: RuntimeSelectDialogProps) {
  const [selectedRuntimeId, setSelectedRuntimeId] = useState<string>("");

  return (
    <Dialog open onOpenChange={onClose}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle className="text-sm font-semibold">
            选择运行时
          </DialogTitle>
          <DialogDescription className="text-xs">
            为 <strong>{agentName}</strong> 选择一个运行时来执行操作。
          </DialogDescription>
        </DialogHeader>

        {loading ? (
          <div className="grid w-full gap-2">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[60px] w-full rounded-lg" />
            ))}
          </div>
        ) : runtimes.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-3 py-10 text-center">
            <Monitor className="h-8 w-8 text-muted-foreground/40" />
            <p className="text-sm text-muted-foreground">没有可用的运行时</p>
            <p className="text-xs text-muted-foreground/70 max-w-xs">
              请先连接一个运行时设备后再执行内置 Agent。
            </p>
          </div>
        ) : (
          <RadioGroup
            className="grid w-full gap-2"
            value={selectedRuntimeId}
            onValueChange={setSelectedRuntimeId}
          >
            {runtimes.map((runtime) => (
              <label
                key={runtime.id}
                className={`flex items-center gap-3 rounded-lg border px-4 py-3 transition-colors cursor-pointer hover:bg-accent/40 ${
                  selectedRuntimeId === runtime.id
                    ? "border-primary bg-primary/5"
                    : ""
                }`}
              >
                <RadioGroupItem value={runtime.id} className="shrink-0" />
                <div className="flex flex-col min-w-0 gap-0.5">
                  <span className="text-sm font-medium truncate">
                    {runtime.name}
                  </span>
                  <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${
                        runtime.status === "online"
                          ? "bg-emerald-500"
                          : "bg-muted-foreground/40"
                      }`}
                    />
                    {runtime.status === "online" ? "在线" : "离线"}
                    {" · "}
                    {runtime.runtime_mode === "local" ? "本地" : "云端"}
                  </span>
                </div>
                <Monitor className="h-4 w-4 text-muted-foreground shrink-0 ml-auto" />
              </label>
            ))}
          </RadioGroup>
        )}

        <div className="flex items-center justify-end gap-2 border-t bg-background px-5 py-3 -mx-4 -mb-4">
          <Button variant="ghost" size="sm" onClick={onClose}>
            取消
          </Button>
          <Button
            size="sm"
            disabled={!selectedRuntimeId}
            onClick={() => onConfirm(selectedRuntimeId)}
          >
            确认执行
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
