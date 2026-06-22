"use client";

import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { WorkflowDetailPage } from "./workflow-detail-page";
import { WorkflowOverviewPage } from "./overview";

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
import { Layers, Pen } from "lucide-react";

export interface WorkflowDetailShellProps {
  workflowId: string;
}

/** Renders either the overview or editor view with a shared view-toggle dropdown. */
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
            {viewMode === "overview" ? <Pen className="size-4" /> : <Layers className="size-4" />}
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.view.section)}</DropdownMenuLabel>
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

  return <WorkflowOverviewPage workflowId={workflowId} viewToggle={viewToggle} />;
}
