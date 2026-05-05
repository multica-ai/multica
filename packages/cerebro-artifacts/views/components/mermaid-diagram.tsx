"use client";

import * as React from "react";

let mermaidPromise: Promise<typeof import("mermaid").default> | null = null;

function loadMermaid() {
  if (!mermaidPromise) {
    mermaidPromise = import("mermaid").then((mod) => {
      const m = mod.default;
      m.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        theme: "default",
        fontFamily: "inherit",
      });
      return m;
    });
  }
  return mermaidPromise;
}

let idCounter = 0;
function nextId() {
  idCounter += 1;
  return `mermaid-svg-${idCounter}`;
}

export function MermaidDiagram({ code }: { code: string }) {
  const [svg, setSvg] = React.useState<string | null>(null);
  const [error, setError] = React.useState<string | null>(null);
  const idRef = React.useRef(nextId());

  React.useEffect(() => {
    let cancelled = false;
    setError(null);
    loadMermaid()
      .then(async (m) => {
        try {
          const { svg } = await m.render(idRef.current, code);
          if (!cancelled) setSvg(svg);
        } catch (err) {
          if (!cancelled) {
            setError(err instanceof Error ? err.message : "Diagram render failed");
          }
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Mermaid load failed");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [code]);

  if (error) {
    return (
      <div className="my-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-xs">
        <p className="font-medium text-destructive">Diagram error</p>
        <p className="mt-1 text-muted-foreground">{error}</p>
        <pre className="mt-2 overflow-x-auto whitespace-pre-wrap font-mono text-[11px] text-muted-foreground">
          {code}
        </pre>
      </div>
    );
  }

  if (!svg) {
    return (
      <div className="my-3 rounded-md border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
        Rendering diagram…
      </div>
    );
  }

  return (
    <div
      className="my-3 overflow-x-auto rounded-md border border-border bg-card p-3"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}
