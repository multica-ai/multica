"use client";

import { useQuery } from "@tanstack/react-query";
import { Eye } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useInboxUnreadCount } from "@multica/core/inbox/queries";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { PanelBody, PanelCount, PanelHeader, PanelLink, PanelList, PanelSection, isPathWithin } from "../panel-nav";
import { routeIconForPath } from "../route-icon-components";

const EMPTY_PROJECTS: Project[] = [];

/**
 * The Work panel for a guest (MUL-9420): its Inbox, the issues it takes part
 * in, and the projects shared with it. Everything else in the workspace is
 * out of a guest's reach, so nothing else is listed.
 */
export function GuestWorkPanel() {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id;
  const unread = useInboxUnreadCount(wsId);
  const { data: projects = EMPTY_PROJECTS } = useQuery({
    ...projectListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const shared = projects.filter((project) => !project.archived_at);
  const InboxIcon = routeIconForPath(p.inbox());
  const InvolvedIcon = routeIconForPath(p.myIssues());

  return (
    <>
      <PanelHeader title={workspace?.name ?? t(($) => $.spaces.work)}>
        <TooltipHint label={t(($) => $.panels.work.guest_badge_hint)} side="bottom">
          <Badge variant="secondary" className="shrink-0 gap-1">
            <Eye className="size-3" aria-hidden="true" />
            {t(($) => $.panels.work.guest_badge)}
          </Badge>
        </TooltipHint>
      </PanelHeader>
      <PanelBody>
        <PanelList>
          <PanelLink
            href={p.inbox()}
            icon={<InboxIcon />}
            label={t(($) => $.nav.inbox)}
            active={pathname === p.inbox()}
            trailing={<PanelCount value={unread} />}
          />
          <PanelLink
            href={p.myIssues()}
            icon={<InvolvedIcon />}
            label={t(($) => $.panels.work.involved)}
            active={isPathWithin(pathname, p.myIssues())}
          />
        </PanelList>
        <PanelSection label={t(($) => $.panels.work.shared_projects)}>
          <PanelList>
            {shared.map((project) => (
              <PanelLink
                key={project.id}
                href={p.projectDetail(project.id)}
                icon={<ProjectIcon project={project} size="md" />}
                label={project.title}
                active={isPathWithin(pathname, p.projectDetail(project.id))}
                trailing={<PanelCount value={project.issue_count - project.done_count} />}
              />
            ))}
          </PanelList>
        </PanelSection>
      </PanelBody>
    </>
  );
}
