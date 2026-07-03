"use client";

import { Suspense, lazy, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { Button } from "@multica/ui/components/ui/button";

import { cerebroWorkflowDetailOptions } from "../core";
import type { CerebroWorkflowEditorMode, CerebroWorkflowType } from "../core/types";
import { WorkflowForm } from "./workflow-form";
import { WorkflowIssueLoopForm } from "./workflow-issue-loop-form";

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
  // FIR-2283 — type is chosen up front for a NEW workflow (TypePicker below);
  // for an existing one it comes off the saved row once loaded.
  const [chosenType, setChosenType] = useState<CerebroWorkflowType | null>(null);

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
        Loading workspace context…
      </div>
    );
  }

  const workflowType: CerebroWorkflowType | null = workflowId
    ? (detail.data?.workflow_type ?? null)
    : chosenType;

  // New workflow, type not chosen yet.
  if (!workflowId && !workflowType) {
    return <TypePicker onChoose={setChosenType} />;
  }

  // Existing workflow: wait until we know the type (and, for a standard
  // workflow, which editor mode it was saved in) before rendering — otherwise
  // we'd flash the wrong surface and lose in-progress edits.
  if (workflowId && (!detail.data || (workflowType === "standard" && !modeHydrated))) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading workflow…
      </div>
    );
  }

  if (workflowType === "issue_loop") {
    return <WorkflowIssueLoopForm workflowId={workflowId} />;
  }

  const heading = workflowId ? "Edit workflow" : "New workflow";
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
              ? "Form mode: all fields in one view."
              : "Canvas mode: trigger and action as a node graph with an inspector."}
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
      Loading canvas…
    </div>
  );
}

// FIR-2283 — type picker for a NEW workflow. Same Section/fieldset idiom as
// the rest of the package (workflow-form.tsx's Section) — no mockup cards,
// just two buttons in the existing layout.
function TypePicker({
  onChoose,
}: {
  onChoose: (type: CerebroWorkflowType) => void;
}) {
  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <h1 className="text-sm font-semibold">New workflow</h1>
          <p className="truncate text-[11px] text-muted-foreground">Choose a type</p>
        </div>
      </PageHeader>
      <div className="flex-1 min-h-0 overflow-y-auto">
        <div className="mx-auto flex max-w-2xl flex-col gap-3 p-6">
          <TypeOption
            title="Standard workflow"
            description="Trigger → conditions → action. Runs a single rule when something happens."
            onClick={() => onChoose("standard")}
            testId="workflow-type-standard"
          />
          <TypeOption
            title="Issue workflow"
            description="Plan → Build → Delivery gate → Done. A self-running loop that only finishes once a real check passes."
            onClick={() => onChoose("issue_loop")}
            testId="workflow-type-issue-loop"
          />
        </div>
      </div>
    </div>
  );
}

function TypeOption({
  title,
  description,
  onClick,
  testId,
}: {
  title: string;
  description: string;
  onClick: () => void;
  testId: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={testId}
      className="flex flex-col gap-1 rounded-md border bg-card p-4 text-left transition-colors hover:bg-accent"
    >
      <span className="text-sm font-medium">{title}</span>
      <span className="text-[11px] text-muted-foreground">{description}</span>
    </button>
  );
}
