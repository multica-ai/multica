"use client";

import { useState, useEffect, useMemo } from "react";
import { Cloud, ChevronDown, Globe, Lock, Loader2 } from "lucide-react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { ActorAvatar } from "../../common/actor-avatar";
import { ModelDropdown } from "./model-dropdown";
import type {
  Agent,
  AgentVisibility,
  RuntimeDevice,
  MemberWithUser,
  CreateAgentRequest,
} from "@multica/core/types";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";

type RuntimeFilter = "mine" | "all";

export function CreateAgentDialog({
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  template,
  onClose,
  onCreate,
}: {
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  // When provided, the dialog opens in "Duplicate" mode: the visible
  // fields (name / description / runtime / visibility / model) are
  // pre-populated from this agent, and the hidden fields
  // (instructions / custom_args / custom_env / max_concurrent_tasks)
  // are forwarded to the create call so the new agent is a true clone.
  // Skills are copied separately by the caller after createAgent
  // succeeds — they're not part of CreateAgentRequest.
  template?: Agent | null;
  onClose: () => void;
  onCreate: (data: CreateAgentRequest) => Promise<void>;
}) {
  const isDuplicate = !!template;
  const [name, setName] = useState(
    template ? `${template.name} (Copy)` : "",
  );
  const [description, setDescription] = useState(template?.description ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>(
    template?.visibility ?? "private",
  );
  const [model, setModel] = useState(template?.model ?? "");
  const [creating, setCreating] = useState(false);
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const [runtimeFilter, setRuntimeFilter] = useState<RuntimeFilter>("mine");

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  const hasOtherRuntimes = runtimes.some((r) => r.owner_id !== currentUserId);

  const filteredRuntimes = useMemo(() => {
    const filtered = runtimeFilter === "mine" && currentUserId
      ? runtimes.filter((r) => r.owner_id === currentUserId)
      : runtimes;
    return [...filtered].sort((a, b) => {
      if (a.owner_id === currentUserId && b.owner_id !== currentUserId) return -1;
      if (a.owner_id !== currentUserId && b.owner_id === currentUserId) return 1;
      return 0;
    });
  }, [runtimes, runtimeFilter, currentUserId]);

  // When duplicating, default to the template's runtime so the clone
  // lands on the same machine — caller can still switch in the picker.
  const [selectedRuntimeId, setSelectedRuntimeId] = useState(
    template?.runtime_id ?? filteredRuntimes[0]?.id ?? "",
  );

  useEffect(() => {
    if (!selectedRuntimeId && filteredRuntimes[0]) {
      setSelectedRuntimeId(filteredRuntimes[0].id);
    }
  }, [filteredRuntimes, selectedRuntimeId]);

  const selectedRuntime = runtimes.find((d) => d.id === selectedRuntimeId) ?? null;

  const handleSubmit = async () => {
    if (!name.trim() || !selectedRuntime) return;
    setCreating(true);
    try {
      // When duplicating, forward the hidden config fields the template
      // carries (instructions / custom_args / custom_env / max_concurrent_tasks)
      // so the clone is functional out of the box without the user
      // having to walk back through every settings tab. Skills are
      // copied by the caller in a follow-up setAgentSkills call.
      const data: CreateAgentRequest = {
        name: name.trim(),
        description: description.trim(),
        runtime_id: selectedRuntime.id,
        visibility,
        model: model.trim() || undefined,
      };
      if (template) {
        if (template.instructions) data.instructions = template.instructions;
        if (template.custom_args.length) data.custom_args = template.custom_args;
        // Skip env when the template's values are redacted from the API
        // response — copying placeholders would create a broken clone.
        if (
          !template.custom_env_redacted &&
          Object.keys(template.custom_env).length > 0
        ) {
          data.custom_env = template.custom_env;
        }
        if (template.max_concurrent_tasks) {
          data.max_concurrent_tasks = template.max_concurrent_tasks;
        }
      }
      await onCreate(data);
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create agent");
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {isDuplicate ? "Duplicate Agent" : "Create Agent"}
          </DialogTitle>
          <DialogDescription>
            {isDuplicate
              ? `Create a new agent based on "${template!.name}". Instructions, env, and skills are copied for you.`
              : "Create a new AI agent for your workspace."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label className="text-xs text-muted-foreground">Name</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Deep Research Agent"
              className="mt-1"
              onKeyDown={(e) => {
                if (e.key !== "Enter" || e.nativeEvent.isComposing) return;
                handleSubmit();
              }}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Description</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What does this agent do?"
              className="mt-1"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">Visibility</Label>
            <div className="mt-1.5 flex gap-2">
              <button
                type="button"
                onClick={() => setVisibility("workspace")}
                className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                  visibility === "workspace"
                    ? "border-primary bg-primary/5"
                    : "border-border hover:bg-muted"
                }`}
              >
                <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-left">
                  <div className="font-medium">Workspace</div>
                  <div className="text-xs text-muted-foreground">All members can assign</div>
                </div>
              </button>
              <button
                type="button"
                onClick={() => setVisibility("private")}
                className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
                  visibility === "private"
                    ? "border-primary bg-primary/5"
                    : "border-border hover:bg-muted"
                }`}
              >
                <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-left">
                  <div className="font-medium">Private</div>
                  <div className="text-xs text-muted-foreground">Only you can assign</div>
                </div>
              </button>
            </div>
          </div>

          <div className="min-w-0">
            <div className="flex items-center justify-between">
              <Label className="text-xs text-muted-foreground">Runtime</Label>
              {hasOtherRuntimes && (
                <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
                  <button
                    type="button"
                    onClick={() => { setRuntimeFilter("mine"); setSelectedRuntimeId(""); }}
                    className={`rounded px-2 py-0.5 text-xs font-medium transition-colors ${
                      runtimeFilter === "mine"
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    Mine
                  </button>
                  <button
                    type="button"
                    onClick={() => { setRuntimeFilter("all"); setSelectedRuntimeId(""); }}
                    className={`rounded px-2 py-0.5 text-xs font-medium transition-colors ${
                      runtimeFilter === "all"
                        ? "bg-background text-foreground shadow-sm"
                        : "text-muted-foreground hover:text-foreground"
                    }`}
                  >
                    All
                  </button>
                </div>
              )}
            </div>
            <Popover open={runtimeOpen} onOpenChange={setRuntimeOpen}>
              <PopoverTrigger
                disabled={runtimes.length === 0 && !runtimesLoading}
                className="flex w-full min-w-0 items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
              >
                {runtimesLoading ? (
                  <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />
                ) : selectedRuntime ? (
                  <ProviderLogo provider={selectedRuntime.provider} className="h-4 w-4 shrink-0" />
                ) : (
                  <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">
                      {runtimesLoading ? "Loading runtimes..." : (selectedRuntime?.name ?? "No runtime available")}
                    </span>
                    {selectedRuntime?.runtime_mode === "cloud" && (
                      <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                        Cloud
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {selectedRuntime
                      ? (getOwnerMember(selectedRuntime.owner_id)?.name ?? selectedRuntime.device_info)
                      : "Register a runtime before creating an agent"}
                  </div>
                </div>
                <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${runtimeOpen ? "rotate-180" : ""}`} />
              </PopoverTrigger>
              <PopoverContent align="start" className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto">
                {filteredRuntimes.map((device) => {
                  const ownerMember = getOwnerMember(device.owner_id);
                  return (
                    <button
                      key={device.id}
                      onClick={() => {
                        setSelectedRuntimeId(device.id);
                        setRuntimeOpen(false);
                      }}
                      className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                        device.id === selectedRuntimeId ? "bg-accent" : "hover:bg-accent/50"
                      }`}
                    >
                      <ProviderLogo provider={device.provider} className="h-4 w-4 shrink-0" />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate font-medium">{device.name}</span>
                          {device.runtime_mode === "cloud" && (
                            <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                              Cloud
                            </span>
                          )}
                        </div>
                        <div className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground">
                          {ownerMember ? (
                            <>
                              <ActorAvatar actorType="member" actorId={ownerMember.user_id} size={14} />
                              <span className="truncate">{ownerMember.name}</span>
                            </>
                          ) : (
                            <span className="truncate">{device.device_info}</span>
                          )}
                        </div>
                      </div>
                      <span
                        className={`h-2 w-2 shrink-0 rounded-full ${
                          device.status === "online" ? "bg-success" : "bg-muted-foreground/40"
                        }`}
                      />
                    </button>
                  );
                })}
              </PopoverContent>
            </Popover>
          </div>

          <ModelDropdown
            runtimeId={selectedRuntime?.id ?? null}
            runtimeOnline={selectedRuntime?.status === "online"}
            value={model}
            onChange={setModel}
            disabled={!selectedRuntime}
          />
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={creating || !name.trim() || !selectedRuntime}
          >
            {creating ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
