"use client";

import { Fragment } from "react";
import { useQuery } from "@tanstack/react-query";
import type { WorkspaceLink } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths, useCurrentWorkspace } from "@multica/core/paths";
import { useCurrentMember } from "@multica/core/permissions";
import { expandLinkUrl, validateLinkUrl, linkListOptions } from "@multica/core/links";
import { Home, MoreHorizontal } from "@multica/ui/icons";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { AppLink } from "../../navigation";
import { openExternal, isDesktopShell } from "../../platform";
import { useT } from "../../i18n";
import { LinkIcon } from "./link-icon";
import { LinkEditor } from "./link-editor";

export function LinksBar({ projectId }: { projectId?: string }) {
  const wsId = useWorkspaceId();
  const ws = useCurrentWorkspace();
  const paths = useWorkspacePaths();
  const { t } = useT("settings");
  const { role } = useCurrentMember(wsId);
  const { data: workspaceLinks = [] } = useQuery(linkListOptions(wsId));
  const { data: projectLinks = [] } = useQuery({
    ...linkListOptions(wsId, projectId),
    enabled: !!projectId,
  });

  if (!ws) return null;

  const links = [...(projectId ? projectLinks : []), ...workspaceLinks].filter(
    (link) => !validateLinkUrl(link.urlTemplate, !!link.projectId),
  );
  if (!links.length) {
    return role === "owner" || role === "admin" ? <LinkEditor projectId={projectId} compact /> : null;
  }

  const href = (link: WorkspaceLink) =>
    paths.link(link.id) + (link.projectId ? `?project=${encodeURIComponent(link.projectId)}` : "");
  const external = (link: WorkspaceLink) =>
    expandLinkUrl(link.urlTemplate, {
      workspaceSlug: ws.slug,
      workspaceId: ws.id,
      projectId: link.projectId ?? undefined,
    });
  const label = (link: WorkspaceLink, compact = false) => (
    <>
      <LinkIcon icon={link.icon} />
      {(!compact || link.showName) && (
        <span className={compact ? "hidden max-w-28 truncate xl:inline" : "truncate"}>
          {link.name}
        </span>
      )}
      {link.isHome && (
        <TooltipHint label={t(($) => $.links.home_page)}>
          <span aria-label={t(($) => $.links.home_page)}>
            <Home className="size-3 text-muted-foreground" />
          </span>
        </TooltipHint>
      )}
    </>
  );
  const item = (link: WorkspaceLink, compact: boolean) =>
    link.openMode === "embed" ? (
      <AppLink
        href={href(link)}
        title={link.name}
        aria-label={link.name}
        className={buttonVariants({ variant: "ghost", size: link.showName ? "sm" : "icon-sm" })}
      >
        {label(link, compact)}
      </AppLink>
    ) : (
      <a
        href={external(link)}
        target="_blank"
        rel="noopener noreferrer"
        title={link.name}
        aria-label={link.name}
        className={buttonVariants({ variant: "ghost", size: link.showName ? "sm" : "icon-sm" })}
        onClick={(e) => {
          if (isDesktopShell()) {
            e.preventDefault();
            openExternal(external(link));
          }
        }}
      >
        {label(link, compact)}
      </a>
    );

  return (
    <nav aria-label={t(($) => $.links.title)} className="flex min-w-0 items-center gap-1">
      <div className="hidden items-center gap-1 sm:flex">
        {links.slice(0, 3).map((link, index) => (
          <div key={link.id} className="flex items-center gap-1">
            {index > 0 && links[index - 1]?.projectId && !link.projectId && <span className="h-4 border-l" />}
            {item(link, true)}
          </div>
        ))}
      </div>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.links.title)}
              className={links.length <= 3 ? "sm:hidden" : ""}
            />
          }
        >
          <MoreHorizontal />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {links.map((link, index) => (
            <Fragment key={link.id}>
              {index > 0 && links[index - 1]?.projectId && !link.projectId && <DropdownMenuSeparator />}
              <DropdownMenuItem
                render={
                  link.openMode === "embed" ? (
                    <AppLink href={href(link)} />
                  ) : (
                    <a
                      href={external(link)}
                      target="_blank"
                      rel="noopener noreferrer"
                      onClick={(e) => {
                        if (isDesktopShell()) {
                          e.preventDefault();
                          openExternal(external(link));
                        }
                      }}
                    />
                  )
                }
              >
                {label(link)}
              </DropdownMenuItem>
            </Fragment>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </nav>
  );
}
