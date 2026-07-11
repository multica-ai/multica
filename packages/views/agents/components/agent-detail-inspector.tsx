"use client";

import {
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { Loader2, Pencil } from "lucide-react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import { AGENT_DESCRIPTION_MAX_LENGTH } from "@multica/core/agents";
import { isImeComposing } from "@multica/core/utils";
import { useTimeAgo } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "../../common/actor-avatar";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { PropRow } from "../../common/prop-row";
import { CharCounter } from "./char-counter";
import { useT } from "../../i18n";
import { ConcurrencyPicker } from "./inspector/concurrency-picker";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { ThinkingPropRow } from "./inspector/thinking-prop-row";
import { AccessPicker } from "./inspector/access-picker";

interface InspectorProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | null;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  /**
   * Computed by the parent via `useAgentPermissions(agent).canEdit.allowed`.
   * When false the inspector renders all editable surfaces as static
   * read-only displays — pickers become text/badges, name/description lose
   * their pencil affordance, the avatar is no longer clickable, and the
   * "Attach skill" trigger is hidden. Mirrors the backend gate at
   * `server/internal/handler/agent.go:519-535`.
   */
  canEdit: boolean;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}

/**
 * General settings surface. Identity and execution controls are grouped by
 * product meaning instead of being squeezed into a persistent inspector next
 * to every task view. This keeps the workbench read-oriented while preserving
 * the existing permission-aware picker and optimistic-update behaviour.
 */
export function AgentDetailInspector({
  agent,
  runtime,
  owner,
  runtimes,
  members,
  currentUserId,
  canEdit,
  onUpdate,
}: InspectorProps) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const update = (data: Record<string, unknown>) => onUpdate(agent.id, data);
  const isOnline = runtime?.status === "online";

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(300px,380px)]">
      <div className="space-y-4">
        <SettingsCard title={t(($) => $.inspector.section_profile)}>
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
            <AvatarEditor agent={agent} canEdit={canEdit} onUpdate={update} />
            <div className="min-w-0 flex-1">
              <NameAndDescription
                agent={agent}
                canEdit={canEdit}
                onUpdate={update}
              />
            </div>
          </div>
        </SettingsCard>

        <SettingsCard title={t(($) => $.inspector.section_execution)}>
          <PropertyGrid>
            <PropRow label={t(($) => $.inspector.prop_runtime)} interactive={false}>
              <RuntimePicker
                value={agent.runtime_id}
                runtimes={runtimes}
                members={members}
                currentUserId={currentUserId}
                canEdit={canEdit}
                onChange={(id) => update({ runtime_id: id })}
              />
            </PropRow>
            <PropRow label={t(($) => $.inspector.prop_model)} interactive={false}>
              <ModelPicker
                runtimeId={agent.runtime_id}
                runtimeOnline={!!isOnline}
                value={agent.model ?? ""}
                canEdit={canEdit}
                onChange={(m) => update({ model: m })}
              />
            </PropRow>
            <ThinkingPropRow
              runtimeId={agent.runtime_id}
              runtimeOnline={!!isOnline}
              provider={runtime?.provider ?? ""}
              model={agent.model ?? ""}
              value={agent.thinking_level ?? ""}
              canEdit={canEdit}
              onChange={(v) => update({ thinking_level: v })}
            />
            <PropRow label={t(($) => $.inspector.prop_concurrency)} interactive={false}>
              <ConcurrencyPicker
                value={agent.max_concurrent_tasks}
                canEdit={canEdit}
                onChange={(n) => update({ max_concurrent_tasks: n })}
              />
            </PropRow>
          </PropertyGrid>
        </SettingsCard>
      </div>

      <div className="space-y-4">
        <SettingsCard title={t(($) => $.inspector.section_access)}>
          <PropertyGrid>
            <PropRow label={t(($) => $.inspector.prop_visibility)} interactive={false}>
              <AccessPicker
                permissionMode={agent.permission_mode}
                invocationTargets={agent.invocation_targets}
                visibility={agent.visibility}
                members={members}
                canEdit={
                  currentUserId !== null && agent.owner_id === currentUserId
                }
                hasComposioAllowlist={
                  (agent.composio_toolkit_allowlist ?? []).length > 0
                }
                onChange={(next) => update(next)}
              />
            </PropRow>
          </PropertyGrid>
        </SettingsCard>

        <SettingsCard title={t(($) => $.inspector.section_details)}>
          <PropertyGrid>
            {owner && (
              <PropRow label={t(($) => $.inspector.prop_owner)} interactive={false}>
                <span className="flex min-w-0 items-center gap-1.5">
                  <ActorAvatar
                    actorType="member"
                    actorId={owner.user_id}
                    size="xs"
                  />
                  <span className="truncate">{owner.name}</span>
                </span>
              </PropRow>
            )}
            <PropRow label={t(($) => $.inspector.prop_created)} interactive={false}>
              <span className="text-muted-foreground">
                {timeAgo(agent.created_at)}
              </span>
            </PropRow>
            <PropRow label={t(($) => $.inspector.prop_updated)} interactive={false}>
              <span className="text-muted-foreground">
                {timeAgo(agent.updated_at)}
              </span>
            </PropRow>
          </PropertyGrid>
        </SettingsCard>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Layout helpers
// ---------------------------------------------------------------------------

function SettingsCard({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section className="rounded-lg border bg-background">
      <div className="border-b px-5 py-3.5">
        <h2 className="text-sm font-semibold">{title}</h2>
      </div>
      <div className="p-5">{children}</div>
    </section>
  );
}

function PropertyGrid({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1">
      {children}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Identity — avatar / name / description editors
// ---------------------------------------------------------------------------

function AvatarEditor({
  agent,
  canEdit,
  onUpdate,
}: {
  agent: Agent;
  canEdit: boolean;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
}) {
  if (!canEdit) {
    return (
      <div className="h-14 w-14 shrink-0 overflow-hidden rounded-full">
        <ActorAvatar actorType="agent" actorId={agent.id} size="2xl" />
      </div>
    );
  }

  return (
    <AvatarUploadControl
      variant="agent"
      value={agent.avatar_url ?? null}
      name={agent.name}
      size={56}
      onUploaded={(url) => onUpdate({ avatar_url: url })}
    />
  );
}

function NameAndDescription({
  agent,
  canEdit,
  onUpdate,
}: {
  agent: Agent;
  canEdit: boolean;
  onUpdate: (data: Record<string, unknown>) => Promise<void>;
}) {
  const { t } = useT("agents");
  if (!canEdit) {
    return (
      <div className="flex flex-col gap-1">
        <span className="text-base font-semibold leading-tight">
          {agent.name}
        </span>
        {agent.description ? (
          <span className="text-xs leading-relaxed text-muted-foreground">
            {agent.description}
          </span>
        ) : (
          <span className="text-xs italic leading-relaxed text-muted-foreground/50">
            {t(($) => $.inspector.no_description_placeholder)}
          </span>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1">
      <InlineEditPopover
        value={agent.name}
        onSave={(v) => onUpdate({ name: v.trim() })}
        kind="input"
        title={t(($) => $.inspector.rename_title)}
        placeholder={t(($) => $.inspector.rename_placeholder)}
        validate={(v) => (v.trim().length > 0 ? null : t(($) => $.inspector.rename_required))}
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

      <DescriptionEditor
        value={agent.description ?? ""}
        onSave={(v) => onUpdate({ description: v })}
      />
    </div>
  );
}

// Description editor — modal because the description benefits from a roomy
// composition surface (the inline popover was 288 px wide × 3 rows, too
// cramped to read or edit anything substantial). Name stays in the inline
// popover above: a single line is the right shape for it.
//
// The editor body is split into a child component that mounts only while
// the dialog is open. That way the draft state is initialised from `value`
// at mount time and never reset by an external update mid-edit — closing
// the dialog unmounts the body, reopening starts fresh with the latest
// value. This is the React-recommended replacement for the
// `useEffect(reset, [value])` anti-pattern (see "You Might Not Need an
// Effect" — Resetting state with a key / mount).
function DescriptionEditor({
  value,
  onSave,
}: {
  value: string;
  onSave: (next: string) => Promise<void>;
}) {
  const { t } = useT("agents");
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="group -mx-1 inline-flex items-start gap-1.5 self-start rounded px-1 text-left text-xs leading-relaxed transition-colors hover:bg-accent/50"
      >
        {value ? (
          <span className="text-muted-foreground">{value}</span>
        ) : (
          <span className="italic text-muted-foreground/50">{t(($) => $.inspector.no_description_placeholder)}</span>
        )}
        <Pencil className="mt-0.5 h-3 w-3 shrink-0 text-muted-foreground/0 transition-colors group-hover:text-muted-foreground" />
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-lg">
          {open && (
            <DescriptionEditorBody
              initialValue={value}
              onSave={onSave}
              onClose={() => setOpen(false)}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}

function DescriptionEditorBody({
  initialValue,
  onSave,
  onClose,
}: {
  initialValue: string;
  onSave: (next: string) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const [draft, setDraft] = useState(initialValue);
  const [saving, setSaving] = useState(false);

  const length = [...draft].length;
  const overLimit = length > AGENT_DESCRIPTION_MAX_LENGTH;
  const dirty = draft !== initialValue;

  const commit = async () => {
    if (overLimit || !dirty) return;
    setSaving(true);
    try {
      await onSave(draft);
      onClose();
    } catch {
      // toast handled by parent's onUpdate
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.inspector.edit_description_title)}</DialogTitle>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <textarea
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={t(($) => $.inspector.description_placeholder)}
          rows={6}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              onClose();
              return;
            }
            if (isImeComposing(e)) return;
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              void commit();
            }
          }}
          className="w-full resize-none rounded-md border bg-transparent px-3 py-2 text-sm outline-none focus-visible:border-input"
        />
        <CharCounter length={length} max={AGENT_DESCRIPTION_MAX_LENGTH} />
      </div>
      <DialogFooter>
        <Button
          variant="ghost"
          size="sm"
          onClick={onClose}
          disabled={saving}
        >
          {t(($) => $.inspector.cancel)}
        </Button>
        <Button
          size="sm"
          onClick={() => void commit()}
          disabled={saving || overLimit || !dirty}
        >
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />
          ) : (
            t(($) => $.inspector.save)
          )}
        </Button>
      </DialogFooter>
    </>
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
  const { t } = useT("agents");
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
                if (e.key === "Escape") {
                  setOpen(false);
                  return;
                }
                if (isImeComposing(e)) return;
                if (e.key === "Enter") {
                  e.preventDefault();
                  void commit();
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
                if (e.key === "Escape") {
                  setOpen(false);
                  return;
                }
                if (isImeComposing(e)) return;
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
              {t(($) => $.inspector.cancel)}
            </Button>
            <Button
              size="sm"
              onClick={() => void commit()}
              disabled={saving || draft === value}
            >
              {saving ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin motion-reduce:animate-none" />
              ) : (
                t(($) => $.inspector.save)
              )}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
