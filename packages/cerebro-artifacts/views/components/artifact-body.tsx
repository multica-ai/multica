"use client";

import * as React from "react";
import { Markdown } from "@multica/views/common/markdown";
import { MermaidDiagram } from "./mermaid-diagram";

const MERMAID_FENCE = /```mermaid\n([\s\S]*?)```/g;

interface Segment {
  type: "markdown" | "mermaid";
  content: string;
}

function splitBody(body: string): Segment[] {
  if (!body.includes("```mermaid")) {
    return [{ type: "markdown", content: body }];
  }
  const segments: Segment[] = [];
  let lastIndex = 0;
  for (const match of body.matchAll(MERMAID_FENCE)) {
    const start = match.index ?? 0;
    if (start > lastIndex) {
      segments.push({ type: "markdown", content: body.slice(lastIndex, start) });
    }
    segments.push({ type: "mermaid", content: (match[1] ?? "").trim() });
    lastIndex = start + match[0].length;
  }
  if (lastIndex < body.length) {
    segments.push({ type: "markdown", content: body.slice(lastIndex) });
  }
  return segments;
}

/**
 * Renders artifact body markdown, with ```mermaid fenced blocks rendered as
 * SVG diagrams. Falls back to a plain code block on render failure.
 */
export function ArtifactBody({ body }: { body: string }) {
  const segments = React.useMemo(() => splitBody(body), [body]);
  return (
    <>
      {segments.map((seg, i) =>
        seg.type === "mermaid" ? (
          <MermaidDiagram key={i} code={seg.content} />
        ) : (
          <Markdown key={i} mode="full">
            {seg.content}
          </Markdown>
        ),
      )}
    </>
  );
}
