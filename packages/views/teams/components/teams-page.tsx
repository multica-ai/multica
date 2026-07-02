"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import { Edit2, Plus, Trash2, Users } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { teamListOptions } from "@multica/core/teams/queries";
import { TEAM_KEY_REGEX, normalizeTeamKey } from "@multica/core/workspace";
import {
  useArchiveTeam,
  useCreateTeam,
  useUpdateTeam,
} from "@multica/core/teams/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Team } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";

export function TeamsPage() {
  const { t } = useT("teams");
  const wsId = useWorkspaceId();
  const { data: teams = [], isLoading } = useQuery(teamListOptions(wsId));
  const createTeam = useCreateTeam();
  const updateTeam = useUpdateTeam();
  const archiveTeam = useArchiveTeam();
  const [editingTeam, setEditingTeam] = useState<Team | null>(null);
  const [creating, setCreating] = useState(false);

  const sortedTeams = useMemo(
    () =>
      [...teams].sort((a, b) => {
        if (a.is_default !== b.is_default) return a.is_default ? -1 : 1;
        if (!!a.archived_at !== !!b.archived_at) return a.archived_at ? 1 : -1;
        return a.name.localeCompare(b.name);
      }),
    [teams],
  );

  const closeDialog = () => {
    setCreating(false);
    setEditingTeam(null);
  };

  const handleArchive = async (team: Team) => {
    try {
      await archiveTeam.mutateAsync(team.id);
      toast.success(t(($) => $.toast_archived));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.toast_archive_failed),
      );
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-2">
        <Users className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        <Button
          size="sm"
          className="ml-auto"
          onClick={() => setCreating(true)}
        >
          <Plus className="h-3.5 w-3.5" />
          {t(($) => $.page.new_team)}
        </Button>
      </PageHeader>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {isLoading ? (
          <div className="rounded-md border p-4 text-sm text-muted-foreground">
            {t(($) => $.page.loading)}
          </div>
        ) : sortedTeams.length === 0 ? (
          <div className="flex h-full min-h-72 flex-col items-center justify-center gap-2 text-muted-foreground">
            <Users className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <Button size="sm" onClick={() => setCreating(true)}>
              {t(($) => $.page.create_first)}
            </Button>
          </div>
        ) : (
          <div className="overflow-hidden rounded-md border">
            <div className="grid grid-cols-[7rem_minmax(12rem,1fr)_8rem_9rem_5rem] items-center gap-3 border-b bg-muted/40 px-3 py-2 text-xs font-medium text-muted-foreground">
              <div>{t(($) => $.table.key)}</div>
              <div>{t(($) => $.table.name)}</div>
              <div>{t(($) => $.table.issues)}</div>
              <div>{t(($) => $.table.state)}</div>
              <div className="text-right">{t(($) => $.table.actions)}</div>
            </div>
            {sortedTeams.map((team) => (
              <div
                key={team.id}
                className="grid grid-cols-[7rem_minmax(12rem,1fr)_8rem_9rem_5rem] items-center gap-3 border-b px-3 py-2 last:border-b-0"
              >
                <div>
                  <span className="inline-flex h-6 min-w-9 items-center justify-center rounded bg-muted px-2 text-xs font-medium text-muted-foreground">
                    {team.key}
                  </span>
                </div>
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">{team.name}</div>
                  {team.description && (
                    <div className="truncate text-xs text-muted-foreground">
                      {team.description}
                    </div>
                  )}
                </div>
                <div className="text-sm tabular-nums text-muted-foreground">
                  {team.issue_counter}
                </div>
                <div className="flex items-center gap-1.5">
                  {team.is_default && (
                    <Badge variant="secondary">{t(($) => $.state.default)}</Badge>
                  )}
                  {team.archived_at && (
                    <Badge variant="outline">{t(($) => $.state.archived)}</Badge>
                  )}
                  {!team.is_default && !team.archived_at && (
                    <Badge variant="outline">{t(($) => $.state.active)}</Badge>
                  )}
                </div>
                <div className="flex justify-end gap-1">
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    onClick={() => setEditingTeam(team)}
                    aria-label={t(($) => $.actions.edit)}
                  >
                    <Edit2 className="h-3.5 w-3.5" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7 text-muted-foreground hover:text-destructive"
                    disabled={team.is_default || !!team.archived_at}
                    onClick={() => handleArchive(team)}
                    aria-label={t(($) => $.actions.archive)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <TeamDialog
        team={editingTeam}
        open={creating || !!editingTeam}
        onOpenChange={(open) => {
          if (!open) closeDialog();
        }}
        onSubmit={async (payload) => {
          if (editingTeam) {
            await updateTeam.mutateAsync({ id: editingTeam.id, ...payload });
            toast.success(t(($) => $.toast_updated));
          } else {
            await createTeam.mutateAsync(payload);
            toast.success(t(($) => $.toast_created));
          }
          closeDialog();
        }}
      />
    </div>
  );
}

function TeamDialog({
  team,
  open,
  onOpenChange,
  onSubmit,
}: {
  team: Team | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (payload: {
    name: string;
    key: string;
    description?: string;
    icon?: string | null;
  }) => Promise<void>;
}) {
  const { t } = useT("teams");
  const [name, setName] = useState(team?.name ?? "");
  const [key, setKey] = useState(team?.key ?? "");
  const [description, setDescription] = useState(team?.description ?? "");
  const [icon, setIcon] = useState(team?.icon ?? "");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    setName(team?.name ?? "");
    setKey(team?.key ?? "");
    setDescription(team?.description ?? "");
    setIcon(team?.icon ?? "");
    setSubmitting(false);
  }, [team, open]);

  const lockKey = !!team && team.issue_counter > 0;
  const normalizedKey = normalizeTeamKey(key);
  const canSubmit =
    name.trim().length > 0 &&
    TEAM_KEY_REGEX.test(normalizedKey) &&
    !submitting;

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await onSubmit({
        name: name.trim(),
        key: normalizedKey,
        description: description.trim(),
        icon: icon.trim() || null,
      });
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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>
              {team ? t(($) => $.dialog.edit_title) : t(($) => $.dialog.create_title)}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.dialog.description)}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="team-name">{t(($) => $.form.name)}</Label>
            <Input
              id="team-name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t(($) => $.form.name_placeholder)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="team-key">{t(($) => $.form.key)}</Label>
            <Input
              id="team-key"
              value={key}
              onChange={(event) => setKey(normalizeTeamKey(event.target.value))}
              placeholder="ENG"
              maxLength={7}
              disabled={lockKey}
            />
            <p className="text-xs text-muted-foreground">
              {lockKey
                ? t(($) => $.form.key_locked)
                : t(($) => $.form.key_hint)}
            </p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="team-icon">{t(($) => $.form.icon)}</Label>
            <Input
              id="team-icon"
              value={icon}
              onChange={(event) => setIcon(event.target.value)}
              placeholder="⚙"
              maxLength={64}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="team-description">{t(($) => $.form.description)}</Label>
            <Textarea
              id="team-description"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder={t(($) => $.form.description_placeholder)}
              rows={3}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t(($) => $.actions.cancel)}
            </Button>
            <Button type="submit" disabled={!canSubmit}>
              {submitting ? t(($) => $.actions.saving) : t(($) => $.actions.save)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
