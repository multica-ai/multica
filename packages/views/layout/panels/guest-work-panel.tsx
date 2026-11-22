"use client";

import { useQuery } from "@tanstack/react-query";
import { Eye } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { isIssueUuid } from "@multica/core/issues/canonical-id";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { resolveClickIntent, useNavigation } from "../../navigation";
import { AutoArchivePromptDialog } from "../../inbox/components/auto-archive-prompt-dialog";
import { useSidebarInboxIssue } from "../../inbox/components/use-sidebar-inbox-issue";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useT } from "../../i18n";
import { PanelBody, PanelCount, PanelHeader, PanelLink, PanelList, PanelSection, isPathWithin } from "../panel-nav";
import { routeIconForPath } from "../route-icon-components";
import { INBOX_LIST, InboxSection, useInboxSectionRows } from "./inbox-section";
import { routeSegment } from "./work-panel";

const EMPTY_PROJECTS: Project[] = [];

/**
 * The Work panel for a guest (MUL-9420): its Inbox, the issues it takes part
 * in, and the projects shared with it. Everything else in the workspace is
 * out of a guest's reach, so nothing else is listed.
 *
 * The Inbox section is the member one: on a wide screen the Inbox page shows
 * only the open notification, so its list has to live here (MUL-9855).
 */
export function GuestWorkPanel() {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname, searchParams, openInNewTab } = useNavigation();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id;
  const { data: projects = EMPTY_PROJECTS } = useQuery({
    ...projectListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  const shared = projects.filter((project) => !project.archived_at);
  const InvolvedIcon = routeIconForPath(p.myIssues());

  // Cache read only: the detail page owns resolving the route's issue.
  const issueSegment = routeSegment(pathname, p.issues());
  const { data: currentIssue } = useQuery({
    ...issueDetailOptions(wsId ?? "", issueSegment ?? ""),
    enabled: false,
  });
  const currentIssueId = issueSegment
    ? (currentIssue?.id ?? (isIssueUuid(issueSegment) ? issueSegment : null))
    : null;
  const inbox = useSidebarInboxIssue(wsId, currentIssueId);
  const inboxOpenKey =
    pathname === p.inbox()
      ? searchParams.get("issue")
      : currentIssueId && inbox.fromInbox
        ? currentIssueId
        : null;
  const inboxRows = useInboxSectionRows(wsId, inboxOpenKey);
  const inboxClaimsIssue = !!currentIssueId && inbox.fromInbox && !inboxRows.collapsed;
  const rememberSidebarIssue = useInboxSelectionStore((s) => s.rememberSidebarIssue);
  const rememberSidebarTabIssue = useInboxSelectionStore((s) => s.rememberSidebarTabIssue);
  const forgetSidebarTabIssue = useInboxSelectionStore((s) => s.forgetSidebarTabIssue);
  // An issue opened from an Inbox row belongs to the Inbox, as in the member
  // panel: its row stays lit and the issue header moves on to the next one.
  const rememberInboxIssue = (event: React.MouseEvent) => {
    // A middle click opens a tab; other buttons open nothing.
    if (event.type === "auxclick" && event.button !== 1) return;
    const link = (event.target as HTMLElement).closest("a[href]");
    const segment = link?.getAttribute("data-issue-id");
    if (!link || !segment || !wsId) return;
    // An Inbox row's hover card renders in a portal, outside the row's list.
    const fromInbox =
      !!link.closest("[data-inbox-card]") || link.closest("[data-sidebar-list]")?.getAttribute("data-sidebar-list") === INBOX_LIST;
    if (!fromInbox) return;
    if (resolveClickIntent(event) !== "push" || (event.shiftKey && !openInNewTab)) {
      if (openInNewTab) rememberSidebarTabIssue(wsId, segment);
      return;
    }
    rememberSidebarIssue(wsId, segment);
    forgetSidebarTabIssue(wsId, segment);
  };

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
      <PanelBody onClickCapture={rememberInboxIssue} onAuxClickCapture={rememberInboxIssue}>
        <PanelList>
          <PanelLink
            href={p.myIssues()}
            icon={<InvolvedIcon />}
            label={t(($) => $.panels.work.involved)}
            active={isPathWithin(pathname, p.myIssues())}
          />
        </PanelList>
        {wsId && (
          <InboxSection
            wsId={wsId}
            rows={inboxRows}
            pathname={pathname}
            activeIssueId={inboxClaimsIssue ? currentIssueId : null}
          />
        )}
        {wsId && <AutoArchivePromptDialog wsId={wsId} active={!!inboxOpenKey} />}
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
