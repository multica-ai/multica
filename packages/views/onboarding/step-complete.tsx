"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ArrowRight, Loader2, Bot } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card } from "@multica/ui/components/ui/card";
import { api } from "@multica/core/api";
import type { Agent, Issue, CreateIssueRequest } from "@multica/core/types";

interface OnboardingIssueDef {
  title: string;
  description: string;
  /** If true, assigned to the agent with status "todo" so it gets picked up */
  assignToAgent: boolean;
  status: "todo" | "backlog";
}

function getOnboardingIssues(): OnboardingIssueDef[] {
  return [
    {
      title: "Say hello to the team!",
      description: [
        "Welcome! This is your first automated task.",
        "",
        "Please introduce yourself to the team:",
        "- What's your name and role in this workspace?",
        "- What kinds of tasks can you help with?",
        "- Give 2–3 concrete examples of things the team can ask you to do",
        "",
        "---",
        "",
        "**Try it out!** After the agent responds, reply with one of these to see it in action:",
        '- "Review this function for bugs: `function add(a, b) { return a - b; }`"',
        '- "Draft a short description for a new onboarding feature"',
        '- "What are some best practices for writing clean commit messages?"',
        "",
        "This issue was automatically assigned to verify your agent is working end-to-end.",
      ].join("\n"),
      assignToAgent: true,
      status: "todo",
    },
    {
      title: "Set up your repository connection",
      description: [
        "Connect a code repository so agents can check out code and submit pull requests.",
        "",
        "**Steps:**",
        "1. Go to **Settings** in the sidebar",
        "2. Under **Repositories**, add a GitHub repo URL",
        "3. The agent daemon will sync the repo locally",
        "",
        "Once connected, your agents can clone, branch, and push code as part of any task.",
      ].join("\n"),
      assignToAgent: false,
      status: "backlog",
    },
    {
      title: "Create a skill for your agent",
      description: [
        "Skills are reusable instructions that make agents better at recurring tasks — deployments, code reviews, migrations, etc.",
        "",
        "**Steps:**",
        "1. Go to **Skills** in the sidebar",
        "2. Click **New Skill**",
        "3. Write a description and instructions (e.g., \"Code Review\" with your team's style guide)",
        "4. Assign the skill to an agent in the agent's settings",
        "",
        "Every skill you create compounds your team's capabilities over time.",
      ].join("\n"),
      assignToAgent: false,
      status: "backlog",
    },
    {
      title: "Invite a teammate",
      description: [
        "Multica works best with a team. Invite a colleague to your workspace so you can collaborate on issues and share agents.",
        "",
        "**Steps:**",
        "1. Go to **Settings → Members**",
        "2. Click **Invite** and enter their email",
        "3. They'll get access to the workspace, all agents, and the issue board",
      ].join("\n"),
      assignToAgent: false,
      status: "backlog",
    },
  ];
}

export function StepComplete({
  wsId,
  agent,
  onEnter,
}: {
  wsId: string;
  agent: Agent | null;
  onEnter: () => void;
}) {
  const [createdIssues, setCreatedIssues] = useState<Issue[]>([]);
  const [loading, setLoading] = useState(true);
  const didCreate = useRef(false);

  useEffect(() => {
    if (didCreate.current) return;
    didCreate.current = true;

    async function createOnboardingIssues() {
      const defs = getOnboardingIssues();
      const issues: Issue[] = [];

      for (const def of defs) {
        try {
          const req: CreateIssueRequest = {
            title: def.title,
            description: def.description,
            status: def.status,
          };
          if (def.assignToAgent && agent) {
            req.assignee_type = "agent";
            req.assignee_id = agent.id;
          }
          const issue = await api.createIssue(req);
          issues.push(issue);
        } catch {
          // Best-effort — continue with remaining issues
        }
      }

      setCreatedIssues(issues);
      setLoading(false);
    }

    createOnboardingIssues();
  }, [agent, wsId]);

  return (
    <div className="flex w-full max-w-md flex-col items-center gap-8">
      {/* Success icon */}
      <div className="flex h-16 w-16 items-center justify-center rounded-full bg-success/10">
        <Check className="h-8 w-8 text-success" />
      </div>

      <div className="text-center">
        <h1 className="text-3xl font-semibold tracking-tight">
          You're all set!
        </h1>
        <p className="mt-2 text-muted-foreground">
          {agent
            ? `Your workspace is ready and ${agent.name} is picking up its first task.`
            : "Your workspace is ready. Create issues and assign them to agents to get started."}
        </p>
      </div>

      {/* Created issues */}
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span>Setting up your workspace...</span>
        </div>
      ) : (
        createdIssues.length > 0 && (
          <Card className="w-full divide-y">
            {createdIssues.map((issue) => (
              <div
                key={issue.id}
                className="flex items-center gap-3 px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">
                    {issue.identifier} {issue.title}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {issue.assignee_id && agent
                      ? `Assigned to ${agent.name}`
                      : issue.status === "todo"
                        ? "To do"
                        : "Backlog"}
                  </div>
                </div>
                {issue.assignee_id && agent && (
                  <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-violet-100 dark:bg-violet-900/30">
                    <Bot className="h-3.5 w-3.5 text-violet-600 dark:text-violet-400" />
                  </div>
                )}
              </div>
            ))}
          </Card>
        )
      )}

      <Button
        className="w-full"
        size="lg"
        onClick={onEnter}
        disabled={loading}
      >
        Go to Workspace
        <ArrowRight className="ml-2 h-4 w-4" />
      </Button>
    </div>
  );
}
