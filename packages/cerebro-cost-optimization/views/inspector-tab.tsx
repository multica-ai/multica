"use client";

import { useCallback, useMemo, useState } from "react";
import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import type {
  PromptInspectorDuplication,
  PromptInspectorSection,
} from "../prompt-inspector";
import { useCostOptimizationPromptInspectorQuery } from "./api";

function sectionTitleById(
  sections: PromptInspectorSection[],
  id: string,
): string {
  return sections.find((s) => s.id === id)?.title ?? id;
}

function buildMarkdownExport(
  sections: PromptInspectorSection[],
  duplication: PromptInspectorDuplication,
): string {
  const lines: string[] = [
    "# Composed system prompt (inspector preview)",
    "",
    `Total tokens (est.): ${duplication.totalTokens.toLocaleString("en-US")}`,
    `Duplicate tokens (est.): ${duplication.duplicateTokens.toLocaleString("en-US")} (${duplication.duplicatePct.toFixed(1)}%)`,
    "",
  ];
  for (const section of sections) {
    lines.push(`## ${section.title}`, "");
    lines.push(section.content, "");
  }
  if (duplication.topOverlapSections.length > 0) {
    lines.push("## Duplication overlaps", "");
    for (const pair of duplication.topOverlapSections) {
      lines.push(
        `- ${pair.sectionA} ↔ ${pair.sectionB}: ${pair.overlapPct.toFixed(1)}%`,
      );
      if (pair.sampleText) {
        lines.push(`  > ${pair.sampleText.replace(/\n/g, " ")}`);
      }
    }
  }
  return lines.join("\n");
}

export function CostOptimizationInspectorTab() {
  const workspace = useCurrentWorkspace();
  const repos = workspace?.repos ?? [];
  const [repoUrl, setRepoUrl] = useState<string>("");

  const effectiveRepo = repoUrl || undefined;
  const inspectorQuery = useCostOptimizationPromptInspectorQuery(effectiveRepo);

  const inspection = inspectorQuery.data?.inspection;
  const sections = inspection?.sections ?? [];
  const duplication = inspection?.duplication;

  const overlapSectionIds = useMemo(() => {
    const ids = new Set<string>();
    if (!duplication) return ids;
    for (const pair of duplication.topOverlapSections) {
      ids.add(pair.sectionA);
      ids.add(pair.sectionB);
    }
    return ids;
  }, [duplication]);

  const copyMarkdown = useCallback(async () => {
    if (!sections.length || !duplication) {
      toast.error("Nothing to copy yet — wait for the inspector to load.");
      return;
    }
    const md = buildMarkdownExport(sections, duplication);
    try {
      await navigator.clipboard.writeText(md);
      toast.success("Copied composed prompt as markdown");
    } catch {
      toast.error("Could not copy to clipboard");
    }
  }, [sections, duplication]);

  return (
    <section className="space-y-4">
      <p className="text-xs text-muted-foreground">
        Preview of the server-built Multica brief injected into the agent runtime
        (same composition as at task claim). Token counts are estimated (chars ÷ 4).
        Sections flagged as duplicated share repeated sentences with another section.
      </p>

      {inspectorQuery.data?.repoNote && (
        <p className="text-xs text-muted-foreground rounded-md border bg-muted/30 p-3">
          {inspectorQuery.data.repoNote}
        </p>
      )}

      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">
            Highlight repository (optional)
          </Label>
          <Select
            value={repoUrl || "__none__"}
            onValueChange={(v) => setRepoUrl(!v || v === "__none__" ? "" : v)}
          >
            <SelectTrigger className="w-[min(100%,28rem)]">
              <SelectValue placeholder="Workspace default order" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">Workspace default</SelectItem>
              {repos.map((repo) => (
                <SelectItem key={repo.url} value={repo.url}>
                  {repo.url}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button
          type="button"
          size="sm"
          variant="outline"
          disabled={!sections.length}
          onClick={() => void copyMarkdown()}
        >
          Copy as markdown
        </Button>
      </div>

      {inspectorQuery.isLoading && (
        <p className="text-xs text-muted-foreground">Loading inspector…</p>
      )}
      {inspectorQuery.isError && (
        <p className="text-xs text-destructive">
          Failed to load prompt inspector:{" "}
          {inspectorQuery.error instanceof Error
            ? inspectorQuery.error.message
            : "unknown error"}
        </p>
      )}

      {duplication && (
        <p className="text-xs text-muted-foreground tabular-nums">
          {duplication.totalTokens.toLocaleString("en-US")} tokens total ·{" "}
          {duplication.duplicateTokens.toLocaleString("en-US")} duplicate (
          {duplication.duplicatePct.toFixed(1)}%)
        </p>
      )}

      {duplication && duplication.topOverlapSections.length > 0 && (
        <Card>
          <CardContent className="space-y-2 pt-4">
            <h3 className="text-sm font-medium">Top overlaps</h3>
            <ul className="space-y-1 text-xs text-muted-foreground">
              {duplication.topOverlapSections.map((pair, i) => (
                <li key={`${pair.sectionA}-${pair.sectionB}-${i}`}>
                  <span className="font-medium text-foreground">
                    {sectionTitleById(sections, pair.sectionA)}
                  </span>
                  {" ↔ "}
                  <span className="font-medium text-foreground">
                    {sectionTitleById(sections, pair.sectionB)}
                  </span>
                  : {pair.overlapPct.toFixed(1)}%
                  {pair.sampleText && (
                    <span className="block italic truncate">
                      “{pair.sampleText}”
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {sections.map((section) => {
          const duplicated = overlapSectionIds.has(section.id);
          return (
            <Card key={section.id}>
              <CardContent className="space-y-2 pt-4">
                <div className="flex flex-wrap items-center gap-2">
                  <h3 className="text-sm font-medium">{section.title}</h3>
                  <Badge variant="secondary" className="tabular-nums text-xs">
                    ~{section.tokens.toLocaleString("en-US")} tokens
                  </Badge>
                  {duplicated && (
                    <Badge variant="outline" className="text-xs">
                      Possible duplicate
                    </Badge>
                  )}
                </div>
                <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-muted/40 p-3 text-xs font-mono">
                  {section.content || "(empty)"}
                </pre>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {!inspectorQuery.isLoading && sections.length === 0 && !inspectorQuery.isError && (
        <p className="text-xs text-muted-foreground italic">
          No sections returned for this workspace.
        </p>
      )}
    </section>
  );
}
