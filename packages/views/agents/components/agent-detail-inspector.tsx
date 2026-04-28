"use client";

import {
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Camera, Loader2, Pencil } from "lucide-react";
import { toast } from "sonner";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { timeAgo } from "@multica/core/utils";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../../common/actor-avatar";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { PropRow } from "../../common/prop-row";
import { availabilityConfig } from "../presence";
import { ConcurrencyPicker } from "./inspector/concurrency-picker";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { SkillAttach } from "./inspector/skill-attach";
import { VisibilityPicker } from "./inspector/visibility-picker";

interface InspectorProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | null;
  presence: AgentPresenceDetail | null | undefined;
  // Below: needed for inline edit. The inspector now owns the editing surface
  // (no Settings tab anymore), so the parent has to pass through everything
  // a write needs.
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}

/**
 * Left 320px column of the agent detail page. Holds the agent's identity card
 * (avatar / name / description / status), inline-editable properties, and
 * skills.
 *
 * **All editing happens here** — there is no separate Settings tab. The
 * trade-off is that the inspector carries some weight (4 inline pickers plus
 * 3 popovers for name/description/avatar), but it eliminates the "see vs
 * edit" mode split that the previous Settings tab created. Users no longer
 * have to switch tabs and hunt for the field they were already looking at.
 */
export function AgentDetailInspector({
  agent,
  runtime,
  owner,
  presence,
  runtimes,
  members,
  currentUserId,
  onUpdate,
}: InspectorProps) {
  const update = (data: Record<string, unknown>) => onUpdate(agent.id, data);
  const isOnline = runtime?.status === "online";

  return (
    <aside className="flex h-full min-h-0 w-full flex-col overflow-y-auto rounded-lg border bg-background">
      {/* Identity */}
      <div className="flex flex-col gap-3 border-b px-5 pb-5 pt-5">
        <AvatarEditor agent={agent} onUpdate={update} />
        <NameAndDescription agent={agent} onUpdate={update} />
        <PresenceBadge presence={presence} />
      </div>

      {/* Properties — editable. Row hover is OFF here on purpose: each chip
          (RuntimePicker, ModelPicker, …) carries its own border + hover-bg
          treatment that already telegraphs "this is a button". A second
          row-wide hover layer on top would just smudge the chip boundary
          and make it harder, not easier, to see what's clickable. */}
      <Section label="Properties">
        <PropRow label="Runtime" interactive={false}>
          <RuntimePicker
            value={agent.runtime_id}
            runtimes={runtimes}
            members={members}
            currentUserId={currentUserId}
            onChange={(id) => update({ runtime_id: id })}
          />
        </PropRow>
        <PropRow label="Model" interactive={false}>
          <ModelPicker
            runtimeId={agent.runtime_id}
            runtimeOnline={!!isOnline}
            value={agent.model ?? ""}
            onChange={(m) => update({ model: m })}
          />
        </PropRow>
        <PropRow label="Visibility" interactive={false}>
          <VisibilityPicker
            value={agent.visibility}
            onChange={(v) => update({ visibility: v })}
          />
        </PropRow>
        <PropRow label="Concurrency" interactive={false}>
          <ConcurrencyPicker
            value={agent.max_concurrent_tasks}
            onChange={(n) => update({ max_concurrent_tasks: n })}
          />
        </PropRow>
      </Section>

      {/* Details — read-only (no hover, no chip styling — these aren't clickable) */}
      <Section label="Details">
        {owner && (
          <PropRow label="Owner" interactive={false}>
            <span className="flex min-w-0 items-center gap-1.5">
              <ActorAvatar
                actorType="member"
                actorId={owner.user_id}
                size={14}
              />
              <span className="truncate">{owner.name}</span>
            </span>
          </PropRow>
        )}
        <PropRow label="Created" interactive={false}>
          <span className="text-muted-foreground">
            {timeAgo(agent.created_at)}
          </span>
        </PropRow>
        <PropRow label="Updated" interactive={false}>
          <span className="text-muted-foreground">
            {timeAgo(agent.updated_at)}
          </span>
        </PropRow>
      </Section>

      {/* Skills */}
      <div className="flex flex-col border-b px-5 py-4">
        <div className="mb-2 flex items-center gap-2">
          <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
            Skills
          </span>
          <span className="font-mono text-[10px] tabular-nums text-muted-foreground/70">
            {agent.skills.length}
          </span>
        </div>
        <div className="flex flex-wrap gap-1">
          {agent.skills.map((s) => (
            <span
              key={s.id}
              className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[10px] font-medium text-muted-foreground"
            >
              {s.name}
            </span>
          ))}
          <SkillAttach agent={agent} />
        </div>
      </div>
    </aside>
  );
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

function Section({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-0.5 border-b px-5 py-4">
      <div className="mb-1 px-2 -mx-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Identity — avatar / name / description editors
// ---------------------------------------------------------------------------

function AvatarEditor({
  agent,
  onUpdate,
}: {
  agent: Agent;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
}) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { upload, uploading } = useFileUpload(api);

  const handleFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    try {
      const result = await upload(file);
      if (!result) return;
      await onUpdate({ avatar_url: result.link });
      toast.success("Avatar updated");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to upload avatar");
    }
  };

  return (
    <>
      <button
        type="button"
        // rounded-lg matches the standard agent avatar treatment used in
        // list rows. Avoid rounded-full — circles are reserved for humans.
        className="group relative h-14 w-14 shrink-0 overflow-hidden rounded-lg bg-muted focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={() => fileInputRef.current?.click()}
        disabled={uploading}
        aria-label="Change avatar"
      >
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size={56}
          className="rounded-none"
        />
        <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
          {uploading ? (
            <Loader2 className="h-4 w-4 animate-spin text-white" />
          ) : (
            <Camera className="h-4 w-4 text-white" />
          )}
        </div>
      </button>
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        className="hidden"
        onChange={handleFile}
      />
    </>
  );
}

function NameAndDescription({
  agent,
  onUpdate,
}: {
  agent: Agent;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
}) {
  return (
    <div className="flex flex-col gap-1">
      <InlineEditPopover
        value={agent.name}
        onSave={(v) => onUpdate({ name: v.trim() })}
        kind="input"
        title="Rename agent"
        placeholder="Agent name"
        validate={(v) => (v.trim().length > 0 ? null : "Name is required")}
      >
        {(triggerProps) => (
          <button
            type="button"
            {...triggerProps}
            className="group -mx-1 inline-flex items-center gap-1.5 self-start rounded px-1 text-left text-base font-semibold leading-tight transition-colors hover:bg-accent/50"
          >
            <span>{agent.name}</span>
            <Pencil className="h-3 w-3 shrink-0 text-muted-foreground/0 transition-colors group-hover:text-muted-foreground" />
          </button>
        )}
      </InlineEditPopover>

      <InlineEditPopover
        value={agent.description ?? ""}
        onSave={(v) => onUpdate({ description: v })}
        kind="textarea"
        title="Edit description"
        placeholder="What does this agent do?"
      >
        {(triggerProps) => (
          <button
            type="button"
            {...triggerProps}
            className="group -mx-1 inline-flex items-start gap-1.5 self-start rounded px-1 text-left text-xs leading-relaxed transition-colors hover:bg-accent/50"
          >
            {agent.description ? (
              <span className="text-muted-foreground">{agent.description}</span>
            ) : (
              <span className="italic text-muted-foreground/50">
                No description
              </span>
            )}
            <Pencil className="mt-0.5 h-3 w-3 shrink-0 text-muted-foreground/0 transition-colors group-hover:text-muted-foreground" />
          </button>
        )}
      </InlineEditPopover>
    </div>
  );
}

// Generic single-field popover editor used for name / description. Keeps the
// trigger styling fully in the caller's hands by using a render prop.
function InlineEditPopover({
  value,
  onSave,
  kind,
  title,
  placeholder,
  validate,
  children,
}: {
  value: string;
  onSave: (next: string) => Promise<void>;
  kind: "input" | "textarea";
  title: string;
  placeholder?: string;
  validate?: (v: string) => string | null;
  children: (triggerProps: {
    onClick: (e: React.MouseEvent) => void;
  }) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset draft when popover opens or upstream value changes between sessions.
  useEffect(() => {
    if (open) {
      setDraft(value);
      setError(null);
    }
  }, [open, value]);

  const commit = async () => {
    const err = validate?.(draft) ?? null;
    if (err) {
      setError(err);
      return;
    }
    if (draft === value) {
      setOpen(false);
      return;
    }
    setSaving(true);
    try {
      await onSave(draft);
      setOpen(false);
    } catch {
      // toast handled by parent's onUpdate
    } finally {
      setSaving(false);
    }
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={children({ onClick: () => setOpen(true) }) as React.ReactElement}
      />
      <PopoverContent align="start" className="w-72 p-3">
        <div className="space-y-2">
          <p className="text-xs font-medium">{title}</p>
          {kind === "input" ? (
            <Input
              autoFocus
              value={draft}
              onChange={(e) => {
                setDraft(e.target.value);
                if (error) setError(null);
              }}
              placeholder={placeholder}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  void commit();
                } else if (e.key === "Escape") {
                  setOpen(false);
                }
              }}
              className="h-8"
            />
          ) : (
            <textarea
              autoFocus
              value={draft}
              onChange={(e) => {
                setDraft(e.target.value);
                if (error) setError(null);
              }}
              placeholder={placeholder}
              onKeyDown={(e) => {
                if (e.key === "Escape") setOpen(false);
                if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  void commit();
                }
              }}
              rows={3}
              className="w-full resize-none rounded-md border bg-transparent px-2 py-1.5 text-xs outline-none focus-visible:border-input"
            />
          )}
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="flex items-center justify-end gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
              disabled={saving}
            >
              Cancel
            </Button>
            <Button
              size="sm"
              onClick={() => void commit()}
              disabled={saving || draft === value}
            >
              {saving ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                "Save"
              )}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

// ---------------------------------------------------------------------------
// Presence badge — unchanged from the previous version
// ---------------------------------------------------------------------------

function PresenceBadge({
  presence,
}: {
  presence: AgentPresenceDetail | null | undefined;
}) {
  if (!presence) {
    return (
      <span className="inline-flex h-5 w-20 animate-pulse rounded-md bg-muted" />
    );
  }
  const av = availabilityConfig[presence.availability];
  // Last-task chip / failure copy intentionally omitted on the detail page
  // — the Recent work panel below shows the same data with full context.

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span
        className={`inline-flex items-center gap-1.5 rounded-md border px-1.5 py-0.5 text-xs ${av.textClass}`}
      >
        <span className={`h-1.5 w-1.5 rounded-full ${av.dotClass}`} />
        {av.label}
      </span>
    </div>
  );
}
