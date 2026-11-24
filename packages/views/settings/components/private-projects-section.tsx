"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
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
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { privateProjectOverviewOptions } from "@multica/core/projects/queries";
import { useJoinPrivateProject } from "@multica/core/projects/mutations";
import type { PrivateProjectOverview } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

const SHOWN_MEMBERS = 8;

/**
 * A private project is open to its member list only, so an owner who is not
 * in one cannot reach it from anywhere else in the app. This is the way back
 * in: every private project with who is in it, and a way to join. Rendered
 * for workspace owners only; the server refuses everyone else.
 */
export function PrivateProjectsSection() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data: projects, isError, refetch } = useQuery(privateProjectOverviewOptions(wsId));
  const join = useJoinPrivateProject();
  const [joining, setJoining] = useState<PrivateProjectOverview | null>(null);

  const confirmJoin = async () => {
    if (!joining) return;
    const { id, title } = joining;
    try {
      await join.mutateAsync(id);
      setJoining(null);
      toast.success(t(($) => $.private_projects.toast_joined, { title }));
    } catch (error) {
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : t(($) => $.private_projects.toast_failed),
      );
    }
  };

  const memberNames = (project: PrivateProjectOverview) => {
    if (project.members.length === 0) return t(($) => $.private_projects.no_members);
    const isLead = (member: PrivateProjectOverview["members"][number]) =>
      project.lead_type === "member" && member.user_id === project.lead_id;
    // The lead first: they are who to ask before stepping in.
    const ordered = [...project.members].sort((a, b) => Number(isLead(b)) - Number(isLead(a)));
    const names = ordered.slice(0, SHOWN_MEMBERS).map((member) =>
      isLead(member) ? t(($) => $.private_projects.lead_name, { name: member.name }) : member.name,
    );
    const rest = project.members.length - names.length;
    if (rest > 0) names.push(t(($) => $.private_projects.more_members, { count: rest }));
    return names.join(", ");
  };

  return (
    <SettingsSection
      anchor="private-projects"
      title={t(($) => $.private_projects.title)}
      description={t(($) => $.private_projects.description)}
    >
      <SettingsCard>
        {isError ? (
          <SettingsRow label={t(($) => $.private_projects.load_failed)}>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              {t(($) => $.private_projects.retry)}
            </Button>
          </SettingsRow>
        ) : !projects ? (
          <div className="min-h-16" aria-busy="true" />
        ) : projects.length === 0 ? (
          <p className="px-4 py-5 text-body text-muted-foreground">
            {t(($) => $.private_projects.empty)}
          </p>
        ) : (
          projects.map((project) => (
            <SettingsRow
              key={project.id}
              label={
                <span className="flex min-w-0 items-center gap-2">
                  <ProjectIcon project={project} size="md" />
                  <span className="truncate">{project.title}</span>
                  {project.archived_at ? (
                    <Badge variant="secondary">{t(($) => $.private_projects.archived)}</Badge>
                  ) : null}
                </span>
              }
              description={memberNames(project)}
            >
              {project.is_member ? (
                <div className="flex items-center gap-3">
                  <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
                    <Check className="size-3.5" aria-hidden="true" />
                    {t(($) => $.private_projects.member)}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    render={<AppLink href={paths.projectDetail(project.id)} />}
                    nativeButton={false}
                  >
                    {t(($) => $.private_projects.open)}
                  </Button>
                </div>
              ) : (
                <Button
                  variant="outline"
                  size="sm"
                  disabled={join.isPending}
                  onClick={() => setJoining(project)}
                >
                  {t(($) => $.private_projects.join)}
                </Button>
              )}
            </SettingsRow>
          ))
        )}
      </SettingsCard>

      <AlertDialog open={!!joining} onOpenChange={(open) => { if (!open && !join.isPending) setJoining(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.private_projects.join_title, { title: joining?.title ?? "" })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {joining && joining.members.length === 0
                ? t(($) => $.private_projects.join_description_empty)
                : t(($) => $.private_projects.join_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={join.isPending}>{t(($) => $.private_projects.cancel)}</AlertDialogCancel>
            <AlertDialogAction disabled={join.isPending} onClick={() => void confirmJoin()}>
              {join.isPending ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : null}
              {t(($) => $.private_projects.join_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  );
}
