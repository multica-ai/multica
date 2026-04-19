"use client";

import { useState, useRef, useMemo } from "react";
import {
  Loader2,
  Save,
  Globe,
  Lock,
  Camera,
  Plus,
  X,
} from "lucide-react";
import type { Agent, AgentVisibility, RuntimeDevice, MemberWithUser } from "@multica/core/types";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { ActorAvatar } from "../../../common/actor-avatar";
import { ProviderLogo } from "../../../runtimes/components/provider-logo";

export function SettingsTab({
  agent,
  runtimes,
  members,
  currentUserId: _currentUserId,  // kept for API compatibility; may be used for mine/all filtering in future
  onSave,
}: {
  agent: Agent;
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  /** The current user's id — available for runtime ownership filtering */
  currentUserId: string | null;
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
  const [name, setName] = useState(agent.name);
  const [description, setDescription] = useState(agent.description ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>(agent.visibility);
  const [maxTasks, setMaxTasks] = useState(agent.max_concurrent_tasks);
  const [selectedRuntimeIds, setSelectedRuntimeIds] = useState<string[]>(
    () => agent.runtime_ids.filter((id) => runtimes.some((r) => r.id === id)),
  );
  const [addRuntimeOpen, setAddRuntimeOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const { upload, uploading } = useFileUpload(api);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  // Runtimes not yet assigned — shown in the Add picker
  const unassignedRuntimes = useMemo(
    () => runtimes.filter((r) => !selectedRuntimeIds.includes(r.id)),
    [runtimes, selectedRuntimeIds],
  );

  // Resolve the assigned devices from the full list (may include extras not in runtimes prop)
  const assignedDevices = useMemo(
    () =>
      selectedRuntimeIds
        .map((id) => runtimes.find((r) => r.id === id))
        .filter((r): r is RuntimeDevice => r !== undefined),
    [selectedRuntimeIds, runtimes],
  );

  const handleAvatarUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    try {
      const result = await upload(file);
      if (!result) return;
      await onSave({ avatar_url: result.link });
      toast.success("Avatar updated");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to upload avatar");
    }
  };

  const runtimesChanged =
    selectedRuntimeIds.length !== agent.runtime_ids.length ||
    selectedRuntimeIds.some((id) => !agent.runtime_ids.includes(id));

  const dirty =
    name !== agent.name ||
    description !== (agent.description ?? "") ||
    visibility !== agent.visibility ||
    maxTasks !== agent.max_concurrent_tasks ||
    runtimesChanged;

  const noRuntimes = selectedRuntimeIds.length === 0;

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error("Name is required");
      return;
    }
    if (noRuntimes) {
      toast.error("At least one runtime is required");
      return;
    }

    setSaving(true);
    try {
      await onSave({
        name: name.trim(),
        description,
        visibility,
        max_concurrent_tasks: maxTasks,
        runtime_ids: selectedRuntimeIds,
      });
      toast.success("Settings saved");
    } catch {
      toast.error("Failed to save settings");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-lg space-y-6">
      <div>
        <Label className="text-xs text-muted-foreground">Avatar</Label>
        <div className="mt-1.5 flex items-center gap-4">
          <button
            type="button"
            className="group relative h-16 w-16 shrink-0 rounded-full bg-muted overflow-hidden focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploading}
          >
            <ActorAvatar actorType="agent" actorId={agent.id} size={64} className="rounded-none" />
            <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
              {uploading ? (
                <Loader2 className="h-5 w-5 animate-spin text-white" />
              ) : (
                <Camera className="h-5 w-5 text-white" />
              )}
            </div>
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={handleAvatarUpload}
          />
          <div className="text-xs text-muted-foreground">
            Click to upload avatar
          </div>
        </div>
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Name</Label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Description</Label>
        <Input
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

      <div>
        <Label className="text-xs text-muted-foreground">Max Concurrent Tasks</Label>
        <Input
          type="number"
          min={1}
          max={50}
          value={maxTasks}
          onChange={(e) => setMaxTasks(Number(e.target.value))}
          className="mt-1 w-24"
        />
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Runtimes</Label>
        <div className="mt-1.5 flex flex-wrap gap-2">
          {assignedDevices.map((device) => {
            const ownerMember = getOwnerMember(device.owner_id);
            return (
              <div
                key={device.id}
                className="flex items-center gap-2 rounded-lg border border-border bg-background px-3 py-2 text-sm"
              >
                <ProviderLogo provider={device.provider} className="h-4 w-4 shrink-0" />
                <span className="font-medium">{device.name}</span>
                {device.runtime_mode === "cloud" && (
                  <span className="rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                    Cloud
                  </span>
                )}
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    device.status === "online" ? "bg-success" : "bg-muted-foreground/40"
                  }`}
                />
                {ownerMember && (
                  <div className="flex items-center gap-1">
                    <ActorAvatar actorType="member" actorId={ownerMember.user_id} size={14} />
                    <span className="truncate text-xs text-muted-foreground">{ownerMember.name}</span>
                  </div>
                )}
                <button
                  type="button"
                  aria-label={`Remove ${device.name}`}
                  onClick={() =>
                    setSelectedRuntimeIds((ids) => ids.filter((id) => id !== device.id))
                  }
                  className="ml-1 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            );
          })}

          {unassignedRuntimes.length > 0 && (
            <Popover open={addRuntimeOpen} onOpenChange={setAddRuntimeOpen}>
              <PopoverTrigger
                className="flex items-center gap-1.5 rounded-lg border border-dashed border-border px-3 py-2 text-sm text-muted-foreground transition-colors hover:border-foreground/30 hover:text-foreground"
              >
                <Plus className="h-3.5 w-3.5" />
                Add runtime
              </PopoverTrigger>
              <PopoverContent align="start" className="w-72 p-1 max-h-60 overflow-y-auto">
                {unassignedRuntimes.map((device) => {
                  const ownerMember = getOwnerMember(device.owner_id);
                  return (
                    <button
                      key={device.id}
                      role="menuitem"
                      onClick={() => {
                        setSelectedRuntimeIds((ids) => [...ids, device.id]);
                        setAddRuntimeOpen(false);
                      }}
                      className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50"
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
          )}
        </div>

        {noRuntimes && (
          <p className="mt-1.5 text-xs text-destructive">At least one runtime is required.</p>
        )}
      </div>

      <Button onClick={handleSave} disabled={!dirty || saving || noRuntimes} size="sm">
        {saving ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <Save className="h-3.5 w-3.5 mr-1.5" />}
        Save Changes
      </Button>
    </div>
  );
}
