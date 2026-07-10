"use client";

import { useState, type FormEvent } from "react";
import { ArrowLeft, ChevronDown, Search } from "lucide-react";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { sanitizeSpaceKeyInput, isValidSpaceKey, RESERVED_SPACE_KEYS } from "@multica/core/workspace";
import { useCreateSpace } from "@multica/core/spaces/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
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
 * Create-space page — /space/new, a static sibling of /space/:key ("new" is
 * a reserved space key so it can never collide with a real space's detail
 * page). Layout mirrors SpaceDetailPage's single max-w-4xl column so create
 * and edit read as the same surface; unlike the edit page's blur-commit
 * fields, this one needs an explicit submit — there's no space.id to PATCH
 * against until the create call returns.
 */
export function CreateSpacePage() {
  const { t } = useT("spaces");
  const p = useWorkspacePaths();
  const navigation = useNavigation();
  const createSpace = useCreateSpace();

  const [name, setName] = useState("");
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [icon, setIcon] = useState("");
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [memberIds, setMemberIds] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  // Validation is quiet until the user tries to submit — a fresh form should
  // not flash "name required" just because the field was focused and left.
  const [submitAttempted, setSubmitAttempted] = useState(false);

  const nameError = submitAttempted && name.trim().length === 0;
  const keyReserved = RESERVED_SPACE_KEYS.has(key);
  const keyStartsWithDigit = /^[0-9]/.test(key);
  const keyError = (keyTouched || submitAttempted) && key.length > 0 && !isValidSpaceKey(key);
  const canSubmit = name.trim().length > 0 && isValidSpaceKey(key) && !submitting;

  // The identifier follows the name until the user edits it directly. Only a
  // valid derivation is applied; a name that can't yield a letter-first key
  // (CJK, digit-leading) leaves the field empty so the placeholder prompts a
  // manual entry rather than seeding an invalid key.
  const handleNameChange = (value: string) => {
    setName(value);
    if (!keyTouched) {
      const derived = sanitizeSpaceKeyInput(value);
      setKey(isValidSpaceKey(derived) ? derived : "");
    }
  };

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitAttempted(true);
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      const space = await createSpace.mutateAsync({
        name: name.trim(),
        key,
        icon: icon.trim() || null,
        member_ids: memberIds,
      });
      toast.success(t(($) => $.toast_created));
      navigation.replace(p.spaceDetail(space.key));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_save_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="justify-between">
        <h1 className="text-sm font-medium">{t(($) => $.dialog.create_title)}</h1>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          onClick={() => navigation.back()}
        >
          <ArrowLeft className="h-4 w-4" />
          {t(($) => $.actions.cancel)}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <form
          onSubmit={submit}
          autoComplete="off"
          className="mx-auto flex w-full max-w-xl flex-col gap-8 px-6 py-8"
        >
          {/* Basics — icon+name and identifier as label-left / control-right rows. */}
          <div className="flex flex-col gap-3">
            <h2 className="text-sm font-semibold">{t(($) => $.dialog.section_basics)}</h2>
            <Card>
              <CardContent className="divide-y p-0">
                <div className="flex items-center justify-between gap-6 px-4 py-3">
                  <span className="text-sm font-medium">{t(($) => $.form.icon_name)}</span>
                  <div className="flex items-center gap-2">
                    {/* Icon is emoji-only — picked, never typed. */}
                    <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
                      <PopoverTrigger
                        render={
                          <button
                            type="button"
                            aria-label={t(($) => $.form.icon)}
                            className="flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-md border bg-muted/40 text-xl transition-colors hover:bg-accent"
                          />
                        }
                      >
                        {icon || <SpaceIcon space={{ icon: null }} className="size-4" />}
                      </PopoverTrigger>
                      <PopoverContent align="start" className="w-auto p-0">
                        <EmojiPicker
                          onSelect={(emoji) => {
                            setIcon(emoji);
                            setIconPickerOpen(false);
                          }}
                        />
                      </PopoverContent>
                    </Popover>
                    <Input
                      id="space-name"
                      autoFocus
                      aria-label={t(($) => $.form.name)}
                      value={name}
                      onChange={(event) => handleNameChange(event.target.value)}
                      aria-invalid={nameError}
                      placeholder={t(($) => $.form.name_placeholder)}
                      className="w-60"
                    />
                  </div>
                </div>

                <div className="flex items-center justify-between gap-6 px-4 py-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium">{t(($) => $.form.key)}</div>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {t(($) => $.form.key_hint_short)}
                    </p>
                  </div>
                  <Input
                    id="space-key"
                    value={key}
                    onChange={(event) => {
                      setKey(sanitizeSpaceKeyInput(event.target.value));
                      setKeyTouched(true);
                    }}
                    aria-invalid={keyError}
                    placeholder="ENG"
                    maxLength={7}
                    className="w-40 shrink-0 font-mono"
                  />
                </div>
              </CardContent>
            </Card>
            {(nameError || keyError) && (
              <p className="px-1 text-xs text-destructive">
                {nameError
                  ? t(($) => $.dialog.name_required)
                  : keyReserved
                    ? t(($) => $.form.key_reserved)
                    : keyStartsWithDigit
                      ? t(($) => $.form.key_start_letter)
                      : t(($) => $.form.key_hint)}
              </p>
            )}
          </div>

          {/* Members — header + inline picker, mirroring the space detail page's
              MembersSection (no card wrapper for a single trigger). */}
          <div className="flex flex-col gap-3">
            <h2 className="text-sm font-semibold">{t(($) => $.dialog.members_entry)}</h2>
            <MembersField memberIds={memberIds} onChange={setMemberIds} />
          </div>

          <div className="flex justify-end">
            <Button type="submit" disabled={!canSubmit}>
              {submitting ? t(($) => $.actions.saving) : t(($) => $.actions.save)}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

/**
 * Member picker for the create page — same avatar-stack + popover-checkbox
 * shape as SpaceDetailPage's MembersSection, but against local unsaved state
 * (the space doesn't exist yet, so there's nothing to PATCH). The creator is
 * always in and always lead, matching what CreateSpace does server-side.
 */
function MembersField({
  memberIds,
  onChange,
}: {
  memberIds: string[];
  onChange: (ids: string[]) => void;
}) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const currentUser = useAuthStore((s) => s.user);
  const { data: allMembers = [] } = useQuery(memberListOptions(wsId));

  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  const invitableMembers = allMembers
    .filter((member) => member.user_id !== currentUser?.id)
    .filter((member) => {
      const q = search.trim().toLowerCase();
      return (
        !q ||
        member.name.toLowerCase().includes(q) ||
        member.email.toLowerCase().includes(q)
      );
    });

  const toggle = (userId: string) =>
    onChange(
      memberIds.includes(userId)
        ? memberIds.filter((id) => id !== userId)
        : [...memberIds, userId],
    );

  const invitedMembers = allMembers.filter((member) => memberIds.includes(member.user_id));
  const stack = invitedMembers.slice(0, 4);
  const overflow = invitedMembers.length - stack.length;

  return (
    <div className="flex flex-col gap-2">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              type="button"
              variant="outline"
              className="h-auto min-w-48 max-w-full self-start justify-start gap-2 py-1.5 font-normal"
            />
          }
        >
          <span className="flex items-center -space-x-1.5">
            {currentUser && (
              <span className="rounded-full ring-2 ring-background">
                <ActorAvatar
                  name={currentUser.name}
                  initials={(currentUser.name || "?").charAt(0).toUpperCase()}
                  avatarUrl={currentUser.avatar_url ?? null}
                  size="sm"
                />
              </span>
            )}
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
          <span className="text-xs text-muted-foreground">
            {memberIds.length === 0
              ? t(($) => $.dialog.member_count_solo)
              : t(($) => $.dialog.member_count, { count: memberIds.length + 1 })}
          </span>
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
            {/* Creator — always in, always lead. */}
            {currentUser && (
              <div className="flex items-center gap-2 border-b px-1 py-2 opacity-80">
                <ActorAvatar
                  name={currentUser.name}
                  initials={(currentUser.name || "?").charAt(0).toUpperCase()}
                  avatarUrl={currentUser.avatar_url ?? null}
                  size="md"
                />
                <span className="min-w-0 flex-1 truncate text-sm">{currentUser.name}</span>
                <span className="text-xs text-muted-foreground">
                  {t(($) => $.dialog.creator_row)}
                </span>
              </div>
            )}
            {invitableMembers.map((member) => (
              <label
                key={member.user_id}
                className="flex cursor-pointer items-center gap-2 border-b px-1 py-2 transition-colors last:border-b-0 hover:bg-accent/40"
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
                <Checkbox
                  checked={memberIds.includes(member.user_id)}
                  onCheckedChange={() => toggle(member.user_id)}
                />
              </label>
            ))}
            {invitableMembers.length === 0 && (
              <div className="px-1 py-4 text-center text-sm text-muted-foreground">
                {t(($) => $.dialog.member_search_empty)}
              </div>
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}
