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
import {
  AGENT_DESCRIPTION_MAX_LENGTH,
  VISIBILITY_DESCRIPTION,
  VISIBILITY_LABEL,
} from "@multica/core/agents";
import { CharCounter } from "./char-counter";
import { useT } from "../../i18n";

type RuntimeFilter = "mine" | "all";

export function CreateAgentDialog({
  runtimes,
  runtimesLoading,
  members,
  currentUserId,
  isWorkspaceAdmin = false,
  template,
  onClose,
  onCreate,
}: {
  runtimes: RuntimeDevice[];
  runtimesLoading?: boolean;
  members: MemberWithUser[];
  currentUserId: string | null;
  // Workspace owners/admins can bind agents to anyone's runtime; regular
  // members can only bind to their own. When false the "All" tab is
  // hidden and the runtime list is force-filtered to runtimes the user
  // owns. Mirrors the server check in canBindAgentToRuntime.
  isWorkspaceAdmin?: boolean;
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
  const { t } = useT("agents");
  const isDuplicate = !!template;
  const [name, setName] = useState(
    template ? `${template.name}${t(($) => $.create_dialog.duplicate_copy_suffix)}` : "",
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
  // Non-admins can only bind to their own runtimes (the server enforces this).
  // Force "mine" so a stale runtimeFilter from a previous admin session, or
  // an attempt to render the "all" view, can't surface someone else's runtime
  // and produce a 403 on submit.
  const effectiveFilter: RuntimeFilter =
    isWorkspaceAdmin ? runtimeFilter : "mine";
  const showFilterTabs = isWorkspaceAdmin && hasOtherRuntimes;

  const filteredRuntimes = useMemo(() => {
    // "all" stays unfiltered (admin only — non-admins are pinned to "mine"
    // by effectiveFilter above). For "mine", missing currentUserId means we
    // cannot identify the caller's runtimes — return empty rather than
    // falling back to the full list, otherwise a transient null userId
    // (auth not yet hydrated) would briefly expose other users' runtimes
    // to a non-admin.
    const filtered =
      effectiveFilter === "all"
        ? runtimes
        : currentUserId
          ? runtimes.filter((r) => r.owner_id === currentUserId)
          : [];
    return [...filtered].sort((a, b) => {
      if (a.owner_id === currentUserId && b.owner_id !== currentUserId) return -1;
      if (a.owner_id !== currentUserId && b.owner_id === currentUserId) return 1;
      return 0;
    });
  }, [runtimes, effectiveFilter, currentUserId]);

  // When duplicating, seed the picker with the template's runtime — but
  // only if it's actually in `filteredRuntimes` (i.e. the user is allowed
  // to bind to it). The reconcile effect below corrects stale selections
  // either way, so the seed is just for the first render to avoid a flash
  // of "no selection" in the happy path.
  const [selectedRuntimeId, setSelectedRuntimeId] = useState(() => {
    const seed = template?.runtime_id ?? filteredRuntimes[0]?.id ?? "";
    if (seed && filteredRuntimes.some((r) => r.id === seed)) return seed;
    return filteredRuntimes[0]?.id ?? "";
  });

  // Keep selection inside the visible filtered set. Two cases this guards:
  // 1) duplicating an agent whose runtime is owned by someone else (template
  //    seeds an unbindable id; we drop it back to the user's first own
  //    runtime instead of letting the form submit a 403),
  // 2) admin flipping the Mine/All tab, leaving a previously-selected
  //    cross-owner runtime no longer in scope.
  useEffect(() => {
    const stillVisible = filteredRuntimes.some((r) => r.id === selectedRuntimeId);
    if (!stillVisible) {
      setSelectedRuntimeId(filteredRuntimes[0]?.id ?? "");
    }
  }, [filteredRuntimes, selectedRuntimeId]);

  // Look up the selected runtime *inside the filtered set* so the trigger
  // label, model picker, and submit button can never reference a runtime
  // the user can't bind to. Belt-and-suspenders alongside the reconcile
  // effect: if React batches/delays the effect, this still keeps the form
  // in a coherent, submittable state.
  const selectedRuntime =
    filteredRuntimes.find((d) => d.id === selectedRuntimeId) ?? null;

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
      toast.error(err instanceof Error ? err.message : t(($) => $.create_dialog.create_failed_toast));
      setCreating(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {isDuplicate ? t(($) => $.create_dialog.title_duplicate) : t(($) => $.create_dialog.title_create)}
          </DialogTitle>
          <DialogDescription>
            {isDuplicate
              ? t(($) => $.create_dialog.description_duplicate, { name: template!.name })
              : t(($) => $.create_dialog.description_create)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.name_label)}</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.create_dialog.name_placeholder)}
              className="mt-1"
              onKeyDown={(e) => e.key === "Enter" && handleSubmit()}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.description_label)}</Label>
            <Input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.create_dialog.description_placeholder)}
              maxLength={AGENT_DESCRIPTION_MAX_LENGTH}
              className="mt-1"
            />
            <div className="mt-1">
              <CharCounter
                length={[...description].length}
                max={AGENT_DESCRIPTION_MAX_LENGTH}
              />
            </div>
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.visibility_label)}</Label>
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
                  <div className="font-medium">{VISIBILITY_LABEL.workspace}</div>
                  <div className="text-xs text-muted-foreground">
                    {VISIBILITY_DESCRIPTION.workspace}
                  </div>
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
                  <div className="font-medium">{VISIBILITY_LABEL.private}</div>
                  <div className="text-xs text-muted-foreground">
                    {VISIBILITY_DESCRIPTION.private}
                  </div>
                </div>
              </button>
            </div>
          </div>

          <div className="min-w-0">
            <div className="flex items-center justify-between">
              <Label className="text-xs text-muted-foreground">{t(($) => $.create_dialog.runtime_label)}</Label>
              {showFilterTabs && (
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
                    {t(($) => $.create_dialog.runtime_filter_mine)}
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
                    {t(($) => $.create_dialog.runtime_filter_all)}
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
                      {runtimesLoading ? t(($) => $.create_dialog.runtime_loading) : (selectedRuntime?.name ?? t(($) => $.create_dialog.runtime_none))}
                    </span>
                    {selectedRuntime?.runtime_mode === "cloud" && (
                      <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                        {t(($) => $.create_dialog.runtime_cloud_badge)}
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">
                    {selectedRuntime
                      ? (getOwnerMember(selectedRuntime.owner_id)?.name ?? selectedRuntime.device_info)
                      : t(($) => $.create_dialog.runtime_register_first)}
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
                              {t(($) => $.create_dialog.runtime_cloud_badge)}
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
            {t(($) => $.create_dialog.cancel)}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={creating || !name.trim() || !selectedRuntime}
          >
            {creating ? t(($) => $.create_dialog.creating) : t(($) => $.create_dialog.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
