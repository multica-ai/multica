"use client";
import { useState } from "react";
import type { CodeGraphRepository } from "@multica/core/types";
import {
  useCodeGraphQuery,
  useCodeGraphSharing,
} from "@multica/core/workspace/codegraph";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import { useT } from "../../i18n";

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function QueryResult({ data }: { data: unknown }) {
  const { t } = useT("layout");
  const entries = Array.isArray(data)
    ? data
    : record(data)
      ? Object.values(data).find(Array.isArray)
      : undefined;
  const text = record(data)
    ? [data.content, data.text, data.source].find(
        (value) => typeof value === "string",
      )
    : typeof data === "string"
      ? data
      : undefined;
  return (
    <div className="space-y-4 rounded-lg border p-4">
      {typeof text === "string" ? (
        <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words font-mono text-caption">
          {text}
        </pre>
      ) : Array.isArray(entries) ? (
        <div className="max-h-72 divide-y overflow-y-auto">
          {entries.length === 0 ? (
            <p className="text-muted-foreground">
              {t(($) => $.code_graph.git_auth.no_results)}
            </p>
          ) : (
            entries.slice(0, 50).map((item, index) => (
              <div key={index} className="space-y-1 py-3">
                {record(item) ? (
                  <>
                    <p className="break-words font-medium">
                      {[
                        item.name,
                        item.title,
                        item.symbol_name,
                        item.path,
                        item.file_path,
                        item.id,
                      ].find((v): v is string => typeof v === "string") ||
                        t(($) => $.code_graph.git_auth.result)}
                    </p>
                    <p className="break-all font-mono text-caption text-muted-foreground">
                      {[
                        item.file_path,
                        item.path,
                        item.line,
                        item.start_line,
                        item.kind,
                      ]
                        .filter(
                          (v) => typeof v === "string" || typeof v === "number",
                        )
                        .join(" · ")}
                    </p>
                    {typeof item.id === "string" && (
                      <p className="break-all font-mono text-caption text-muted-foreground">
                        {item.id}
                      </p>
                    )}
                  </>
                ) : (
                  <p className="break-words">
                    {typeof item === "string" || typeof item === "number"
                      ? String(item)
                      : t(($) => $.code_graph.git_auth.result)}
                  </p>
                )}
              </div>
            ))
          )}
        </div>
      ) : record(data) ? (
        <dl className="space-y-2">
          {Object.entries(data)
            .filter(
              ([, v]) =>
                typeof v === "string" ||
                typeof v === "number" ||
                typeof v === "boolean",
            )
            .slice(0, 16)
            .map(([key, value]) => (
              <div key={key} className="flex flex-wrap justify-between gap-3">
                <dt className="text-caption text-muted-foreground">{key}</dt>
                <dd className="max-w-full break-all font-mono text-caption">
                  {String(value)}
                </dd>
              </div>
            ))}
        </dl>
      ) : (
        <p>{t(($) => $.code_graph.git_auth.result)}</p>
      )}
      <details className="border-t pt-3">
        <summary className="cursor-pointer text-caption text-muted-foreground">
          {t(($) => $.code_graph.git_auth.raw_result)}
        </summary>
        <pre className="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-all text-caption">
          {JSON.stringify(data, null, 2)}
        </pre>
      </details>
    </div>
  );
}
export function CodeGraphTools({
  wsId,
  repo,
  mode = "query",
}: {
  wsId: string;
  repo: CodeGraphRepository;
  mode?: "query" | "sharing";
}) {
  const { t } = useT("layout");
  const [query, setQuery] = useState("");
  const [symbol, setSymbol] = useState("");
  const [path, setPath] = useState("");
  const [start, setStart] = useState("1");
  const [selectedBuild, setSelectedBuild] = useState("");
  const result = useCodeGraphQuery(wsId, repo.id);
  const build = selectedBuild || repo.active_build_id;
  const run = (op: string) => {
    const params: Record<string, string> = {
      op,
      q: query,
      symbol_id: symbol,
      path,
      start_line: start,
      end_line: String(Number(start) + 99),
    };
    if (build) params.build_id = build;
    result.mutate(params);
  };
  if (mode === "sharing") return <CodeGraphSharing wsId={wsId} repo={repo} />;
  if (!repo.active_build_id)
    return (
      <p role="status" className="text-muted-foreground">
        {t(($) => $.code_graph.build_required)}
      </p>
    );
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-medium">{t(($) => $.code_graph.graph_tools)}</h3>
        <label className="flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          {t(($) => $.code_graph.git_auth.query_version)}
          <select
            className="min-w-0 max-w-64 rounded-md border bg-background px-2 py-1 font-mono text-caption text-foreground"
            value={build}
            onChange={(e) => {
              setSelectedBuild(e.target.value);
              result.reset();
            }}
          >
            {(repo.builds ?? [])
              .filter((entry) => entry.status === "ready")
              .map((entry) => (
                <option value={entry.id} key={entry.id}>
                  {entry.commit.slice(0, 12) || entry.id.slice(0, 8)}
                  {entry.id === repo.active_build_id
                    ? " · " + t(($) => $.code_graph.git_auth.current)
                    : ""}
                </option>
              ))}
            {!(repo.builds ?? []).some((entry) => entry.id === build) && (
              <option value={build}>
                {repo.commit_hash?.slice(0, 12) || build?.slice(0, 8)}
              </option>
            )}
          </select>
        </label>
      </div>
      <Tabs defaultValue="search" className="gap-5">
        <TabsList>
          <TabsTrigger value="search">
            {t(($) => $.code_graph.search_action)}
          </TabsTrigger>
          <TabsTrigger value="source">
            {t(($) => $.code_graph.read_source)}
          </TabsTrigger>
          <TabsTrigger value="relations">
            {t(($) => $.code_graph.git_auth.relations)}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="search" className="space-y-4">
          <label className="grid gap-2">
            {t(($) => $.code_graph.symbol_search)}
            <Input value={query} onChange={(e) => setQuery(e.target.value)} />
          </label>
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              disabled={!query.trim() || result.isPending}
              onClick={() => run("search")}
            >
              {t(($) => $.code_graph.search_action)}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={result.isPending}
              onClick={() => run("documents")}
            >
              {t(($) => $.code_graph.document_search)}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={result.isPending}
              onClick={() => run("info")}
            >
              {t(($) => $.code_graph.graph_info)}
            </Button>
          </div>
        </TabsContent>
        <TabsContent value="source" className="space-y-4">
          <label className="grid gap-2">
            {t(($) => $.code_graph.source_path)}
            <Input value={path} onChange={(e) => setPath(e.target.value)} />
          </label>
          <label className="grid max-w-40 gap-2">
            {t(($) => $.code_graph.start_line)}
            <Input
              type="number"
              min={1}
              value={start}
              onChange={(e) => setStart(e.target.value)}
            />
          </label>
          <Button
            size="sm"
            disabled={!path || result.isPending}
            onClick={() => run("read")}
          >
            {t(($) => $.code_graph.read_source)}
          </Button>
        </TabsContent>
        <TabsContent value="relations" className="space-y-4">
          <label className="grid gap-2">
            {t(($) => $.code_graph.symbol_id)}
            <Input value={symbol} onChange={(e) => setSymbol(e.target.value)} />
          </label>
          <div className="flex flex-wrap gap-2">
            {(["callers", "callees", "impact", "document_refs"] as const).map(
              (op) => (
                <Button
                  key={op}
                  variant="outline"
                  size="sm"
                  disabled={!symbol || result.isPending}
                  onClick={() => run(op)}
                >
                  {op === "callers"
                    ? t(($) => $.code_graph.callers)
                    : op === "callees"
                      ? t(($) => $.code_graph.callees)
                      : op === "impact"
                        ? t(($) => $.code_graph.impact)
                        : t(($) => $.code_graph.document_refs)}
                </Button>
              ),
            )}
          </div>
        </TabsContent>
      </Tabs>
      {result.isPending && (
        <p role="status">{t(($) => $.code_graph.loading)}</p>
      )}
      {result.error && (
        <p role="alert" className="text-destructive">
          {result.error.message}
        </p>
      )}
      {result.data ? (
        <QueryResult data={result.data.data} />
      ) : (
        !result.isPending && (
          <p className="py-8 text-center text-body text-muted-foreground">
            {t(($) => $.code_graph.git_auth.query_empty)}
          </p>
        )
      )}
    </section>
  );
}
function CodeGraphSharing({
  wsId,
  repo,
}: {
  wsId: string;
  repo: CodeGraphRepository;
}) {
  const { t } = useT("layout");
  const [confirmed, setConfirmed] = useState(false);
  const sharing = useCodeGraphSharing(wsId, repo.id);
  return (
    <section className="space-y-4">
      <h3 className="font-medium">{t(($) => $.code_graph.git_auth.sharing)}</h3>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.git_auth.public_hint)}
      </p>
      {!repo.public_read_enabled && (
        <label className="flex items-start gap-2 text-body">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(e) => setConfirmed(e.target.checked)}
          />
          {t(($) => $.code_graph.public_warning)}
        </label>
      )}
      <Button
        variant="outline"
        size="sm"
        disabled={
          !repo.active_build_id ||
          sharing.isPending ||
          (!repo.public_read_enabled && !confirmed)
        }
        onClick={() => sharing.mutate(!repo.public_read_enabled)}
      >
        {repo.public_read_enabled
          ? t(($) => $.code_graph.revoke_read)
          : t(($) => $.code_graph.enable_read)}
      </Button>
      {sharing.error && (
        <p role="alert" className="text-destructive">
          {sharing.error.message}
        </p>
      )}
    </section>
  );
}
