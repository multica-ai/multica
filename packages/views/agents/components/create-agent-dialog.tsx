"use client";

import { useState, useEffect, useMemo } from "react";
import { Cloud, ChevronDown, Globe, Lock, Loader2 } from "lucide-react";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { ActorAvatar } from "../../common/actor-avatar";
import { ModelDropdown } from "./model-dropdown";
import type {
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
import { useAgentsT } from "../i18n";

type RuntimeFilter = "mine" | "all";

export function CreateAgentDialog({
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  onClose,
  onCreate,
}: {
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  onClose: () => void;
  onCreate: (data: CreateAgentRequest) => Promise<void>;
}) {
  const t = useAgentsT();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<AgentVisibility>("private");
  const [model, setModel] = useState("");
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

  const [selectedRuntimeId, setSelectedRuntimeId] = useState(filteredRuntimes[0]?.id ?? "");

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
      await onCreate({
        name: name.trim(),
        description: description.trim(),
        runtime_id: selectedRuntime.id,
        visibility,
        model: model.trim() || undefined,
      });
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t.toasts.failedToCreate);
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t.createDialog.title}</DialogTitle>
          <DialogDescription>
            {t.createDialog.description}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label className="text-xs text-muted-foreground">{t.createDialog.nameLabel}</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t.createDialog.namePlaceholder}
              className="mt-1"
              onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t.createDialog.descriptionLabel}</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t.createDialog.descriptionPlaceholder}
              className="mt-1"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t.createDialog.visibilityLabel}</Label>
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
                  <div className="font-medium">{t.createDialog.workspaceTitle}</div>
                  <div className="text-xs text-muted-foreground">{t.createDialog.workspaceHelp}</div>
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
                  <div className="font-medium">{t.createDialog.privateTitle}</div>
                  <div className="text-xs text-muted-foreground">{t.createDialog.privateHelp}</div>
                </div>
              </button>
            </div>
          </div>

          <div className="min-w-0">
            <div className="flex items-center justify-between">
              <Label className="text-xs text-muted-foreground">{t.createDialog.runtimeLabel}</Label>
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
                    {t.createDialog.runtimeFilterMine}
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
                    {t.createDialog.runtimeFilterAll}
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
                      {runtimesLoading ? t.createDialog.loadingRuntimes : (selectedRuntime?.name ?? t.createDialog.noRuntimeAvailable)}
                    </span>
                    {selectedRuntime?.runtime_mode === "cloud" && (
                      <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                        {t.createDialog.cloudBadge}
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {selectedRuntime
                      ? (getOwnerMember(selectedRuntime.owner_id)?.name ?? selectedRuntime.device_info)
                      : t.createDialog.registerRuntimeFirst}
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
                              {t.createDialog.cloudBadge}
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
            {t.createDialog.cancel}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={creating || !name.trim() || !selectedRuntime}
          >
            {creating ? t.createDialog.creating : t.createDialog.create}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
