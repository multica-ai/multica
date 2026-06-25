"use client";

import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { WorkflowDetailPage } from "./workflow-detail-page";
import { WorkflowPanoramaPage } from "./overview";

import { useT } from "../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { GitFork, Pen } from "lucide-react";

export interface WorkflowDetailShellProps {
  workflowId: string;
}

/** Renders the panorama (default) or editor view with a direct-toggle button. */
export function WorkflowDetailShell({ workflowId }: WorkflowDetailShellProps) {
  const { t } = useT("workflows");
  const viewMode = useWorkflowViewStore((s) => s.viewMode);
  const setViewMode = useWorkflowViewStore((s) => s.setViewMode);

  const isPanorama = viewMode === "panorama";

  const viewToggle = (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="icon-sm"
            className="text-muted-foreground"
            aria-label={isPanorama ? t(($) => $.view.editor) : t(($) => $.view.panorama)}
            onClick={() => setViewMode(isPanorama ? "editor" : "panorama")}
          >
            {isPanorama ? <Pen className="size-4" /> : <GitFork className="size-4" />}
          </Button>
        }
      />
      <TooltipContent>
        {isPanorama ? t(($) => $.view.editor) : t(($) => $.view.panorama)}
      </TooltipContent>
    </Tooltip>
  );

  if (viewMode === "editor") {
    return <WorkflowDetailPage workflowId={workflowId} viewToggle={viewToggle} />;
  }

  return <WorkflowPanoramaPage workflowId={workflowId} viewToggle={viewToggle} />;
}
