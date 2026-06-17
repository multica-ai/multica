"use client";

// CEREBRO-PATCH(sidebar-sprints-nav): TECH-3684 list a project's sprints in the
// left menu, under the project — so a sprint is reachable as its own view the
// same way a sub-project is. Lives in views so it can use AppLink natively and
// only reads cerebro-sprints/core (no new package cycle).

import { useQuery } from "@tanstack/react-query";
import { CalendarRange } from "lucide-react";

import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { projectSprintsOptions } from "@multica/cerebro-sprints/core/queries";
import { SidebarMenuItem, SidebarMenuButton } from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";

import { AppLink, useNavigation } from "../navigation";

interface Props {
  projectId: string;
  /** Project nesting depth — sprints render one level deeper than the project. */
  level: number;
}

export function ProjectSprintsNav({ projectId, level }: Props) {
  const enabled = useFeatureFlag("cerebro_sprints");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { pathname } = useNavigation();
  const sprintsQuery = useQuery({
    ...projectSprintsOptions(wsId, projectId),
    enabled: enabled && !!wsId && !!projectId,
  });

  if (!enabled) return null;
  // Only the active sprints worth surfacing in the menu.
  const sprints = (sprintsQuery.data?.sprints ?? []).filter(
    (s) => s.status === "planned" || s.status === "active",
  );
  if (sprints.length === 0) return null;

  const childLevel = level + 1;
  const nestedStyle = { marginLeft: childLevel * 12, paddingLeft: childLevel * 8 };

  return (
    <ul className="relative flex w-full min-w-0 flex-col gap-0">
      {sprints.map((sprint) => {
        const href = paths.sprintDetail(sprint.id);
        const isActive = pathname === href;
        return (
          <SidebarMenuItem key={sprint.id}>
            <div className="relative" style={nestedStyle}>
              <SidebarMenuButton
                isActive={isActive}
                render={<AppLink href={href} />}
                className={cn(
                  "h-7 text-muted-foreground hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground",
                )}
              >
                <CalendarRange className="size-3.5 shrink-0" />
                <span className="truncate">{sprint.name}</span>
              </SidebarMenuButton>
            </div>
          </SidebarMenuItem>
        );
      })}
    </ul>
  );
}
