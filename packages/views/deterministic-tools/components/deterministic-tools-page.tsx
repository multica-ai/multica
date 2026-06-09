import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, type DeterministicToolResult } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { FlaskConical, Loader2, Terminal } from "lucide-react";
import { PageHeader } from "../../layout/page-header";

// English copy is inlined for this first cut; extracting to an i18n namespace
// (locales/<lang>/deterministic-tools.json) is a follow-up.
const COPY = {
  title: "Deterministic Tools",
  tagline: "Write a deterministic Go step and run it instantly against sample input.",
  test: "Test",
  testing: "Running…",
  codeLabel: "Step source (Go)",
  inputLabel: "Sample input (JSON)",
  resultLabel: "Result",
  empty: "Run the step to see its Result envelope here.",
  inputError: "Sample input must be valid JSON.",
};

// The interpreter contract: a package `step` exposing
// `func Run(input map[string]any) map[string]any`. Pure stdlib only.
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
    return <p className="text-sm text-muted-foreground">{COPY.empty}</p>;
  }
  return (
    <div className="flex flex-col gap-3 text-sm">
      <div className="flex items-center gap-2">
        <StatusBadge status={result.status} />
        {result.error_code ? (
          <span className="font-mono text-xs text-muted-foreground">{result.error_code}</span>
        ) : null}
        {result.retryable ? (
          <span className="font-mono text-xs text-muted-foreground">retryable</span>
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
  const [source, setSource] = useState(STARTER_SOURCE);
  const [input, setInput] = useState(STARTER_INPUT);
  const [inputError, setInputError] = useState<string | null>(null);

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
      return api.testDeterministicTool({ source, input: parsedInput });
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
        <Button type="button" size="sm" onClick={() => test.mutate()} disabled={test.isPending}>
          {test.isPending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <FlaskConical className="h-3 w-3" />
          )}
          {test.isPending ? COPY.testing : COPY.test}
        </Button>
      </PageHeader>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-px bg-border lg:grid-cols-2">
        {/* Editor column */}
        <div className="flex min-h-0 flex-col gap-3 overflow-auto bg-background p-5">
          <div className="flex min-h-0 flex-1 flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">{COPY.codeLabel}</label>
            <Textarea
              value={source}
              onChange={(e) => setSource(e.target.value)}
              spellCheck={false}
              className="min-h-[18rem] flex-1 resize-none font-mono text-xs leading-relaxed"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label className="text-xs font-medium text-muted-foreground">{COPY.inputLabel}</label>
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              spellCheck={false}
              className="min-h-[6rem] resize-none font-mono text-xs leading-relaxed"
            />
            {inputError ? <p className="text-xs text-destructive">{inputError}</p> : null}
          </div>
        </div>

        {/* Result column */}
        <div className="flex min-h-0 flex-col gap-2 overflow-auto bg-background p-5">
          <span className="text-xs font-medium text-muted-foreground">{COPY.resultLabel}</span>
          <ResultPanel result={test.data} />
        </div>
      </div>
    </div>
  );
}
