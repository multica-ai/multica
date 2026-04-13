"use client";

import { useState, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronDown,
  Globe,
  Lock,
  AlertCircle,
  Crown,
  Code,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { api } from "@multica/core/api";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { ProviderLogo } from "../runtimes/components/provider-logo";
import type {
  Agent,
  AgentVisibility,
  CreateAgentRequest,
} from "@multica/core/types";

interface AgentTemplate {
  id: string;
  name: string;
  description: string;
  instructions: string;
  icon: typeof Crown;
}

const AGENT_TEMPLATES: AgentTemplate[] = [
  {
    id: "master",
    name: "Master Agent",
    description: "Manages workspace, assigns tasks, and coordinates work",
    instructions:
      "You are a Master Agent for this workspace. Your role is to manage and coordinate tasks, triage incoming issues, and ensure work is distributed effectively across the team.",
    icon: Crown,
  },
  {
    id: "coding",
    name: "Coding Agent",
    description: "Checks out code, implements features, and submits PRs",
    instructions:
      "You are a Coding Agent. Your role is to check out code repositories, implement features and bug fixes based on issue descriptions, write tests, and submit pull requests.",
    icon: Code,
  },
];

export function StepAgent({
  wsId,
  onNext,
  onAgentCreated,
}: {
  wsId: string;
  onNext: () => void;
  onAgentCreated: (agent: Agent) => void;
}) {
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const hasRuntime = runtimes.length > 0;

  // Template selection
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(
    null,
  );

  // Form state — populated from template, editable
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [selectedRuntimeId, setSelectedRuntimeId] = useState("");
  const [visibility, setVisibility] = useState<AgentVisibility>("workspace");
  const [creating, setCreating] = useState(false);
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const [showForm, setShowForm] = useState(false);

  // Auto-select first runtime
  useEffect(() => {
    if (!selectedRuntimeId && runtimes[0]) {
      setSelectedRuntimeId(runtimes[0].id);
    }
  }, [runtimes, selectedRuntimeId]);

  const selectedRuntime =
    runtimes.find((r) => r.id === selectedRuntimeId) ?? null;

  const handleSelectTemplate = (template: AgentTemplate) => {
    setSelectedTemplateId(template.id);
    setName(template.name);
    setDescription(template.description);
    setShowForm(true);
  };

  const handleCreate = async () => {
    if (!name.trim() || !selectedRuntime) return;
    const template = AGENT_TEMPLATES.find((t) => t.id === selectedTemplateId);
    setCreating(true);
    try {
      const req: CreateAgentRequest = {
        name: name.trim(),
        description: description.trim() || undefined,
        instructions: template?.instructions,
        runtime_id: selectedRuntime.id,
        visibility,
      };
      const agent = await api.createAgent(req);
      onAgentCreated(agent);
      onNext();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to create agent",
      );
      setCreating(false);
    }
  };

  return (
    <div className="flex w-full max-w-lg flex-col items-center gap-8">
      <div className="text-center">
        <h1 className="text-3xl font-semibold tracking-tight">
          Create Your First Agent
        </h1>
        <p className="mt-2 text-muted-foreground">
          Choose a template to get started, then customize your agent.
        </p>
      </div>

      {/* No runtime warning */}
      {!hasRuntime && (
        <div className="flex w-full items-start gap-2 rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-sm text-warning">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <p>
            No runtime connected. Go back to connect a runtime, or skip and set
            one up later.
          </p>
        </div>
      )}

      {/* Template cards */}
      {!showForm && (
        <div className="grid w-full grid-cols-2 gap-4">
          {AGENT_TEMPLATES.map((template) => {
            const Icon = template.icon;
            return (
              <Card
                key={template.id}
                role="button"
                tabIndex={0}
                onClick={() => handleSelectTemplate(template)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    handleSelectTemplate(template);
                  }
                }}
                className="cursor-pointer p-5 transition-all hover:border-foreground/20"
              >
                <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                  <Icon className="h-5 w-5" />
                </div>
                <h3 className="font-semibold">{template.name}</h3>
                <p className="mt-1 text-sm text-muted-foreground">
                  {template.description}
                </p>
              </Card>
            );
          })}
        </div>
      )}

      {/* Agent configuration form */}
      {showForm && (
        <Card className="w-full p-5 space-y-4">
          {/* Name */}
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">Agent Name</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Coding Agent"
            />
          </div>

          {/* Description */}
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this agent do?"
            />
          </div>

          {/* Runtime selector */}
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">Runtime</Label>
            <Popover open={runtimeOpen} onOpenChange={setRuntimeOpen}>
              <PopoverTrigger
                disabled={!hasRuntime}
                className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
              >
                {selectedRuntime ? (
                  <ProviderLogo
                    provider={selectedRuntime.provider}
                    className="h-4 w-4 shrink-0"
                  />
                ) : (
                  <div className="h-4 w-4 shrink-0 rounded-full bg-muted-foreground/30" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">
                      {selectedRuntime?.name ?? "No runtime available"}
                    </span>
                    {selectedRuntime?.runtime_mode === "cloud" && (
                      <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                        Cloud
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {selectedRuntime
                      ? `${selectedRuntime.provider} · ${selectedRuntime.device_info}`
                      : "Connect a runtime first"}
                  </div>
                </div>
                <ChevronDown
                  className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${runtimeOpen ? "rotate-180" : ""}`}
                />
              </PopoverTrigger>
              <PopoverContent
                align="start"
                className="w-[var(--anchor-width)] max-h-60 overflow-y-auto p-1"
              >
                {runtimes.map((rt) => (
                  <button
                    key={rt.id}
                    onClick={() => {
                      setSelectedRuntimeId(rt.id);
                      setRuntimeOpen(false);
                    }}
                    className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                      rt.id === selectedRuntimeId
                        ? "bg-accent"
                        : "hover:bg-accent/50"
                    }`}
                  >
                    <ProviderLogo
                      provider={rt.provider}
                      className="h-4 w-4 shrink-0"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{rt.name}</span>
                        {rt.runtime_mode === "cloud" && (
                          <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                            Cloud
                          </span>
                        )}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {rt.provider} · {rt.device_info}
                      </div>
                    </div>
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${
                        rt.status === "online"
                          ? "bg-success"
                          : "bg-muted-foreground/40"
                      }`}
                    />
                  </button>
                ))}
              </PopoverContent>
            </Popover>
          </div>

          {/* Visibility */}
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">Visibility</Label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setVisibility("workspace")}
                className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors ${
                  visibility === "workspace"
                    ? "border-primary bg-primary/5"
                    : "border-border hover:bg-muted"
                }`}
              >
                <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-left">
                  <div className="font-medium">Workspace</div>
                  <div className="text-xs text-muted-foreground">
                    All members can assign
                  </div>
                </div>
              </button>
              <button
                type="button"
                onClick={() => setVisibility("private")}
                className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors ${
                  visibility === "private"
                    ? "border-primary bg-primary/5"
                    : "border-border hover:bg-muted"
                }`}
              >
                <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-left">
                  <div className="font-medium">Private</div>
                  <div className="text-xs text-muted-foreground">
                    Only you can assign
                  </div>
                </div>
              </button>
            </div>
          </div>
        </Card>
      )}

      {/* Actions */}
      <div className="flex w-full flex-col items-center gap-3">
        {showForm ? (
          <>
            <Button
              className="w-full"
              size="lg"
              onClick={handleCreate}
              disabled={creating || !name.trim() || !selectedRuntime}
            >
              {creating ? "Creating..." : "Create Agent"}
            </Button>
            <button
              type="button"
              onClick={() => {
                setShowForm(false);
                setSelectedTemplateId(null);
              }}
              className="text-sm text-muted-foreground underline-offset-4 hover:underline"
            >
              Back to templates
            </button>
          </>
        ) : (
          <button
            type="button"
            onClick={onNext}
            className="text-sm text-muted-foreground underline-offset-4 hover:underline"
          >
            Skip for now
          </button>
        )}
      </div>
    </div>
  );
}
