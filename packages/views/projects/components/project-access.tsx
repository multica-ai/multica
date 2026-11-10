"use client";

import { useId, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Globe, Lock, Shield, UserPlus, X } from "@multica/ui/icons";
import type { MemberWithUser, Project, ProjectVisibility } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { projectMembersOptions } from "@multica/core/projects/queries";
import { canManageProjectAccess, useProjectAccessMutation } from "@multica/core/projects/access";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@multica/ui/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Command, CommandEmpty, CommandInput, CommandItem, CommandList } from "@multica/ui/components/ui/command";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@multica/ui/components/ui/alert-dialog";
import { ActorAvatar } from "../../common/actor-avatar";
import { PillButton } from "../../common/pill-button";
import { useT } from "../../i18n";

const EMPTY_IDS: string[] = [];

function AccessFields({ visibility, onVisibilityChange, members, selected, leadId, userId, disabled, canChangeScope, canUsePrivate, canAdd, canRemove, onAdd, onRemove }: {
  visibility: ProjectVisibility;
  onVisibilityChange: (value: ProjectVisibility) => void;
  members: MemberWithUser[];
  selected: string[];
  leadId?: string | null;
  userId?: string;
  disabled: boolean;
  canChangeScope: boolean;
  canUsePrivate: boolean;
  canAdd: boolean;
  canRemove: (id: string) => boolean;
  onAdd: (id: string) => Promise<boolean | void> | void;
  onRemove: (id: string) => Promise<void> | void;
}) {
  const { t } = useT("projects");
  const groupId = useId();
  const [adding, setAdding] = useState(false);
  const recipients = members.filter((member) => selected.includes(member.user_id));
  return (
    <div className="space-y-5">
      <fieldset disabled={disabled} className="divide-y rounded-lg border">
        <legend className="sr-only">{t(($) => $.access.label)}</legend>
        {(["workspace", "private"] as const).map((value) => {
          const Icon = value === "private" ? Lock : Globe;
          return (
            <label key={value} className={`flex cursor-pointer items-center gap-3 px-4 py-3 has-disabled:cursor-default has-disabled:opacity-60 ${visibility === value ? "bg-muted/60" : "hover:bg-surface-hover"}`}>
              <input type="radio" name={groupId} value={value} checked={visibility === value}
                disabled={!canChangeScope || (value === "private" && !canUsePrivate)}
                onChange={() => onVisibilityChange(value)} className="size-4 accent-foreground" />
              <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              <span className="text-body font-medium">{value === "private" ? t(($) => $.access.private) : t(($) => $.access.workspace)}</span>
            </label>
          );
        })}
      </fieldset>
      {visibility === "private" ? (
        <div className="space-y-3">
          <p className="text-caption font-medium">{t(($) => $.access.members_count, { count: recipients.length })}</p>
          {canAdd ? (
            <Popover open={adding} onOpenChange={setAdding}>
              <PopoverTrigger render={<Button type="button" variant="outline" disabled={disabled} className="w-full justify-start text-muted-foreground"><UserPlus />{t(($) => $.access.add)}</Button>} />
              <PopoverContent align="start" className="w-80 p-0">
                <Command>
                  <CommandInput placeholder={t(($) => $.access.search)} aria-label={t(($) => $.access.search)} />
                  <CommandList>
                    <CommandEmpty>{t(($) => $.access.no_members)}</CommandEmpty>
                    {members.filter((member) => !selected.includes(member.user_id)).map((member) => (
                      <CommandItem key={member.user_id} value={`${member.name} ${member.email}`} disabled={disabled}
                        onSelect={async () => { if (await onAdd(member.user_id) !== false) setAdding(false); }}>
                        <ActorAvatar actorType="member" actorId={member.user_id} size="sm" />
                        <div className="min-w-0"><p className="truncate">{member.name}</p><p className="truncate text-caption text-muted-foreground">{member.email}</p></div>
                      </CommandItem>
                    ))}
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
          ) : null}
          <div className="max-h-64 divide-y overflow-y-auto rounded-lg border">
            {recipients.map((member) => (
              <div key={member.user_id} className="flex items-center gap-3 px-3 py-2.5">
                <ActorAvatar actorType="member" actorId={member.user_id} size="sm" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-body">{member.name}{member.user_id === userId ? <span className="ml-1.5 text-caption text-muted-foreground">{t(($) => $.access.you)}</span> : null}</p>
                  <p className="truncate text-caption text-muted-foreground">{member.email}</p>
                </div>
                {member.user_id === leadId ? <span className="shrink-0 text-caption text-muted-foreground">{t(($) => $.table.lead)}</span> : member.role === "owner" || member.role === "admin" ? (
                  <span className="shrink-0 text-caption text-muted-foreground">{member.role}</span>
                ) : canRemove(member.user_id) ? (
                  <Button type="button" variant="ghost" size="icon-xs" disabled={disabled}
                    aria-label={t(($) => $.access.remove, { name: member.name })}
                    onClick={() => void onRemove(member.user_id)}><X /></Button>
                ) : null}
              </div>
            ))}
            {recipients.length === 0 ? <p className="p-3 text-caption text-muted-foreground">{t(($) => $.access.no_selected)}</p> : null}
          </div>
        </div>
      ) : null}
      <p className="flex items-start gap-2 text-caption text-muted-foreground"><Shield className="mt-0.5 size-4 shrink-0" aria-hidden="true" />{t(($) => $.access.admin_notice)}</p>
    </div>
  );
}

export function ProjectAccessControl({ project }: { project: Project }) {
  const { t } = useT("projects");
  const [open, setOpen] = useState(false);
  const wsId = useWorkspaceId();
  const changeAccess = useProjectAccessMutation(wsId, project.id);
  const Icon = project.visibility === "private" ? Lock : Globe;
  return (
    <Dialog open={open} onOpenChange={(value) => { if (!changeAccess.isPending) setOpen(value); }}>
      <DialogTrigger render={<Button type="button" variant="ghost" size="sm" className="h-auto min-h-7 max-w-full justify-start px-0" aria-label={t(($) => $.access.label)}><Icon /><span className="truncate">{project.visibility === "private" ? t(($) => $.access.private) : t(($) => $.access.workspace)}</span></Button>} />
      <DialogContent className="flex max-h-[90dvh] flex-col sm:max-w-lg">
        <DialogHeader><DialogTitle>{t(($) => $.access.title, { title: project.title })}</DialogTitle><DialogDescription>{t(($) => $.access.description)}</DialogDescription></DialogHeader>
        {open ? <ProjectAccessEditor project={project} onClose={() => setOpen(false)} changeAccess={changeAccess} /> : null}
      </DialogContent>
    </Dialog>
  );
}

function ProjectAccessEditor({ project, onClose, changeAccess }: { project: Project; onClose: () => void; changeAccess: ReturnType<typeof useProjectAccessMutation> }) {
  const { t } = useT("projects");
  const { t: tc } = useT("common");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const workspaceMembers = useQuery(memberListOptions(wsId));
  const projectMembers = useQuery(projectMembersOptions(wsId, project.id));
  const [visibility, setVisibility] = useState<ProjectVisibility>(project.visibility ?? "workspace");
  const [draftIds, setDraftIds] = useState<string[] | null>(null);
  const [error, setError] = useState("");
  const [confirmPublic, setConfirmPublic] = useState(false);
  const members = workspaceMembers.data ?? [];
  const role = members.find((member) => member.user_id === userId)?.role;
  const leadId = project.lead_type === "member" ? project.lead_id : null;
  const canManage = canManageProjectAccess(project, userId, role);
  const currentIds = projectMembers.data?.members.map((member) => member.user_id) ?? EMPTY_IDS;
  const selected = project.visibility === "private" ? currentIds : (draftIds ?? Array.from(new Set([...currentIds, ...(userId ? [userId] : []), ...(leadId ? [leadId] : [])])));
  const loading = workspaceMembers.isPending || projectMembers.isPending;
  const loadFailed = workspaceMembers.isError || projectMembers.isError;
  const dirty = visibility !== (project.visibility ?? "workspace");
  const pending = changeAccess.isPending;
  const run = async (change: Parameters<typeof changeAccess.mutateAsync>[0]) => {
    setError("");
    try { await changeAccess.mutateAsync(change); return true; }
    catch (err) { setError(err instanceof Error ? err.message : t(($) => $.access.failed)); return false; }
  };
  const saveScope = async () => {
    if (await run({ type: "visibility", visibility, memberIds: selected })) { setConfirmPublic(false); onClose(); }
  };
  return (
    <>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {loading ? <div role="status" className="flex justify-center py-8"><Spinner /></div> : loadFailed ? (
          <div role="alert" className="space-y-2 text-body"><p>{t(($) => $.access.load_failed)}</p><Button variant="outline" size="sm" onClick={() => { void workspaceMembers.refetch(); void projectMembers.refetch(); }}>{t(($) => $.access.retry)}</Button></div>
        ) : (
          <AccessFields visibility={visibility} onVisibilityChange={setVisibility} members={members} selected={selected} userId={userId} leadId={leadId}
            disabled={pending} canChangeScope={canManage} canUsePrivate
            canAdd={canManage}
            canRemove={(id) => canManage && id !== leadId}
            onAdd={async (id) => { if (project.visibility === "private") return run({ type: "add", userId: id }); setDraftIds([...selected, id]); return true; }}
            onRemove={async (id) => {
              if (project.visibility !== "private") { setDraftIds(selected.filter((value) => value !== id)); return; }
              await run({ type: "remove", userId: id });
            }} />
        )}
        {error ? <p role="alert" className="mt-3 text-body text-destructive">{error}</p> : null}
      </div>
      <DialogFooter>
        <Button type="button" disabled={pending || (dirty && (loading || loadFailed))} aria-busy={pending} onClick={() => {
          if (!dirty) onClose();
          else if (visibility === "workspace") setConfirmPublic(true);
          else void saveScope();
        }}>{pending ? <Spinner /> : null}{dirty ? tc(($) => $.save) : t(($) => $.access.done)}</Button>
      </DialogFooter>
      <AlertDialog open={confirmPublic} onOpenChange={setConfirmPublic}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle>{t(($) => $.access.publish_title)}</AlertDialogTitle><AlertDialogDescription>{t(($) => $.access.publish_description)}</AlertDialogDescription></AlertDialogHeader>
          {error ? <p role="alert" className="text-body text-destructive">{error}</p> : null}
          <AlertDialogFooter><AlertDialogCancel disabled={pending}>{tc(($) => $.cancel)}</AlertDialogCancel><AlertDialogAction disabled={pending} aria-busy={pending} onClick={async (event) => { event.preventDefault(); await saveScope(); }}>{tc(($) => $.confirm)}</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

export function ProjectAccessDraftPicker({ visibility, memberIds, leadId, members, onChange }: {
  visibility: ProjectVisibility;
  memberIds: string[];
  leadId?: string;
  members: MemberWithUser[];
  onChange: (visibility: ProjectVisibility, ids: string[]) => void;
}) {
  const { t } = useT("projects");
  const userId = useAuthStore((s) => s.user?.id);
  const fixed = [userId, leadId].filter((id): id is string => !!id);
  const selected = Array.from(new Set([...memberIds, ...fixed]));
  const Icon = visibility === "private" ? Lock : Globe;
  return (
    <Popover>
      <PopoverTrigger render={
        <PillButton aria-label={t(($) => $.access.label)}>
          <Icon className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{visibility === "private" ? t(($) => $.access.private) : t(($) => $.access.workspace)}</span>
        </PillButton>
      } />
      <PopoverContent side="top" align="end" className="max-h-[70dvh] w-96 max-w-[calc(100vw-2rem)] overflow-y-auto">
        <AccessFields visibility={visibility} onVisibilityChange={(value) => onChange(value, selected)} members={members} selected={selected} leadId={leadId} userId={userId}
          disabled={false} canChangeScope canUsePrivate canAdd
          canRemove={(id) => !fixed.includes(id)}
          onAdd={(id) => onChange(visibility, [...selected, id])}
          onRemove={(id) => onChange(visibility, selected.filter((value) => value !== id))} />
      </PopoverContent>
    </Popover>
  );
}
