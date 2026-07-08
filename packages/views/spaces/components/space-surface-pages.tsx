"use client";

import { ListTodo } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { activeSpaceListOptions } from "@multica/core/spaces/queries";
import type { Space } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { IssuesSurfaceHeader } from "../../issues/components/issues-page";
import { ProjectsPage } from "../../projects/components/projects-page";
import { AutopilotsPage } from "../../autopilots/components/autopilots-page";
import { SpaceIcon } from "./space-icon";
import { useT } from "../../i18n";

/**
 * Space surface pages — the sidebar's per-space children (/space/:key/...).
 * Spaces are addressed by key (readable URLs; keys freeze once a space has
 * issues), resolved against the cached space list.
 */
function useSpaceByKey(spaceKey: string): { space: Space | undefined; resolved: boolean } {
  const wsId = useWorkspaceId();
  const { data: spaces = [], isSuccess } = useQuery(activeSpaceListOptions(wsId));
  const space = spaces.find((t) => t.key.toLowerCase() === spaceKey.toLowerCase());
  return { space, resolved: isSuccess };
}

function SpaceNotFound() {
  const { t } = useT("spaces");
  return (
    <div className="flex flex-1 min-h-0 items-center justify-center text-sm text-muted-foreground">
      {t(($) => $.surface.not_found)}
    </div>
  );
}

export function SpaceIssuesPage({ spaceKey }: { spaceKey: string }) {
  const { t } = useT("issues");
  const p = useWorkspacePaths();
  const { space, resolved } = useSpaceByKey(spaceKey);
  if (!space) return resolved ? <SpaceNotFound /> : null;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        {/* Space name links into the space's settings page. */}
        <AppLink
          href={p.spaceDetail(space.key)}
          className="flex items-center gap-2 rounded px-1 -mx-1 transition-colors hover:bg-accent/40"
        >
          <SpaceIcon space={space} />
          <h1 className="text-sm font-medium">{space.name}</h1>
        </AppLink>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.page.breadcrumb_title)}
        </span>
      </PageHeader>

      <IssueSurface
        scope={{ type: "space", spaceId: space.id }}
        modes={["board", "list", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            isRefreshing={controller.isRefreshing}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-muted-foreground/40" />
            <p className="text-sm">{t(($) => $.page.empty_title)}</p>
            <p className="text-xs">{t(($) => $.page.empty_hint)}</p>
          </div>
        )}
      />
    </div>
  );
}

export function SpaceProjectsPage({ spaceKey }: { spaceKey: string }) {
  const { space, resolved } = useSpaceByKey(spaceKey);
  if (!space) return resolved ? <SpaceNotFound /> : null;
  return <ProjectsPage spaceId={space.id} />;
}

export function SpaceAutopilotsPage({ spaceKey }: { spaceKey: string }) {
  const { space, resolved } = useSpaceByKey(spaceKey);
  if (!space) return resolved ? <SpaceNotFound /> : null;
  return <AutopilotsPage spaceId={space.id} />;
}
