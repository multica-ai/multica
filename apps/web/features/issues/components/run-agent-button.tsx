"use client";

import { useState } from "react";
import { Play, Loader2, Bot } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { api } from "@/shared/api";
import { useWorkspaceStore } from "@/features/workspace";
import type { Agent, Issue } from "@/shared/types";

interface RunAgentButtonProps {
  issue: Issue;
  agents: Agent[];
  disabled?: boolean;
  onTaskStarted?: (taskId: string) => void;
}

export function RunAgentButton({ issue, agents, disabled, onTaskStarted }: RunAgentButtonProps) {
  const [running, setRunning] = useState(false);

  const availableAgents = agents.filter(
    (a) => !a.archived_at && a.status !== "offline"
  );

  const handleRun = async (agentId?: string) => {
    setRunning(true);
    try {
      const result = await api.runAgentOnIssue(issue.id, agentId);
      toast.success("Agent task started");
      onTaskStarted?.(result.task_id);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to start agent");
    } finally {
      setRunning(false);
    }
  };

  // If the issue has an agent assignee, show a simple button.
  const hasAgentAssignee = issue.assignee_type === "agent" && issue.assignee_id;

  if (hasAgentAssignee && availableAgents.length <= 1) {
    return (
      <Button
        size="sm"
        variant="default"
        disabled={disabled || running}
        onClick={() => handleRun()}
        className="gap-1.5"
      >
        {running ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <Play className="h-3.5 w-3.5" />
        )}
        Run Agent
      </Button>
    );
  }

  // Otherwise, show a dropdown to select which agent to run.
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            size="sm"
            variant="default"
            disabled={disabled || running || availableAgents.length === 0}
            className="gap-1.5"
          >
            {running ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Play className="h-3.5 w-3.5" />
            )}
            Run Agent
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        {availableAgents.map((agent) => (
          <DropdownMenuItem key={agent.id} onClick={() => handleRun(agent.id)}>
            <Bot className="h-4 w-4 mr-2 text-purple-500" />
            {agent.name}
          </DropdownMenuItem>
        ))}
        {availableAgents.length === 0 && (
          <DropdownMenuItem disabled>
            No agents available
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
