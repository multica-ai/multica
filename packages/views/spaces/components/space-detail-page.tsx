"use client";

import { useEffect, useMemo, useState } from "react";
import { ChevronDown, Search } from "lucide-react";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { spaceListOptions, spaceMembersOptions } from "@multica/core/spaces/queries";
import {
  useArchiveSpace,
  useReplaceSpaceMembers,
  useUpdateSpace,
  useUpdateSpaceMemberRole,
} from "@multica/core/spaces/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspacePaths } from "@multica/core/paths";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { memberListOptions } from "@multica/core/workspace/queries";
import { sanitizeSpaceKeyInput, isValidSpaceKey, RESERVED_SPACE_KEYS } from "@multica/core/workspace";
import type { Space } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { SpaceIcon } from "./space-icon";
import { useT } from "../../i18n";

/**
 * Space settings — /space/:key/settings. Identity fields apply inline, then
 * membership and lifecycle controls follow in the same narrow column.
 */
export function SpaceSettingsPage({ spaceKey }: { spaceKey: string }) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  // Full list (not active-only): an archived space's settings stay viewable.
  const { data: spaces = [], isSuccess } = useQuery(spaceListOptions(wsId));
  const space = spaces.find((tm) => tm.key.toLowerCase() === spaceKey.toLowerCase());
  // The server protects the configured Default Space separately. This count
  // keeps the older, complementary invariant visible too: a workspace must
  // never archive its final active Space.
  const isLastActiveSpace = spaces.filter((s) => !s.archived_at).length <= 1;

  if (!space) {
    return isSuccess ? (
      <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.surface.not_found)}
      </div>
    ) : null;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-2">
        <SpaceIcon space={space} />
        <h1 className="text-sm font-medium">{space.name}</h1>
        <span className="text-sm text-muted-foreground">/</span>
        <span className="text-sm text-muted-foreground">
          {t(($) => $.settings.title)}
        </span>
        {space.is_default && <Badge variant="secondary">{t(($) => $.state.default)}</Badge>}
        {space.archived_at && <Badge variant="outline">{t(($) => $.state.archived)}</Badge>}
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-8 px-6 py-8">
          <Identity space={space} />
          <MembersSection space={space} isLastActiveSpace={isLastActiveSpace} />
          <ArchiveSection space={space} isLastActiveSpace={isLastActiveSpace} />
        </div>
      </div>
    </div>
  );
}

/**
 * Icon + name rendered as page content. The icon applies on pick; name
 * commits on blur (Escape restores) — there is no save button anywhere on
 * this page.
 */
function Identity({ space }: { space: Space }) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";
  const canManage = isAdmin || space.member_role === "lead" || space.member_role === "admin";
  const navigation = useNavigation();
  const p = useWorkspacePaths();
  const updateSpace = useUpdateSpace();
  const [name, setName] = useState(space.name);
  const [keyDraft, setKeyDraft] = useState(space.key);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [pendingKey, setPendingKey] = useState<string | null>(null);

  // Re-seed when navigating between spaces (or after a save round-trips).
  useEffect(() => {
    setName(space.name);
    setKeyDraft(space.key);
  }, [space.id, space.name, space.key]);

  // The Identifier doubles as the issue-number prefix (ENG-1) and is the
  // workspace-wide issue namespace, so key changes are admin-only. Renaming a
  // space that already has issues is allowed — the server records aliases so
  // old OLDKEY-N references keep resolving — but we confirm first (below).
  const canEditKey = canManage;
  const keyReserved = RESERVED_SPACE_KEYS.has(keyDraft);
  const keyStartsWithDigit = /^[0-9]/.test(keyDraft);
  const keyError = keyDraft.length > 0 && !isValidSpaceKey(keyDraft);

  const saveField = async (patch: { name?: string; icon?: string | null; visibility?: "open" | "private" }) => {
    try {
      await updateSpace.mutateAsync({ id: space.id, ...patch });
      toast.success(t(($) => $.toast_updated));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_save_failed),
      );
    }
  };

  const commitName = () => {
    const next = name.trim();
    if (!next) {
      setName(space.name);
      return;
    }
    if (next !== space.name) void saveField({ name: next });
  };

  const doRename = async (nextKey: string) => {
    try {
      await updateSpace.mutateAsync({ id: space.id, key: nextKey });
      toast.success(t(($) => $.toast_updated));
      // The Identifier is the space's URL segment — move to the new URL so
      // the current /space/:key route keeps resolving.
      navigation.replace(p.spaceDetail(nextKey));
    } catch (err) {
      setKeyDraft(space.key);
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_save_failed),
      );
    }
  };

  const commitKey = () => {
    if (!canEditKey) return;
    if (!isValidSpaceKey(keyDraft) || keyDraft === space.key) {
      setKeyDraft(space.key);
      return;
    }
    // A rename rewrites every issue identifier in the space, so confirm first
    // when the space already holds issues. Keep the field on the pending value.
    if (space.issue_counter > 0) {
      setPendingKey(keyDraft);
      return;
    }
    void doRename(keyDraft);
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center gap-3">
        {/* Icon is emoji-only and applies immediately on pick. */}
        <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
          <PopoverTrigger
            render={
              <button
                type="button"
                aria-label={t(($) => $.form.icon)}
                disabled={!canManage}
                className="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-lg bg-muted/60 text-2xl transition-colors hover:bg-accent"
              />
            }
          >
            {space.icon || <SpaceIcon space={{ icon: null }} className="size-5" />}
          </PopoverTrigger>
          <PopoverContent align="start" className="w-auto p-0">
            <EmojiPicker
              onSelect={(emoji) => {
                setIconPickerOpen(false);
                void saveField({ icon: emoji });
              }}
            />
          </PopoverContent>
        </Popover>
        <Input
          aria-label={t(($) => $.form.name)}
          value={name}
          disabled={!canManage}
          onChange={(event) => setName(event.target.value)}
          onBlur={commitName}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur();
            if (event.key === "Escape") {
              setName(space.name);
              event.currentTarget.blur();
            }
          }}
          className="h-auto rounded-none border-0 bg-transparent px-0 py-1 !text-2xl font-bold leading-snug tracking-tight shadow-none focus-visible:ring-0 dark:bg-transparent"
        />
      </div>
      <div className="flex flex-col gap-1 pt-1">
        <Label htmlFor="space-key" className="text-xs font-medium text-muted-foreground">
          {t(($) => $.form.key)}
        </Label>
        <Input
          id="space-key"
          value={keyDraft}
          onChange={(event) => setKeyDraft(sanitizeSpaceKeyInput(event.target.value))}
          onBlur={commitKey}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur();
            if (event.key === "Escape") {
              setKeyDraft(space.key);
              event.currentTarget.blur();
            }
          }}
          disabled={!canEditKey}
          maxLength={7}
          placeholder={space.key}
          variant="underline"
          className={cn("max-w-40 font-mono", keyError && "border-destructive")}
        />
        <p className={cn("text-xs", keyError ? "text-destructive" : "text-muted-foreground")}>
          {keyError && keyReserved
            ? t(($) => $.form.key_reserved)
            : keyError && keyStartsWithDigit
              ? t(($) => $.form.key_start_letter)
              : t(($) => $.form.key_hint)}
        </p>
      </div>
      <div className="flex flex-col gap-1 pt-1">
        <Label className="text-xs font-medium text-muted-foreground">
          {t(($) => $.form.visibility)}
        </Label>
        <Select
          value={space.visibility}
          disabled={!canManage}
          onValueChange={(value) => {
            if (value === "open" || value === "private") {
              void saveField({ visibility: value });
            }
          }}
        >
          <SelectTrigger size="sm" className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="open">{t(($) => $.form.visibility_open)}</SelectItem>
            <SelectItem value="private" disabled={space.is_default}>
              {t(($) => $.form.visibility_private)}
            </SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {space.is_default
            ? t(($) => $.form.visibility_default_hint)
            : space.visibility === "private"
              ? t(($) => $.form.visibility_private_hint)
              : t(($) => $.form.visibility_open_hint)}
        </p>
      </div>

      <Dialog
        open={pendingKey !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPendingKey(null);
            setKeyDraft(space.key);
          }
        }}
      >
        <DialogContent showCloseButton={false} className="sm:max-w-sm">
          <DialogTitle>{t(($) => $.form.key_rename_confirm_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.form.key_rename_confirm_body, {
              count: space.issue_counter,
              oldKey: space.key,
              newKey: pendingKey ?? "",
            })}
          </DialogDescription>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setPendingKey(null);
                setKeyDraft(space.key);
              }}
            >
              {t(($) => $.actions.cancel)}
            </Button>
            <Button
              onClick={() => {
                const next = pendingKey;
                setPendingKey(null);
                if (next) void doRename(next);
              }}
            >
              {t(($) => $.actions.save)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/**
 * Member stack + config popover. The checkbox set is the full source of
 * truth; saving replaces the space's membership wholesale. Deselecting
 * everyone means the space has no reason to exist — saving then archives it,
 * behind a confirm (blocked when this is the workspace's last active space).
 */
function MembersSection({ space, isLastActiveSpace }: { space: Space; isLastActiveSpace: boolean }) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";
  const canManage = isAdmin || space.member_role === "lead" || space.member_role === "admin";
  const { data: members = [] } = useQuery(spaceMembersOptions(wsId, space.id));
  const { data: allMembers = [] } = useQuery(memberListOptions(wsId));
  const replaceMembers = useReplaceSpaceMembers();
  const updateMemberRole = useUpdateSpaceMemberRole();
  const archiveSpace = useArchiveSpace();

  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [confirmArchive, setConfirmArchive] = useState(false);
  const [saving, setSaving] = useState(false);

  // Seed the checkbox set from the live membership each time the panel opens.
  useEffect(() => {
    if (open) {
      setSelected(members.map((member) => member.user_id));
      setSearch("");
    }
  }, [open, members]);

  const filteredMembers = useMemo(() => {
    const q = search.trim().toLowerCase();
    return allMembers.filter(
      (member) =>
        !q ||
        member.name.toLowerCase().includes(q) ||
        member.email.toLowerCase().includes(q),
    );
  }, [allMembers, search]);
  const membershipByUser = useMemo(
    () => new Map(members.map((member) => [member.user_id, member])),
    [members],
  );

  const toggle = (userId: string) =>
    setSelected((prev) =>
      prev.includes(userId) ? prev.filter((id) => id !== userId) : [...prev, userId],
    );

  const doArchive = async () => {
    setConfirmArchive(false);
    setOpen(false);
    try {
      await archiveSpace.mutateAsync(space.id);
      toast.success(t(($) => $.toast_archived));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_archive_failed),
      );
    }
  };

  const save = async () => {
    if (selected.length === 0) {
      if (isLastActiveSpace) {
        toast.error(t(($) => $.settings.last_space_cannot_archive));
        return;
      }
      // Empty membership funnels into archive, which is admin-only —
      // pre-check here so members get the reason instead of a raw 403.
      if (!canManage) {
        toast.error(t(($) => $.settings.archive_admin_only));
        return;
      }
      setConfirmArchive(true);
      return;
    }
    setSaving(true);
    try {
      await replaceMembers.mutateAsync({ id: space.id, member_ids: selected });
      toast.success(t(($) => $.toast_updated));
      setOpen(false);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_save_failed),
      );
    } finally {
      setSaving(false);
    }
  };

  const stack = members.slice(0, 5);
  const overflow = members.length - stack.length;

  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-xs font-medium text-muted-foreground">
        {t(($) => $.settings.members)}
      </h3>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="outline"
              disabled={!canManage}
              className="h-auto min-w-48 max-w-full self-start justify-start gap-2 py-1.5 font-normal"
            />
          }
        >
          {members.length > 0 ? (
            <span className="flex items-center -space-x-1.5">
              {stack.map((member) => (
                <span key={member.user_id} className="rounded-full ring-2 ring-background">
                  <ActorAvatar
                    name={member.name}
                    initials={(member.name || member.email || "?").charAt(0).toUpperCase()}
                    avatarUrl={member.avatar_url}
                    size="sm"
                  />
                </span>
              ))}
              {overflow > 0 && (
                <span className="flex size-5 items-center justify-center rounded-full bg-muted text-[10px] font-medium text-muted-foreground ring-2 ring-background">
                  +{overflow}
                </span>
              )}
            </span>
          ) : (
            <span className="text-sm text-muted-foreground">
              {t(($) => $.settings.members_empty)}
            </span>
          )}
          <ChevronDown className="ml-auto size-3 text-muted-foreground" />
        </PopoverTrigger>
        <PopoverContent align="start" className="flex w-80 flex-col gap-2 p-3">
          <div className="relative shrink-0">
            <Search className="absolute left-0 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              autoFocus
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t(($) => $.dialog.member_search)}
              variant="underline"
              className="pl-6"
            />
          </div>
          <div className="max-h-64 min-h-0 overflow-y-auto overflow-x-hidden">
            {filteredMembers.map((member) => {
              const membership = membershipByUser.get(member.user_id);
              return (
              <div
                key={member.user_id}
                className="flex items-center gap-2 border-b px-1 py-2 transition-colors last:border-b-0 hover:bg-accent/40"
              >
                <ActorAvatar
                  name={member.name}
                  initials={(member.name || member.email || "?").charAt(0).toUpperCase()}
                  avatarUrl={member.avatar_url}
                  size="md"
                />
                <span className="min-w-0 flex-1 truncate text-sm">
                  {member.name || member.email}
                </span>
                {membership && canManage && selected.includes(member.user_id) && (
                  <Select
                    value={membership.role}
                    disabled={updateMemberRole.isPending}
                    onValueChange={(value) => {
                      if (value === "lead" || value === "admin" || value === "member" || value === "guest") {
                        void updateMemberRole.mutateAsync({
                          id: space.id,
                          userId: member.user_id,
                          role: value,
                        }).catch((err: unknown) => {
                          toast.error(err instanceof Error ? err.message : t(($) => $.toast_save_failed));
                        });
                      }
                    }}
                  >
                    <SelectTrigger size="sm" className="w-24">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {(["lead", "admin", "member", "guest"] as const).map((role) => (
                        <SelectItem key={role} value={role}>{role}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                <Checkbox
                  checked={selected.includes(member.user_id)}
                  onCheckedChange={() => toggle(member.user_id)}
                />
              </div>
              );
            })}
            {filteredMembers.length === 0 && (
              <div className="px-1 py-4 text-center text-sm text-muted-foreground">
                {t(($) => $.dialog.member_search_empty)}
              </div>
            )}
          </div>
          <div className="flex items-center justify-between pt-1">
            <span className="text-xs text-muted-foreground">
              {t(($) => $.dialog.member_count, { count: selected.length })}
            </span>
            <Button size="sm" onClick={save} disabled={saving}>
              {saving ? t(($) => $.actions.saving) : t(($) => $.actions.save)}
            </Button>
          </div>
        </PopoverContent>
      </Popover>

      <Dialog open={confirmArchive} onOpenChange={setConfirmArchive}>
        <DialogContent showCloseButton={false} className="sm:max-w-sm">
          <DialogTitle>{t(($) => $.settings.archive_confirm_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.settings.archive_confirm_body, { name: space.name })}
          </DialogDescription>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmArchive(false)}>
              {t(($) => $.actions.cancel)}
            </Button>
            <Button variant="destructive" onClick={doArchive}>
              {t(($) => $.actions.archive)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ArchiveSection({
  space,
  isLastActiveSpace,
}: {
  space: Space;
  isLastActiveSpace: boolean;
}) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";
  const canManage = isAdmin || space.member_role === "lead" || space.member_role === "admin";
  const archiveSpace = useArchiveSpace();
  const [confirmOpen, setConfirmOpen] = useState(false);

  // Only an already-archived space drops the section (archiving it again is
  // meaningless). Every other blocked state renders disabled with the reason
  // in a tooltip — nothing is hidden, so the rule is always discoverable.
  if (space.archived_at) return null;
  const blockedReason = space.is_default
    ? "default"
    : isLastActiveSpace
    ? "last"
    : !canManage
      ? "admin"
      : null;

  const doArchive = async () => {
    setConfirmOpen(false);
    try {
      await archiveSpace.mutateAsync(space.id);
      toast.success(t(($) => $.toast_archived));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_archive_failed),
      );
    }
  };

  return (
    <div className="flex flex-col gap-1">
      <h3 className="mb-1 text-xs font-medium text-muted-foreground">
        {t(($) => $.settings.danger_title)}
      </h3>
      {/* Archiving is admin-only and never allowed on a workspace's last
          active space (server enforces both). Blocked states stay visible
          but disabled with the reason, so users learn the rule instead of
          wondering where the action went. */}
      <Tooltip>
        <TooltipTrigger render={<span className="inline-flex w-fit" />}>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            disabled={blockedReason !== null}
            onClick={() => setConfirmOpen(true)}
          >
            {t(($) => $.actions.archive)}
          </Button>
        </TooltipTrigger>
        {blockedReason !== null && (
          <TooltipContent>
            {blockedReason === "default"
              ? t(($) => $.settings.default_space_cannot_archive)
              : blockedReason === "last"
              ? t(($) => $.settings.last_space_cannot_archive)
              : t(($) => $.settings.archive_admin_only)}
          </TooltipContent>
        )}
      </Tooltip>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent showCloseButton={false} className="sm:max-w-sm">
          <DialogTitle>{t(($) => $.settings.archive_confirm_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.settings.archive_confirm_body, { name: space.name })}
          </DialogDescription>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirmOpen(false)}>
              {t(($) => $.actions.cancel)}
            </Button>
            <Button variant="destructive" onClick={doArchive}>
              {t(($) => $.actions.archive)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
