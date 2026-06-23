"use client";

import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { WorkflowDetailPage } from "./workflow-detail-page";
import { WorkflowOverviewPage, WorkflowPanoramaPage } from "./overview";

import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { GitFork, Layers, Pen } from "lucide-react";

export interface WorkflowDetailShellProps {
  workflowId: string;
}

/** Renders the panorama (default), overview, or editor view with a shared view-toggle dropdown. */
export function WorkflowDetailShell({ workflowId }: WorkflowDetailShellProps) {
  const { t } = useT("workflows");
  const viewMode = useWorkflowViewStore((s) => s.viewMode);
  const setViewMode = useWorkflowViewStore((s) => s.setViewMode);

  // View toggle button — rendered inside both pages' PageHeaders
  const viewToggle = (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="outline" size="icon-sm" className="text-muted-foreground" title={t(($) => $.view.section)}>
            {viewMode === "panorama" ? <GitFork className="size-4" /> :
             viewMode === "overview" ? <Layers className="size-4" /> :
             <Pen className="size-4" />}
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.view.section)}</DropdownMenuLabel>
          <DropdownMenuItem onClick={() => setViewMode("panorama")}>
            <GitFork className="size-4 mr-2" />
            {"全景图"}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setViewMode("overview")}>
            <Layers className="size-4 mr-2" />
            {t(($) => $.view.overview)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setViewMode("editor")}>
            <Pen className="size-4 mr-2" />
            {t(($) => $.view.editor)}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );

  if (viewMode === "editor") {
    return <WorkflowDetailPage workflowId={workflowId} viewToggle={viewToggle} />;
  }

  if (viewMode === "overview") {
    return <WorkflowOverviewPage workflowId={workflowId} viewToggle={viewToggle} />;
  }

  return <WorkflowPanoramaPage workflowId={workflowId} viewToggle={viewToggle} />;
}
