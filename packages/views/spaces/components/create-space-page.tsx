"use client";

import { useState, type FormEvent } from "react";
import { ArrowLeft, Search } from "lucide-react";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { PlainTextField } from "@multica/ui/components/common/plain-text-field";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { normalizeSpaceKey, isValidSpaceKey, RESERVED_SPACE_KEYS } from "@multica/core/workspace";
import { useCreateSpace } from "@multica/core/spaces/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
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

const underline =
  "rounded-none border-0 border-b border-input bg-transparent dark:bg-transparent px-0 shadow-none focus-visible:ring-0 focus-visible:border-foreground";

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
  const [nameTouched, setNameTouched] = useState(false);
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [icon, setIcon] = useState("");
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [description, setDescription] = useState("");
  const [memberIds, setMemberIds] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const normalizedKey = normalizeSpaceKey(key);
  const nameError = nameTouched && name.trim().length === 0;
  const keyReserved = RESERVED_SPACE_KEYS.has(normalizedKey);
  const keyError = keyTouched && key.length > 0 && !isValidSpaceKey(normalizedKey);
  const canSubmit = name.trim().length > 0 && isValidSpaceKey(normalizedKey) && !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setNameTouched(true);
    setKeyTouched(true);
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      const space = await createSpace.mutateAsync({
        name: name.trim(),
        key: normalizedKey,
        description: description.trim() || undefined,
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
          className="mx-auto flex w-full max-w-4xl flex-col gap-8 px-6 py-8"
        >
          <div className="flex flex-col gap-3">
            <div className="flex items-center gap-3">
              {/* Icon is emoji-only — picked, never typed. */}
              <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
                <PopoverTrigger
                  render={
                    <button
                      type="button"
                      aria-label={t(($) => $.form.icon)}
                      className="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-lg bg-muted/60 text-2xl transition-colors hover:bg-accent"
                    />
                  }
                >
                  {icon || <SpaceIcon space={{ icon: null }} className="size-5" />}
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
              <div className="min-w-0 flex-1">
                <Input
                  autoFocus
                  aria-label={t(($) => $.form.name)}
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  onBlur={() => setNameTouched(true)}
                  placeholder={t(($) => $.form.name_placeholder)}
                  className="h-auto rounded-none border-0 bg-transparent px-0 py-1 !text-2xl font-bold leading-snug tracking-tight shadow-none focus-visible:ring-0 dark:bg-transparent"
                />
              </div>
            </div>
            {/* pl-[52px] = icon (size-10) + row gap-3, so this lines up under the name input, not the icon.
                Always mounted (visibility toggled, not conditionally rendered) so the row below doesn't jump. */}
            <p
              className={cn(
                "pl-[52px] text-xs text-destructive",
                nameError ? "visible" : "invisible",
              )}
            >
              {t(($) => $.dialog.name_required)}
            </p>
            <PlainTextField
              defaultValue={description}
              placeholder={t(($) => $.form.description_placeholder)}
              aria-label={t(($) => $.form.description)}
              className="text-sm text-muted-foreground"
              limitHint={(count, max) => t(($) => $.form.description_limit, { count, max })}
              onCommit={setDescription}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="space-key" className="text-xs font-medium text-muted-foreground">
              {t(($) => $.form.key)}
            </Label>
            <Input
              id="space-key"
              value={key}
              onChange={(event) => setKey(normalizeSpaceKey(event.target.value))}
              onBlur={() => setKeyTouched(true)}
              placeholder="ENG"
              maxLength={7}
              className={cn(underline, "max-w-40 font-mono", keyError && "border-destructive")}
            />
            <p className={cn("text-xs", keyError ? "text-destructive" : "text-muted-foreground")}>
              {keyError && keyReserved ? t(($) => $.form.key_reserved) : t(($) => $.form.key_hint)}
            </p>
          </div>

          <MembersField memberIds={memberIds} onChange={setMemberIds} />

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
      <h3 className="text-xs font-medium text-muted-foreground">
        {t(($) => $.dialog.members_entry)}
      </h3>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <button
              type="button"
              className="-mx-1.5 flex items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-accent/60"
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
                  size={22}
                />
              </span>
            )}
            {stack.map((member) => (
              <span key={member.user_id} className="rounded-full ring-2 ring-background">
                <ActorAvatar
                  name={member.name}
                  initials={(member.name || member.email || "?").charAt(0).toUpperCase()}
                  avatarUrl={member.avatar_url}
                  size={22}
                />
              </span>
            ))}
            {overflow > 0 && (
              <span className="flex size-[22px] items-center justify-center rounded-full bg-muted text-[10px] font-medium text-muted-foreground ring-2 ring-background">
                +{overflow}
              </span>
            )}
          </span>
          <span className="text-xs text-muted-foreground">
            {t(($) => $.dialog.member_count, { count: memberIds.length + 1 })}
          </span>
        </PopoverTrigger>
        <PopoverContent align="start" className="flex w-80 flex-col gap-2 p-3">
          <div className="relative shrink-0">
            <Search className="absolute left-0 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              autoFocus
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t(($) => $.dialog.member_search)}
              className={cn(underline, "pl-6")}
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
                  size={24}
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
                  size={24}
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
