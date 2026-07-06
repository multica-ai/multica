"use client";

import { useState } from "react";
import { Send } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useCopyAgent } from "@multica/core/agents";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";

interface DeployAgentModalProps {
  agent: Agent;
  currentWorkspaceId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function DeployAgentModal({
  agent,
  currentWorkspaceId,
  open,
  onOpenChange,
}: DeployAgentModalProps) {
  const [targetSlug, setTargetSlug] = useState("");
  const [name, setName] = useState(agent.name);

  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const copyAgent = useCopyAgent();

  const otherWorkspaces = workspaces.filter((w) => w.id !== currentWorkspaceId);

  const handleDeploy = () => {
    if (!targetSlug) return;
    copyAgent.mutate(
      {
        agentId: agent.id,
        targetWorkspaceSlug: targetSlug,
        name: name !== agent.name ? name : undefined,
      },
      {
        onSuccess: () => {
          toast.success(`Agent deployed to ${targetSlug}`);
          onOpenChange(false);
        },
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : "Failed to deploy agent");
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Deploy agent to workspace</DialogTitle>
          <DialogDescription>
            Copies this agent&apos;s configuration and skills to another workspace.
            Secrets are never copied. The target workspace&apos;s runner is used automatically.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4 py-2">
          <div className="flex flex-col gap-1.5">
            <Label>Target workspace</Label>
            <Select value={targetSlug} onValueChange={(v) => setTargetSlug(v ?? "")}>
              <SelectTrigger>
                <SelectValue placeholder="Select a workspace…" />
              </SelectTrigger>
              <SelectContent>
                {otherWorkspaces.map((w) => (
                  <SelectItem key={w.id} value={w.slug}>
                    {w.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>Agent name</Label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={agent.name}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleDeploy} disabled={!targetSlug || copyAgent.isPending}>
            <Send className="h-4 w-4" />
            {copyAgent.isPending ? "Deploying…" : "Deploy"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
