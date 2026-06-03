"use client";

import { useCallback, useMemo, useState } from "react";
import { ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
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
  PromptInspectorLayer,
  PromptInspectorSection,
} from "../prompt-inspector";
import { sourceKindLabel } from "../prompt-inspector";
import { useCostOptimizationPromptInspectorQuery } from "./api";

function sectionTitleById(
  sections: PromptInspectorSection[],
  id: string,
): string {
  return sections.find((s) => s.id === id)?.title ?? id;
}

function allSectionsFromLayers(
  layers: PromptInspectorLayer[],
): PromptInspectorSection[] {
  const out: PromptInspectorSection[] = [];
  for (const layer of layers) {
    if (layer.sections?.length) {
      out.push(...layer.sections);
    } else if (layer.content.trim()) {
      out.push({
        id: layer.id,
        title: layer.title,
        content: layer.content,
        tokens: layer.tokens,
      });
    }
  }
  return out;
}

function buildMarkdownExport(
  layers: PromptInspectorLayer[],
  composedTotal: number,
  duplication: PromptInspectorDuplication,
  fullMarkdown: string,
): string {
  const lines: string[] = [
    "# Composed agent prompt (inspector preview)",
    "",
    `Composed system prompt (est.): ${composedTotal.toLocaleString("en-US")} tokens`,
    `Duplicate tokens (est.): ${duplication.duplicateTokens.toLocaleString("en-US")} (${duplication.duplicatePct.toFixed(1)}%)`,
    "",
    "## Merged runtime file preview",
    "",
    fullMarkdown,
    "",
  ];
  for (const layer of layers) {
    lines.push(`## ${layer.title}`, "");
    lines.push(
      `- Source: ${sourceKindLabel(layer.sourceKind)}`,
      `- Path: \`${layer.filePath}\``,
      `- Injection: ${layer.injectionPoint}`,
    );
    if (layer.sourceUrl) {
      lines.push(`- Link: ${layer.sourceUrl}`);
    }
    lines.push("");
    if (layer.unavailable) {
      lines.push(layer.unavailableNote ?? "(not available)", "");
      continue;
    }
    if (layer.sections?.length) {
      for (const section of layer.sections) {
        lines.push(`### ${section.title}`, "", section.content, "");
      }
    } else {
      lines.push(layer.content, "");
    }
  }
  return lines.join("\n");
}

function LayerCard({
  layer,
  overlapSectionIds,
}: {
  layer: PromptInspectorLayer;
  overlapSectionIds: Set<string>;
}) {
  const subsections = layer.sections ?? [];
  const showBody = !layer.unavailable && (subsections.length > 0 || layer.content);

  return (
    <Card>
      <CardContent className="space-y-3 pt-4">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="space-y-1 min-w-0">
            <h3 className="text-sm font-medium">{layer.title}</h3>
            <p className="text-xs text-muted-foreground">
              <span className="font-medium text-foreground">
                {sourceKindLabel(layer.sourceKind)}
              </span>
              {" · "}
              <code className="text-[11px]">{layer.filePath}</code>
            </p>
            <p className="text-xs text-muted-foreground">{layer.injectionPoint}</p>
          </div>
          <div className="flex flex-wrap items-center gap-2 shrink-0">
            {!layer.unavailable && layer.tokens > 0 && (
              <Badge variant="secondary" className="tabular-nums text-xs">
                ~{layer.tokens.toLocaleString("en-US")} tokens
              </Badge>
            )}
            {layer.sourceUrl && (
              <a
                href={layer.sourceUrl}
                target="_blank"
                rel="noopener noreferrer"
                className={buttonVariants({
                  variant: "outline",
                  size: "sm",
                  className: "h-7 text-xs",
                })}
              >
                Open source
                <ExternalLink className="ml-1 size-3" />
              </a>
            )}
          </div>
        </div>

        {layer.unavailable && (
          <p className="text-xs text-muted-foreground italic rounded-md border border-dashed p-3">
            {layer.unavailableNote ?? "Not available for this preview."}
          </p>
        )}

        {showBody && subsections.length === 0 && (
          <pre className="max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-muted/40 p-3 text-xs font-mono">
            {layer.content || "(empty)"}
          </pre>
        )}

        {subsections.length > 0 && (
          <div className="space-y-3 border-l-2 border-muted pl-3">
            {subsections.map((section) => {
              const dupKey = `${layer.id}/${section.id}`;
              const duplicated = overlapSectionIds.has(dupKey);
              return (
                <div key={dupKey} className="space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h4 className="text-xs font-medium">{section.title}</h4>
                    <Badge variant="secondary" className="tabular-nums text-[10px]">
                      ~{section.tokens.toLocaleString("en-US")} tokens
                    </Badge>
                    {duplicated && (
                      <Badge variant="outline" className="text-[10px]">
                        Possible duplicate
                      </Badge>
                    )}
                  </div>
                  <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted/40 p-2 text-xs font-mono">
                    {section.content || "(empty)"}
                  </pre>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function CostOptimizationInspectorTab() {
  const workspace = useCurrentWorkspace();
  const repos = workspace?.repos ?? [];
  const [repoUrl, setRepoUrl] = useState<string>("");

  const effectiveRepo = repoUrl || undefined;
  const inspectorQuery = useCostOptimizationPromptInspectorQuery(effectiveRepo);

  const inspection = inspectorQuery.data?.inspection;
  const layers = inspection?.layers ?? [];
  const runtimeSections = inspection?.sections ?? [];
  const duplication = inspection?.duplication;
  const fullMarkdown = inspection?.fullMarkdown ?? "";
  const composedTotal =
    inspection?.composedTokens ??
    layers.reduce((n, layer) => n + (layer.unavailable ? 0 : layer.tokens), 0);

  const flatSections = useMemo(
    () => allSectionsFromLayers(layers),
    [layers],
  );

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
    if (!layers.length || !duplication) {
      toast.error("Nothing to copy yet — wait for the inspector to load.");
      return;
    }
    const md = buildMarkdownExport(layers, composedTotal, duplication, fullMarkdown);
    try {
      await navigator.clipboard.writeText(md);
      toast.success("Copied composed prompt as markdown");
    } catch {
      toast.error("Could not copy to clipboard");
    }
  }, [layers, composedTotal, duplication, fullMarkdown]);

  return (
    <section className="space-y-4">
      <p className="text-xs text-muted-foreground">
        Preview of what the daemon prepares at task claim: repository instruction
        files (when selected), the Multica runtime brief merged into the provider
        config file, and sidecar context files. Token counts are estimated (chars
        ÷ 4). Duplication is scored across all layers shown below.
      </p>

      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">
            Repository (loads CLAUDE.md / AGENTS.md from GitHub)
          </Label>
          <Select
            value={repoUrl || "__none__"}
            onValueChange={(v) => setRepoUrl(!v || v === "__none__" ? "" : v)}
          >
            <SelectTrigger className="w-[min(100%,28rem)]">
              <SelectValue placeholder="No repository — server + sidecar only" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__none__">No repository</SelectItem>
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
          disabled={!layers.length}
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

      {composedTotal > 0 && (
        <p className="text-xs text-muted-foreground tabular-nums">
          <span className="font-medium text-foreground">
            {composedTotal.toLocaleString("en-US")} tokens
          </span>{" "}
          composed system prompt (all layers)
          {duplication && (
            <>
              {" · "}
              {duplication.duplicateTokens.toLocaleString("en-US")} duplicate (
              {duplication.duplicatePct.toFixed(1)}%)
            </>
          )}
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
                    {sectionTitleById(flatSections.length ? flatSections : runtimeSections, pair.sectionA)}
                  </span>
                  {" ↔ "}
                  <span className="font-medium text-foreground">
                    {sectionTitleById(flatSections.length ? flatSections : runtimeSections, pair.sectionB)}
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

      {fullMarkdown.trim() && (
        <Card>
          <CardContent className="space-y-2 pt-4">
            <h3 className="text-sm font-medium">Merged CLAUDE.md preview</h3>
            <p className="text-xs text-muted-foreground">
              Simulated file as written to the workdir: repository content (if any),
              then the Multica managed block.
            </p>
            <pre className="max-h-72 overflow-auto whitespace-pre-wrap rounded-md bg-muted/40 p-3 text-xs font-mono">
              {fullMarkdown}
            </pre>
          </CardContent>
        </Card>
      )}

      <div className="space-y-3">
        {layers.map((layer) => (
          <LayerCard
            key={layer.id}
            layer={layer}
            overlapSectionIds={overlapSectionIds}
          />
        ))}
      </div>

      {!inspectorQuery.isLoading && layers.length === 0 && !inspectorQuery.isError && (
        <p className="text-xs text-muted-foreground italic">
          No layers returned for this workspace.
        </p>
      )}
    </section>
  );
}
