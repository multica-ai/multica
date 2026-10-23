"use client";

import { useMemo, useState, type ComponentProps } from "react";
import { Globe, Lock, Users } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import {
  RevokeRuntimeAccessDialog,
  parseRuntimeAccessRevocationPlan,
  type RuntimeAccessRevocationPlan,
} from "./revoke-runtime-access-dialog";

type RuntimeAccessScope = "private" | "public" | "members";

// Below this size the member list fits without scrolling, so a search field
// would only add noise.
const MEMBER_SEARCH_THRESHOLD = 7;

const SCOPE_ICON = { private: Lock, public: Globe, members: Users } as const;

export function runtimeAccessScope(runtime: AgentRuntime): RuntimeAccessScope {
  if (runtime.visibility === "public") return "public";
  return (runtime.shared_with?.length ?? 0) > 0 ? "members" : "private";
}

export function RuntimeAccessReadout({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const scope = runtimeAccessScope(runtime);
  const Icon = SCOPE_ICON[scope];
  return (
    <Tooltip>
      <TooltipTrigger render={
        <span className="inline-flex items-center gap-1.5 rounded-md border bg-muted/30 px-2 py-1.5 text-caption">
          <Icon className="h-3 w-3 text-muted-foreground" />
          <span className="font-medium">{t(($) => $.detail.visibility_label[scope])}</span>
        </span>
      } />
      <TooltipContent>{t(($) => $.detail.visibility_hint_readonly[scope])}</TooltipContent>
    </Tooltip>
  );
}

/**
 * Owner-only access control. Private and Public apply in one click, as before
 * member sharing existed; restricting access still goes through the reviewed
 * revocation dialog when the server reports affected agents or runs. Choosing
 * members opens a draft popover that persists only on Save, so a partial
 * selection is never shared.
 */
export function RuntimeAccessEditor({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const updateRuntime = useUpdateRuntime(wsId);
  const current = runtimeAccessScope(runtime);
  const persisted = useMemo(() => runtime.shared_with ?? [], [runtime.shared_with]);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [revokePlan, setRevokePlan] = useState<RuntimeAccessRevocationPlan | null>(null);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [revokeMembers, setRevokeMembers] = useState<string[]>([]);
  const disabled = updateRuntime.isPending || revokeOpen;

  const recipients = useMemo(
    () => members.filter((member) => persisted.includes(member.user_id)),
    [members, persisted],
  );

  const apply = (scope: RuntimeAccessScope, sharedWith: string[], onApplied?: () => void) => {
    updateRuntime.mutate(
      {
        runtimeId: runtime.id,
        patch: { visibility: scope === "public" ? "public" : "private", shared_with: sharedWith },
      },
      {
        onSuccess: () => {
          onApplied?.();
          toast.success(t(($) => $.detail.visibility_toast_updated, {
            visibility: t(($) => $.detail.visibility_label[scope]),
          }));
        },
        onError: (err) => {
          const plan = parseRuntimeAccessRevocationPlan(err);
          if (plan) {
            onApplied?.();
            setRevokeMembers(sharedWith);
            setRevokePlan(plan);
            setRevokeOpen(true);
            return;
          }
          toast.error(err instanceof Error && err.message ? err.message : t(($) => $.detail.visibility_toast_failed));
        },
      },
    );
  };

  return (
    <div className="space-y-2">
      <div
        role="group"
        aria-label={t(($) => $.detail.diagnostics_visibility)}
        className="flex w-full items-center gap-0.5 rounded-md bg-muted p-0.5"
      >
        {(["private", "public"] as const).map((scope) => (
          <ScopeButton
            key={scope}
            scope={scope}
            active={current === scope}
            disabled={disabled}
            onClick={() => {
              if (current !== scope) apply(scope, []);
            }}
          />
        ))}
        <Popover open={pickerOpen} onOpenChange={(open) => setPickerOpen(open && !disabled)}>
          <PopoverTrigger
            render={<ScopeButton scope="members" active={current === "members"} disabled={disabled} />}
          />
          <PopoverContent align="end" className="w-72 p-0">
            {pickerOpen ? (
              <MemberPicker
                members={members.filter((member) => member.user_id !== runtime.owner_id)}
                initial={persisted}
                saving={updateRuntime.isPending}
                onCancel={() => setPickerOpen(false)}
                onSave={(ids) => apply("members", ids, () => setPickerOpen(false))}
              />
            ) : null}
          </PopoverContent>
        </Popover>
      </div>
      {current === "members" ? (
        <button
          type="button"
          disabled={disabled}
          onClick={() => setPickerOpen(true)}
          className="group flex w-full items-center gap-2 rounded-md px-1 py-1 text-left text-caption text-muted-foreground hover:bg-muted hover:text-foreground disabled:pointer-events-none"
        >
          <span className="flex shrink-0 -space-x-1.5">
            {recipients.slice(0, 4).map((member) => (
              <ActorAvatar key={member.user_id} actorType="member" actorId={member.user_id} size="xs" />
            ))}
          </span>
          <span className="min-w-0 truncate">
            {recipients.length > 0
              ? recipients.map((member) => member.name).join(", ")
              : t(($) => $.detail.visibility_members_count, { count: persisted.length })}
          </span>
          <span className="ml-auto shrink-0 font-medium text-foreground">
            {t(($) => $.detail.visibility_members_edit)}
          </span>
        </button>
      ) : null}
      <RevokeRuntimeAccessDialog open={revokeOpen} onOpenChange={setRevokeOpen}
        runtime={runtime} wsId={wsId} initialPlan={revokePlan} sharedWith={revokeMembers} />
    </div>
  );
}

function ScopeButton({
  scope,
  active,
  disabled,
  ...props
}: {
  scope: RuntimeAccessScope;
  active: boolean;
  disabled: boolean;
} & ComponentProps<"button">) {
  const { t } = useT("runtimes");
  const Icon = SCOPE_ICON[scope];
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            {...props}
            aria-pressed={active}
            disabled={disabled}
            className={`inline-flex min-w-0 flex-1 items-center justify-center gap-1.5 rounded-xs px-2 py-1 text-caption font-medium transition-colors ${
              active
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            } disabled:cursor-not-allowed disabled:opacity-60`}
          >
            <Icon className="size-3 shrink-0" aria-hidden="true" />
            <span className="truncate">{t(($) => $.detail.visibility_choice[scope])}</span>
          </button>
        }
      />
      <TooltipContent>{t(($) => $.detail.visibility_hint[scope])}</TooltipContent>
    </Tooltip>
  );
}

function MemberPicker({
  members,
  initial,
  saving,
  onCancel,
  onSave,
}: {
  members: Array<{ user_id: string; name: string }>;
  initial: string[];
  saving: boolean;
  onCancel: () => void;
  onSave: (ids: string[]) => void;
}) {
  const { t } = useT("runtimes");
  const { t: tc } = useT("common");
  const [selected, setSelected] = useState(initial);
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const visible = needle
    ? members.filter((member) => member.name.toLowerCase().includes(needle))
    : members;
  // Order-insensitive: the server stores the list sorted.
  const dirty = selected.length !== initial.length || selected.some((id) => !initial.includes(id));

  return (
    <div className="flex flex-col">
      <div className="space-y-2 border-b px-3 py-2.5">
        <p className="text-caption font-medium">{t(($) => $.detail.visibility_members_title)}</p>
        {members.length >= MEMBER_SEARCH_THRESHOLD ? (
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t(($) => $.detail.visibility_members_search)}
            aria-label={t(($) => $.detail.visibility_members_search)}
            className="h-8"
            autoFocus
          />
        ) : null}
      </div>
      <div className="max-h-64 overflow-y-auto overscroll-contain py-1">
        {members.length === 0 || visible.length === 0 ? (
          <p className="px-3 py-3 text-caption text-muted-foreground">
            {members.length === 0
              ? t(($) => $.detail.visibility_members_none)
              : t(($) => $.detail.visibility_members_no_match)}
          </p>
        ) : visible.map((member) => {
          const id = `runtime-share-${member.user_id}`;
          return (
            <div key={member.user_id} className="flex items-center gap-2 px-3 py-1.5 hover:bg-surface-hover">
              <Checkbox
                id={id}
                checked={selected.includes(member.user_id)}
                disabled={saving}
                onCheckedChange={(checked) => setSelected((ids) => checked === true
                  ? [...ids, member.user_id]
                  : ids.filter((value) => value !== member.user_id))}
              />
              <label htmlFor={id} className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
                <ActorAvatar actorType="member" actorId={member.user_id} size="sm" />
                <span className="truncate text-caption">{member.name}</span>
              </label>
            </div>
          );
        })}
      </div>
      <div className="flex items-center justify-end gap-2 border-t px-3 py-2">
        <Button type="button" variant="outline" size="sm" onClick={onCancel} disabled={saving}>
          {tc(($) => $.cancel)}
        </Button>
        <Button
          type="button"
          size="sm"
          onClick={() => onSave(selected)}
          disabled={saving || !dirty || selected.length === 0}
          aria-busy={saving}
        >
          {tc(($) => $.save)}
        </Button>
      </div>
    </div>
  );
}
