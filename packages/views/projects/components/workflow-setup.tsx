"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ChevronRight,
  Copy,
  LayoutTemplate,
  Plus,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects";
import {
  createWorkflowDraft,
  effectiveIssueWorkflowOptions,
  workflowFromDefinition,
  type WorkflowDraft,
} from "@multica/core/issue-workflows";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";
import { WorkflowEditor } from "./workflow-editor";

function CopyProjectWorkflow({
  onSelect,
}: {
  onSelect: (draft: WorkflowDraft) => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const projects = useQuery(projectListOptions(wsId));
  const [projectId, setProjectId] = useState("");
  const workflow = useQuery({
    ...effectiveIssueWorkflowOptions(wsId, projectId),
    enabled: !!projectId,
  });
  const items = (projects.data ?? []).map((p) => ({
    value: p.id,
    label: p.title,
  }));
  return (
    <div className="space-y-5">
      <Select
        items={items}
        value={projectId || null}
        onValueChange={(v) => setProjectId(v ?? "")}
      >
        <SelectTrigger
          aria-label={t(($) => $.workflow.copy_select)}
          className="w-full"
        >
          <SelectValue placeholder={t(($) => $.workflow.copy_select)} />
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {projects.isLoading && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.workflow_rules.loading)}
        </p>
      )}
      {projects.isSuccess && !items.length && (
        <p className="text-body text-muted-foreground">
          {t(($) => $.workflow.no_projects)}
        </p>
      )}
      {(projects.isError || (!!projectId && workflow.isError)) && (
        <div role="alert">
          <p>{t(($) => $.workflow.load_error)}</p>
          <Button
            variant="outline"
            onClick={() => {
              if (projects.isError) void projects.refetch();
              else void workflow.refetch();
            }}
          >
            {t(($) => $.workflow.retry)}
          </Button>
        </div>
      )}
      {projectId && workflow.isLoading && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.workflow_rules.loading)}
        </p>
      )}
      {projectId && workflow.data && (
        <section
          aria-label={t(($) => $.workflow.preview)}
          className="rounded-lg border p-4"
        >
          <div className="flex flex-wrap items-center gap-2">
            {workflow.data.statuses
              .filter((s) => !s.archived_at)
              .map((s, i) => (
                <span key={s.id} className="flex items-center gap-2 text-body">
                  {i > 0 && (
                    <ChevronRight className="size-3 text-muted-foreground" />
                  )}
                  {s.name}
                </span>
              ))}
          </div>
          <p className="mt-4 text-caption text-muted-foreground">
            {t(($) => $.workflow.copy_independent)}
          </p>
          <Button
            className="mt-4"
            disabled={
              !workflow.data.workflow.id || !workflow.data.statuses.length
            }
            onClick={() => {
              if (workflow.data)
                onSelect(workflowFromDefinition(workflow.data));
            }}
          >
            {t(($) => $.workflow.use_copy)}
          </Button>
        </section>
      )}
    </div>
  );
}

export type WorkflowSetupScreen = "source" | "templates" | "copy" | "editor";

export function WorkflowSetup({
  value,
  onChange,
  showErrors,
  disabled,
  screen,
  onScreenChange: setScreen,
}: {
  value?: WorkflowDraft;
  onChange: (draft: WorkflowDraft) => void;
  showErrors: boolean;
  disabled: boolean;
  screen: WorkflowSetupScreen;
  onScreenChange: (screen: WorkflowSetupScreen) => void;
}) {
  const { t } = useT("projects");
  const pick = (draft: WorkflowDraft) => {
    onChange(draft);
    setScreen("editor");
  };
  const ready = t(($) => $.workflow.ready),
    done = t(($) => $.workflow.done);
  const chooseTemplate = (kind: "simple" | "development") => {
    const draft =
      kind === "simple"
        ? createWorkflowDraft(
            [ready, t(($) => $.workflow.working), done],
          )
        : createWorkflowDraft(
            [
              ready,
              t(($) => $.workflow.develop),
              t(($) => $.workflow.review),
              t(($) => $.workflow.accept),
              done,
            ],
          );
    if (kind === "development") {
      for (const status of draft.statuses.slice(1, 3)) {
        status.policy.assignee = { type: "agent", id: "" };
        status.policy.executor = { type: "agent", id: "" };
      }
      draft.statuses[1]!.policy.instructions = t(
        ($) => $.workflow.develop_prompt,
      );
      draft.statuses[2]!.policy.instructions = t(($) => $.workflow.review_prompt);
    }
    pick(draft);
  };
  const origins = [
    {
      icon: Plus,
      title: t(($) => $.workflow.blank),
      hint: t(($) => $.workflow.blank_hint),
      action: () => pick(createWorkflowDraft([ready, done])),
    },
    {
      icon: LayoutTemplate,
      title: t(($) => $.workflow.templates),
      hint: t(($) => $.workflow.templates_hint),
      action: () => setScreen("templates"),
    },
    {
      icon: Copy,
      title: t(($) => $.workflow.copy),
      hint: t(($) => $.workflow.copy_hint),
      action: () => setScreen("copy"),
    },
  ];
  return (
    <fieldset disabled={disabled} className="min-w-0 space-y-5">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-title font-semibold">
            {screen === "source"
              ? t(($) => $.workflow.source_title)
              : screen === "templates"
                ? t(($) => $.workflow.template_title)
                : screen === "copy"
                  ? t(($) => $.workflow.copy)
                  : t(($) => $.workflow.editor_title)}
          </h2>
          <p className="mt-1 text-body text-muted-foreground">
            {screen === "editor"
              ? t(($) => $.workflow.editor_hint)
              : t(($) => $.workflow.source_hint)}
          </p>
        </div>
        {screen !== "source" && (
          <Button variant="ghost" size="sm" onClick={() => setScreen("source")}>
            {screen === "editor" ? (
              t(($) => $.workflow.change_source)
            ) : (
              <>
                <ArrowLeft />
                {t(($) => $.workflow.back)}
              </>
            )}
          </Button>
        )}
        {screen === "source" && value && (
          <Button variant="ghost" size="sm" onClick={() => setScreen("editor")}>
            {t(($) => $.workflow.cancel)}
          </Button>
        )}
      </header>
      {screen === "source" && (
        <div className="space-y-3">
          {value && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.workflow.replace_warning)}
            </p>
          )}
          {origins.map(({ icon: Icon, title, hint, action }) => (
            <button
              key={title}
              type="button"
              onClick={action}
              className="flex w-full items-center gap-4 rounded-lg border p-5 text-left hover:bg-accent/60"
            >
              <Icon className="size-5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1">
                <span className="block text-body font-medium">{title}</span>
                <span className="mt-1 block text-caption text-muted-foreground">
                  {hint}
                </span>
              </span>
              <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
            </button>
          ))}
        </div>
      )}
      {screen === "templates" && (
        <div className="grid gap-3 sm:grid-cols-2">
          {(["simple", "development"] as const).map((key) => (
            <button
              type="button"
              key={key}
              onClick={() => chooseTemplate(key)}
              className="space-y-3 rounded-lg border p-5 text-left hover:bg-accent/60"
            >
              <span className="text-caption text-muted-foreground">
                {t(($) => $.workflow.preset)}
              </span>
              <span className="block text-body font-medium">
                {t(($) => $.workflow[key])}
              </span>
              <span className="block text-caption text-muted-foreground">
                {t(($) =>
                  key === "simple"
                    ? $.workflow.simple_hint
                    : $.workflow.development_hint,
                )}
              </span>
              <span className="block text-caption">
                {key === "simple"
                  ? `${ready} → ${t(($) => $.workflow.working)} → ${done}`
                  : `${ready} → ${t(($) => $.workflow.develop)} → ${t(($) => $.workflow.review)} → ${t(($) => $.workflow.accept)} → ${done}`}
              </span>
            </button>
          ))}
        </div>
      )}
      {screen === "copy" && <CopyProjectWorkflow onSelect={pick} />}
      {screen === "editor" && value && (
        <WorkflowEditor
          value={value}
          onChange={onChange}
          showErrors={showErrors}
          disabled={disabled}
        />
      )}
    </fieldset>
  );
}
