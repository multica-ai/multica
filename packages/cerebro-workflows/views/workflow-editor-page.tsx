"use client";

import { Suspense, lazy, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { Button } from "@multica/ui/components/ui/button";

import { cerebroWorkflowDetailOptions } from "../core";
import type { CerebroWorkflowEditorMode } from "../core/types";
import { WorkflowForm } from "./workflow-form";

// Lazy-load the canvas so the ~70 KB xyflow bundle (plus its CSS) only ships
// to users who actually open canvas mode. Form-mode users — the majority for
// the foreseeable future — pay nothing extra for the phase-2 surface.
const WorkflowCanvasLazy = lazy(() =>
  import("./workflow-canvas").then((m) => ({ default: m.WorkflowCanvas })),
);

interface Props {
  workflowId?: string;
}

export function WorkflowEditorPage({ workflowId }: Props) {
  const featureEnabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";

  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId ?? ""),
    enabled: !!workflowId && !!wsId,
  });

  const [mode, setMode] = useState<CerebroWorkflowEditorMode>("form");
  const [modeHydrated, setModeHydrated] = useState(false);

  useEffect(() => {
    if (workflowId && detail.data && !modeHydrated) {
      const stored = detail.data.editor_mode;
      if (stored === "canvas" || stored === "form") {
        setMode(stored);
      }
      setModeHydrated(true);
    }
  }, [detail.data, modeHydrated, workflowId]);

  if (!featureEnabled) return null;
  if (!workspace) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  // For existing workflows, wait until we know which mode they were saved in
  // before rendering — otherwise we'd flash the form editor and then snap to
  // canvas, losing any in-progress local edits if the user types fast.
  if (workflowId && !modeHydrated) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Indlæser workflow…
      </div>
    );
  }

  const heading = workflowId ? "Rediger workflow" : "Nyt workflow";
  const toggle = (
    <div
      className="inline-flex items-center gap-1 rounded-md border bg-muted p-1 text-xs"
      role="tablist"
      aria-label="Workflow editor"
    >
      <ModeButton
        active={mode === "form"}
        onClick={() => setMode("form")}
        data-testid="editor-mode-form"
      >
        Form
      </ModeButton>
      <ModeButton
        active={mode === "canvas"}
        onClick={() => setMode("canvas")}
        data-testid="editor-mode-canvas"
      >
        Canvas
      </ModeButton>
    </div>
  );

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">{heading}</h1>
          <p className="truncate text-[11px] text-muted-foreground">
            {mode === "form"
              ? "Form-mode: alle felter i én visning."
              : "Canvas-mode: trigger og action som node-graf med inspector."}
          </p>
        </div>
        {toggle}
      </PageHeader>

      {mode === "form" ? (
        <WorkflowForm workflowId={workflowId} embedded />
      ) : (
        <Suspense fallback={<CanvasLoading />}>
          <WorkflowCanvasLazy workflowId={workflowId} embedded />
        </Suspense>
      )}
    </div>
  );
}

function ModeButton({
  active,
  onClick,
  children,
  ...rest
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "default" : "ghost"}
      className="h-7 px-3 text-xs"
      onClick={onClick}
      role="tab"
      aria-selected={active}
      {...rest}
    >
      {children}
    </Button>
  );
}

function CanvasLoading() {
  return (
    <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
      Indlæser canvas…
    </div>
  );
}
