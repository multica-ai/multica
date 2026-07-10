"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { FolderKanban, ListTodo, Settings2, Users, Zap } from "lucide-react";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import { useJoinSpace, useLeaveSpace } from "@multica/core/spaces/mutations";
import { spaceListOptions, spaceMembersOptions } from "@multica/core/spaces/queries";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { PageHeader } from "../../layout/page-header";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { SpaceIcon } from "./space-icon";
import { useWorkspacePaths } from "@multica/core/paths";

export function SpaceOverviewPage({ spaceKey }: { spaceKey: string }) {
  const { t } = useT("spaces");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: spaces = [], isSuccess } = useQuery(spaceListOptions(wsId));
  const space = spaces.find(
    (candidate) => candidate.key.toLowerCase() === spaceKey.toLowerCase(),
  );
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: autopilots = [] } = useQuery(autopilotListOptions(wsId));
  const { data: members = [] } = useQuery({
    ...spaceMembersOptions(wsId, space?.id ?? ""),
    enabled: !!space,
  });
  const joinSpace = useJoinSpace();
  const leaveSpace = useLeaveSpace();
  const [confirmLeave, setConfirmLeave] = useState(false);

  const projectCount = useMemo(
    () => projects.filter((project) => project.space_id === space?.id).length,
    [projects, space?.id],
  );
  const autopilotCount = useMemo(
    () => autopilots.filter((autopilot) => autopilot.space_id === space?.id).length,
    [autopilots, space?.id],
  );

  if (!space) {
    return isSuccess ? (
      <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.surface.not_found)}
      </div>
    ) : null;
  }

  const destinations = [
    {
      icon: ListTodo,
      label: t(($) => $.settings.stats_issues),
      value: space.issue_counter,
      href: paths.spaceIssues(space.key),
    },
    {
      icon: FolderKanban,
      label: t(($) => $.settings.stats_projects),
      value: projectCount,
      href: paths.spaceProjects(space.key),
    },
    {
      icon: Zap,
      label: t(($) => $.settings.stats_autopilots),
      value: autopilotCount,
      href: paths.spaceAutopilots(space.key),
    },
    {
      icon: Settings2,
      label: t(($) => $.settings.title),
      value: null,
      href: paths.spaceSettings(space.key),
    },
  ];

  const handleJoin = async () => {
    try {
      await joinSpace.mutateAsync(space.id);
      toast.success(t(($) => $.toast_joined));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.toast_save_failed),
      );
    }
  };

  const handleLeave = async () => {
    try {
      await leaveSpace.mutateAsync(space.id);
      setConfirmLeave(false);
      toast.success(t(($) => $.toast_left));
      if (space.visibility === "private") navigation.replace(paths.myIssues());
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.toast_save_failed),
      );
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="gap-2">
        <SpaceIcon space={space} />
        <h1 className="min-w-0 truncate text-sm font-medium">{space.name}</h1>
        {space.is_default && (
          <Badge variant="secondary">{t(($) => $.state.default)}</Badge>
        )}
        {space.archived_at && (
          <Badge variant="outline">{t(($) => $.state.archived)}</Badge>
        )}
        <div className="ml-auto">
          {!space.archived_at && !space.is_member && space.visibility === "open" && (
            <Button
              size="sm"
              variant="outline"
              disabled={joinSpace.isPending}
              onClick={() => void handleJoin()}
            >
              {t(($) => $.actions.join)}
            </Button>
          )}
          {!space.archived_at && space.is_member && (
            <Button
              size="sm"
              variant="ghost"
              disabled={leaveSpace.isPending}
              onClick={() => setConfirmLeave(true)}
            >
              {t(($) => $.actions.leave)}
            </Button>
          )}
        </div>
      </PageHeader>

      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-8 px-6 py-8">
          <section className="space-y-3">
            <h2 className="text-xs font-medium text-muted-foreground">
              {t(($) => $.overview.work_title)}
            </h2>
            <div className="grid grid-cols-[repeat(auto-fit,minmax(11rem,1fr))] gap-3">
              {destinations.map((destination) => (
                <AppLink
                  key={destination.href}
                  href={destination.href}
                  className="flex items-center gap-3 rounded-lg border border-input/60 px-4 py-3 transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <destination.icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">
                    {destination.label}
                  </span>
                  {destination.value !== null && (
                    <span className="text-xs tabular-nums text-muted-foreground">
                      {destination.value}
                    </span>
                  )}
                </AppLink>
              ))}
            </div>
          </section>

          <section className="space-y-3">
            <div className="flex items-center justify-between gap-3">
              <h2 className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                <Users className="size-4" aria-hidden />
                {t(($) => $.settings.members)}
              </h2>
              <AppLink
                href={paths.spaceSettings(space.key)}
                className="text-xs text-muted-foreground hover:text-foreground hover:underline"
              >
                {t(($) => $.overview.manage_members)}
              </AppLink>
            </div>
            {members.length > 0 ? (
              <div className="flex flex-wrap items-center gap-2">
                {members.slice(0, 12).map((member) => (
                  <ActorAvatar
                    key={member.user_id}
                    name={member.name}
                    initials={(member.name || member.email || "?").charAt(0).toUpperCase()}
                    avatarUrl={member.avatar_url}
                    size="sm"
                  />
                ))}
                <span className="text-xs text-muted-foreground">
                  {t(($) => $.overview.member_count, { count: members.length })}
                </span>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                {t(($) => $.settings.members_empty)}
              </p>
            )}
          </section>
        </div>
      </main>

      <AlertDialog open={confirmLeave} onOpenChange={setConfirmLeave}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.overview.leave_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.overview.leave_confirm_description, { name: space.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.actions.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void handleLeave()}>
              {t(($) => $.actions.leave)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
