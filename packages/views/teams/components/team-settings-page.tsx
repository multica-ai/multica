"use client";

import { useEffect, useMemo, useState } from "react";
import { FolderKanban, ListTodo, Search, Zap } from "lucide-react";
import { EmojiPicker } from "@multica/ui/components/common/emoji-picker";
import { PlainTextField } from "@multica/ui/components/common/plain-text-field";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { teamListOptions, teamMembersOptions } from "@multica/core/teams/queries";
import {
  useArchiveTeam,
  useReplaceTeamMembers,
  useUpdateTeam,
} from "@multica/core/teams/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Team } from "@multica/core/types";
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { TeamIcon } from "./team-icon";
import { useT } from "../../i18n";

// Underline style for the member search field inside the config popover.
const underline =
  "rounded-none border-0 border-b border-input bg-transparent dark:bg-transparent px-0 shadow-none focus-visible:ring-0 focus-visible:border-foreground";

/**
 * Team settings — /team/:key/settings, Linear team-page shape. Left column is
 * the identity itself rendered as page text (icon picker applies on pick,
 * name and description commit on blur — no save buttons); the right column
 * holds members (avatar stack → checkbox config; saving an empty set
 * archives behind a confirm), go-to links, and archive.
 */
export function TeamSettingsPage({ teamKey }: { teamKey: string }) {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  // Full list (not active-only): an archived team's settings stay viewable.
  const { data: teams = [], isSuccess } = useQuery(teamListOptions(wsId));
  const team = teams.find((tm) => tm.key.toLowerCase() === teamKey.toLowerCase());

  if (!team) {
    return isSuccess ? (
      <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.surface.not_found)}
      </div>
    ) : null;
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-2">
        <TeamIcon team={team} />
        <h1 className="text-sm font-medium">{team.name}</h1>
        {team.is_default && <Badge variant="secondary">{t(($) => $.state.default)}</Badge>}
        {team.archived_at && <Badge variant="outline">{t(($) => $.state.archived)}</Badge>}
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-3xl gap-10 px-6 py-8">
          <div className="min-w-0 flex-1">
            <Identity team={team} />
          </div>
          <aside className="flex w-52 shrink-0 flex-col gap-6">
            <MembersSection team={team} />
            <GotoSection team={team} />
            <ArchiveSection team={team} />
          </aside>
        </div>
      </div>
    </div>
  );
}

/**
 * Icon + name + description rendered as page content. The icon applies on
 * pick; name and description commit on blur (Escape restores) — there is no
 * save button anywhere on this page.
 */
function Identity({ team }: { team: Team }) {
  const { t } = useT("teams");
  const updateTeam = useUpdateTeam();
  const [name, setName] = useState(team.name);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);

  // Re-seed when navigating between teams (or after a save round-trips).
  useEffect(() => {
    setName(team.name);
  }, [team.id, team.name]);

  const saveField = async (patch: { name?: string; icon?: string | null; description?: string }) => {
    try {
      await updateTeam.mutateAsync({ id: team.id, ...patch });
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
      setName(team.name);
      return;
    }
    if (next !== team.name) void saveField({ name: next });
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
                className="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-lg bg-muted/60 text-2xl transition-colors hover:bg-accent"
              />
            }
          >
            {team.icon || <TeamIcon team={{ icon: null }} className="size-5" />}
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
          onChange={(event) => setName(event.target.value)}
          onBlur={commitName}
          onKeyDown={(event) => {
            if (event.key === "Enter") event.currentTarget.blur();
            if (event.key === "Escape") {
              setName(team.name);
              event.currentTarget.blur();
            }
          }}
          className="h-auto rounded-none border-0 bg-transparent px-0 py-1 !text-2xl font-bold leading-snug tracking-tight shadow-none focus-visible:ring-0 dark:bg-transparent"
        />
      </div>
      {/* Keyed by the server value so a committed save (or a WS update)
          re-seeds the field without fighting in-progress typing. */}
      <PlainTextField
        key={`${team.id}:${team.description}`}
        defaultValue={team.description}
        placeholder={t(($) => $.form.description_placeholder)}
        aria-label={t(($) => $.form.description)}
        className="text-sm text-muted-foreground"
        limitHint={(count, max) => t(($) => $.form.description_limit, { count, max })}
        onCommit={(value) => void saveField({ description: value })}
      />
    </div>
  );
}

/**
 * Member stack + config popover. The checkbox set is the full source of
 * truth; saving replaces the team's membership wholesale. Deselecting
 * everyone means the team has no reason to exist — saving then archives it,
 * behind a confirm (blocked for the default team).
 */
function MembersSection({ team }: { team: Team }) {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(teamMembersOptions(wsId, team.id));
  const { data: allMembers = [] } = useQuery(memberListOptions(wsId));
  const replaceMembers = useReplaceTeamMembers();
  const archiveTeam = useArchiveTeam();

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

  const toggle = (userId: string) =>
    setSelected((prev) =>
      prev.includes(userId) ? prev.filter((id) => id !== userId) : [...prev, userId],
    );

  const doArchive = async () => {
    setConfirmArchive(false);
    setOpen(false);
    try {
      await archiveTeam.mutateAsync(team.id);
      toast.success(t(($) => $.toast_archived));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message ? err.message : t(($) => $.toast_archive_failed),
      );
    }
  };

  const save = async () => {
    if (selected.length === 0) {
      if (team.is_default) {
        toast.error(t(($) => $.settings.default_cannot_archive));
        return;
      }
      setConfirmArchive(true);
      return;
    }
    setSaving(true);
    try {
      await replaceMembers.mutateAsync({ id: team.id, member_ids: selected });
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
            <button
              type="button"
              className="-mx-1.5 flex items-center gap-2 rounded-md px-1.5 py-1 text-left transition-colors hover:bg-accent/60"
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
          ) : (
            <span className="text-sm text-muted-foreground">
              {t(($) => $.picker.empty)}
            </span>
          )}
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
            {filteredMembers.map((member) => (
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
                  checked={selected.includes(member.user_id)}
                  onCheckedChange={() => toggle(member.user_id)}
                />
              </label>
            ))}
            {filteredMembers.length === 0 && (
              <div className="px-1 py-4 text-center text-sm text-muted-foreground">
                {t(($) => $.picker.empty)}
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
            {t(($) => $.settings.archive_confirm_body, { name: team.name })}
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

function GotoSection({ team }: { team: Team }) {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  // Counts come from the shared list caches — cheap, and clicking through
  // lands on the matching team surface.
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: autopilots = [] } = useQuery(autopilotListOptions(wsId));
  const projectCount = useMemo(
    () => projects.filter((project) => project.team_ids?.includes(team.id)).length,
    [projects, team.id],
  );
  const autopilotCount = useMemo(
    () => autopilots.filter((autopilot) => autopilot.team_id === team.id).length,
    [autopilots, team.id],
  );

  const links = [
    { icon: ListTodo, label: t(($) => $.settings.stats_issues), value: team.issue_counter, href: p.teamIssues(team.key) },
    { icon: FolderKanban, label: t(($) => $.settings.stats_projects), value: projectCount, href: p.teamProjects(team.key) },
    { icon: Zap, label: t(($) => $.settings.stats_autopilots), value: autopilotCount, href: p.teamAutopilots(team.key) },
  ];

  return (
    <div className="flex flex-col gap-1">
      <h3 className="mb-1 text-xs font-medium text-muted-foreground">
        {t(($) => $.settings.goto)}
      </h3>
      {links.map((link) => (
        <AppLink
          key={link.href}
          href={link.href}
          className="-mx-1.5 flex items-center gap-2 rounded-md px-1.5 py-1 text-sm transition-colors hover:bg-accent/60"
        >
          <link.icon className="h-4 w-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0 flex-1 truncate">{link.label}</span>
          <span className="text-xs tabular-nums text-muted-foreground">{link.value}</span>
        </AppLink>
      ))}
    </div>
  );
}

function ArchiveSection({ team }: { team: Team }) {
  const { t } = useT("teams");
  const archiveTeam = useArchiveTeam();
  const [confirmOpen, setConfirmOpen] = useState(false);

  if (team.is_default || team.archived_at) return null;

  const doArchive = async () => {
    setConfirmOpen(false);
    try {
      await archiveTeam.mutateAsync(team.id);
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
      <button
        type="button"
        onClick={() => setConfirmOpen(true)}
        className="-mx-1.5 flex items-center rounded-md px-1.5 py-1 text-left text-sm text-destructive transition-colors hover:bg-destructive/10"
      >
        {t(($) => $.actions.archive)}
      </button>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent showCloseButton={false} className="sm:max-w-sm">
          <DialogTitle>{t(($) => $.settings.archive_confirm_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.settings.archive_confirm_body, { name: team.name })}
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
