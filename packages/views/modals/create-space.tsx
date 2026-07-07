"use client";

import { useMemo, useState, type FormEvent } from "react";
import { ChevronLeft, ChevronRight, Search, Users } from "lucide-react";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { SPACE_KEY_REGEX, normalizeSpaceKey } from "@multica/core/workspace";
import { useCreateSpace } from "@multica/core/spaces/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";
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
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useT } from "../i18n";

// Linear-style underline fields: no box, just a bottom rule that darkens on
// focus. Validation reads inline under each field, never in a global slot.
const underline =
  "rounded-none border-0 border-b border-input bg-transparent dark:bg-transparent px-0 shadow-none focus-visible:ring-0 focus-visible:border-foreground";

/**
 * Create-space modal (registry: "create-space"). Single-column underline form
 * (name+icon, identifier) with a drill-in member-pick view — tapping
 * the Members row swaps the whole body, a back button returns. Fixed height
 * so the dialog never resizes between views.
 */
export function CreateSpaceModal({ onClose }: { onClose: () => void }) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const currentUser = useAuthStore((s) => s.user);
  const createSpace = useCreateSpace();

  const [view, setView] = useState<"main" | "members">("main");
  const [name, setName] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [key, setKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [icon, setIcon] = useState("");
  const [iconPickerOpen, setIconPickerOpen] = useState(false);
  const [memberIds, setMemberIds] = useState<string[]>([]);
  const [memberSearch, setMemberSearch] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const { data: allMembers = [] } = useQuery(memberListOptions(wsId));
  const invitableMembers = useMemo(() => {
    const q = memberSearch.trim().toLowerCase();
    return allMembers
      .filter((member) => member.user_id !== currentUser?.id)
      .filter(
        (member) =>
          !q ||
          member.name.toLowerCase().includes(q) ||
          member.email.toLowerCase().includes(q),
      );
  }, [allMembers, currentUser?.id, memberSearch]);

  const normalizedKey = normalizeSpaceKey(key);
  const nameError = nameTouched && name.trim().length === 0;
  const keyError = keyTouched && !SPACE_KEY_REGEX.test(normalizedKey);
  const canSubmit =
    name.trim().length > 0 && SPACE_KEY_REGEX.test(normalizedKey) && !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setNameTouched(true);
    setKeyTouched(true);
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await createSpace.mutateAsync({
        name: name.trim(),
        key: normalizedKey,
        icon: icon.trim() || null,
        member_ids: memberIds,
      });
      toast.success(t(($) => $.toast_created));
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.toast_save_failed),
      );
    } finally {
      setSubmitting(false);
    }
  };

  const toggleMember = (userId: string) =>
    setMemberIds((prev) =>
      prev.includes(userId) ? prev.filter((id) => id !== userId) : [...prev, userId],
    );

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent showCloseButton={false} className="sm:max-w-lg !h-[32rem] p-0 gap-0 flex flex-col overflow-hidden">
        <form onSubmit={submit} autoComplete="off" className="flex min-h-0 flex-1 flex-col gap-4 px-5 pt-4 pb-4">
          {view === "main" ? (
            <>
              <div className="space-y-1.5">
                <DialogTitle>{t(($) => $.dialog.create_title)}</DialogTitle>
                <DialogDescription>{t(($) => $.dialog.description)}</DialogDescription>
              </div>

              <div className="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto">
                <div className="space-y-1.5">
                  <Label htmlFor="space-name">{t(($) => $.form.icon_name)}</Label>
                  <div className="flex items-start gap-3">
                    {/* Icon is emoji-only — picked, never typed. */}
                    <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
                      <PopoverTrigger
                        render={
                          <button
                            type="button"
                            aria-label={t(($) => $.form.icon)}
                            className="flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-lg text-xl transition-colors hover:bg-accent/60"
                          />
                        }
                      >
                        {icon || <Users className="size-4 text-muted-foreground" />}
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
                    <div className="min-w-0 flex-1 space-y-1.5">
                      <Input
                        id="space-name"
                        autoFocus
                        value={name}
                        onChange={(event) => setName(event.target.value)}
                        onBlur={() => setNameTouched(true)}
                        placeholder={t(($) => $.form.name_placeholder)}
                        className={cn(underline, nameError && "border-destructive")}
                      />
                      {nameError && (
                        <p className="text-xs text-destructive">
                          {t(($) => $.dialog.name_required)}
                        </p>
                      )}
                    </div>
                  </div>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="space-key">{t(($) => $.form.key)}</Label>
                  <Input
                    id="space-key"
                    value={key}
                    onChange={(event) => setKey(normalizeSpaceKey(event.target.value))}
                    onBlur={() => setKeyTouched(true)}
                    placeholder="ENG"
                    maxLength={7}
                    className={cn(underline, "font-mono", keyError && "border-destructive")}
                  />
                  <p className={cn("text-xs", keyError ? "text-destructive" : "text-muted-foreground")}>
                    {t(($) => $.form.key_hint)}
                  </p>
                </div>


                {/* Members — drill-in entry: swaps the whole body. */}
                <button
                  type="button"
                  onClick={() => setView("members")}
                  className="flex items-center gap-2 border-b border-input pb-2 text-left transition-colors hover:border-foreground"
                >
                  <Users className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="text-sm">{t(($) => $.dialog.members_entry)}</span>
                  <span className="ml-auto text-xs text-muted-foreground">
                    {t(($) => $.dialog.member_count, { count: memberIds.length + 1 })}
                  </span>
                  <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                </button>
              </div>
            </>
          ) : (
            <>
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() => setView("main")}
                  aria-label={t(($) => $.dialog.back)}
                  className="flex size-6 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <ChevronLeft className="h-4 w-4" />
                </button>
                <DialogTitle className="text-base">
                  {t(($) => $.dialog.members_pick_title)}
                </DialogTitle>
                <span className="ml-auto text-xs text-muted-foreground">
                  {t(($) => $.dialog.member_count, { count: memberIds.length + 1 })}
                </span>
              </div>

              <div className="relative shrink-0">
                <Search className="absolute left-0 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  autoFocus
                  value={memberSearch}
                  onChange={(event) => setMemberSearch(event.target.value)}
                  placeholder={t(($) => $.dialog.member_search)}
                  className={cn(underline, "pl-6")}
                />
              </div>

              <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden">
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
                    <span className="hidden truncate text-xs text-muted-foreground sm:block">
                      {member.email}
                    </span>
                    <Checkbox
                      checked={memberIds.includes(member.user_id)}
                      onCheckedChange={() => toggleMember(member.user_id)}
                    />
                  </label>
                ))}
                {invitableMembers.length === 0 && (
                  <div className="px-1 py-4 text-center text-sm text-muted-foreground">
                    {t(($) => $.dialog.member_search_empty)}
                  </div>
                )}
              </div>
            </>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t(($) => $.actions.cancel)}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting ? t(($) => $.actions.saving) : t(($) => $.actions.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
