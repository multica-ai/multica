"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  api,
  type DeterministicTool,
  type DeterministicToolResult,
} from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Switch } from "@multica/ui/components/ui/switch";
import { FlaskConical, Loader2, Plus, Save, Terminal, Trash2 } from "lucide-react";
import { PageHeader } from "../../layout/page-header";

// English copy is inlined for this first cut; extracting to an i18n namespace is
// a follow-up.
const COPY = {
  title: "Deterministic Tools",
  tagline: "Write a deterministic Go step, save it, and the agent can call it as a tool.",
  newTool: "New",
  save: "Save",
  saving: "Saving…",
  test: "Test",
  testing: "Running…",
  delete: "Delete",
  empty: "No tools yet. Create one to get started.",
  disabledTag: "off",
  nameLabel: "Name",
  namePlaceholder: "snake_case_name",
  descriptionLabel: "Description",
  descriptionPlaceholder: "What the agent uses this for",
  enabledLabel: "Enabled",
  codeLabel: "Step source (Go)",
  inputLabel: "Sample input (JSON)",
  resultLabel: "Result",
  runHint: "Run the step to see its Result envelope here.",
  inputError: "Sample input must be valid JSON.",
  nameRequired: "Name is required.",
  saved: "Tool saved",
  deleted: "Tool deleted",
};

const STARTER_SOURCE = `package step

import "strings"

// Run receives the decoded sample input and returns a Result envelope:
// status ("ok"|"error"), summary, machine_data, and (on error) error_code.
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	if name == "" {
		return map[string]any{
			"status":     "error",
			"error_code": "INVALID_INPUT",
			"summary":    "input.name is required",
		}
	}
	return map[string]any{
		"status":  "ok",
		"summary": "Greeted " + name,
		"machine_data": map[string]any{
			"greeting": "Hello, " + strings.ToUpper(name),
			"length":   len(name),
		},
	}
}
`;

const STARTER_INPUT = `{
  "name": "world"
}`;

type Draft = {
  id: string | null;
  name: string;
  description: string;
  source: string;
  enabled: boolean;
};

const NEW_DRAFT: Draft = {
  id: null,
  name: "",
  description: "",
  source: STARTER_SOURCE,
  enabled: true,
};

function draftFromTool(t: DeterministicTool): Draft {
  return {
    id: t.id,
    name: t.name,
    description: t.description,
    source: t.source || STARTER_SOURCE,
    enabled: t.enabled,
  };
}

function StatusBadge({ status }: { status: string }) {
  const ok = status === "ok";
  return (
    <span
      className={
        "inline-flex items-center rounded px-1.5 py-0.5 font-mono text-xs " +
        (ok ? "bg-emerald-500/15 text-emerald-600" : "bg-destructive/15 text-destructive")
      }
    >
      {status || "unknown"}
    </span>
  );
}

function ResultPanel({ result }: { result: DeterministicToolResult | undefined }) {
  if (!result) {
    return <p className="text-sm text-muted-foreground">{COPY.runHint}</p>;
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <div className="flex items-center gap-2">
        <StatusBadge status={result.status} />
        {result.error_code ? (
          <span className="font-mono text-xs text-muted-foreground">{result.error_code}</span>
        ) : null}
      </div>
      {result.summary ? <p className="text-foreground">{result.summary}</p> : null}
      {result.machine_data && Object.keys(result.machine_data).length > 0 ? (
        <pre className="overflow-auto rounded-md bg-muted/50 p-3 font-mono text-xs text-muted-foreground">
          {JSON.stringify(result.machine_data, null, 2)}
        </pre>
      ) : null}
    </div>
  );
}

export function DeterministicToolsPage() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const listKey = ["deterministic-tools", wsId] as const;

  const { data: tools = [] } = useQuery({
    queryKey: listKey,
    queryFn: () => api.listDeterministicTools(),
    enabled: !!wsId,
  });

  const [draft, setDraft] = useState<Draft>(NEW_DRAFT);
  const [input, setInput] = useState(STARTER_INPUT);
  const [inputError, setInputError] = useState<string | null>(null);

  const patch = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));

  const save = useMutation({
    mutationFn: async () => {
      const name = draft.name.trim();
      if (name === "") throw new Error(COPY.nameRequired);
      const payload = { name, description: draft.description, source: draft.source, enabled: draft.enabled };
      return draft.id
        ? api.updateDeterministicTool(draft.id, payload)
        : api.createDeterministicTool(payload);
    },
    onSuccess: (saved) => {
      queryClient.invalidateQueries({ queryKey: listKey });
      setDraft(draftFromTool(saved));
      toast.success(COPY.saved);
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const remove = useMutation({
    mutationFn: async () => {
      if (!draft.id) return;
      await api.deleteDeterministicTool(draft.id);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: listKey });
      setDraft(NEW_DRAFT);
      toast.success(COPY.deleted);
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const test = useMutation({
    mutationFn: async () => {
      let parsedInput: Record<string, unknown> = {};
      const trimmed = input.trim();
      if (trimmed !== "") {
        try {
          parsedInput = JSON.parse(trimmed) as Record<string, unknown>;
        } catch {
          throw new Error(COPY.inputError);
        }
      }
      return api.testDeterministicTool({ source: draft.source, input: parsedInput });
    },
    onMutate: () => setInputError(null),
    onError: (err: Error) => setInputError(err.message),
  });

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Terminal className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{COPY.title}</h1>
          <p className="ml-2 hidden text-xs text-muted-foreground md:block">{COPY.tagline}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => test.mutate()}
            disabled={test.isPending}
          >
            {test.isPending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <FlaskConical className="h-3 w-3" />
            )}
            {test.isPending ? COPY.testing : COPY.test}
          </Button>
          <Button type="button" size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="h-3 w-3 animate-spin" /> : <Save className="h-3 w-3" />}
            {save.isPending ? COPY.saving : COPY.save}
          </Button>
        </div>
      </PageHeader>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-px bg-border lg:grid-cols-[14rem_1fr_1fr]">
        {/* Saved tools list */}
        <div className="flex min-h-0 flex-col gap-1 overflow-auto bg-background p-2">
          <Button
            type="button"
            size="sm"
            variant="ghost"
            className="justify-start"
            onClick={() => {
              setDraft(NEW_DRAFT);
              test.reset();
            }}
          >
            <Plus className="h-3 w-3" />
            {COPY.newTool}
          </Button>
          {tools.length === 0 ? (
            <p className="px-2 py-1 text-xs text-muted-foreground">{COPY.empty}</p>
          ) : (
            tools.map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => {
                  setDraft(draftFromTool(t));
                  test.reset();
                }}
                className={
                  "flex items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent " +
                  (draft.id === t.id ? "bg-accent" : "")
                }
              >
                <span className="truncate font-mono text-xs">{t.name}</span>
                {!t.enabled ? (
                  <span className="shrink-0 text-[10px] text-muted-foreground">{COPY.disabledTag}</span>
                ) : null}
              </button>
            ))
          )}
        </div>

        {/* Editor */}
        <div className="flex min-h-0 flex-col gap-3 overflow-auto bg-background p-5">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-1 flex-col gap-1.5">
              <label className="text-xs font-medium text-muted-foreground">{COPY.nameLabel}</label>
              <Input
                value={draft.name}
                onChange={(e) => patch({ name: e.target.value })}
                placeholder={COPY.namePlaceholder}
                spellCheck={false}
                className="font-mono text-xs"
              />
            </div>
            <label className="flex items-center gap-2 pb-2 text-xs text-muted-foreground">
              <Switch checked={draft.enabled} onCheckedChange={(v) => patch({ enabled: v })} />
              {COPY.enabledLabel}
            </label>
            {draft.id ? (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                className="pb-2 text-destructive hover:text-destructive"
                onClick={() => remove.mutate()}
                disabled={remove.isPending}
              >
                <Trash2 className="h-3 w-3" />
                {COPY.delete}
              </Button>
            ) : null}
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">{COPY.descriptionLabel}</label>
            <Input
              value={draft.description}
              onChange={(e) => patch({ description: e.target.value })}
              placeholder={COPY.descriptionPlaceholder}
              className="text-xs"
            />
          </div>
          <div className="flex min-h-0 flex-1 flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">{COPY.codeLabel}</label>
            <Textarea
              value={draft.source}
              onChange={(e) => patch({ source: e.target.value })}
              spellCheck={false}
              className="min-h-[16rem] flex-1 resize-none font-mono text-xs leading-relaxed"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">{COPY.inputLabel}</label>
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              spellCheck={false}
              className="min-h-[5rem] resize-none font-mono text-xs leading-relaxed"
            />
            {inputError ? <p className="text-xs text-destructive">{inputError}</p> : null}
          </div>
        </div>

        {/* Result */}
        <div className="flex min-h-0 flex-col gap-2 overflow-auto bg-background p-5">
          <span className="text-xs font-medium text-muted-foreground">{COPY.resultLabel}</span>
          <ResultPanel result={test.data} />
        </div>
      </div>
    </div>
  );
}
