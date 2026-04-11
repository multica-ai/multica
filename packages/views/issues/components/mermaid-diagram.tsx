"use client";

import { useState, useEffect, useRef } from "react";

let mermaidPromise: Promise<typeof import("mermaid")> | null = null;

function getMermaid() {
  if (!mermaidPromise) {
    mermaidPromise = import("mermaid").then((m) => {
      m.default.initialize({
        startOnLoad: false,
        theme: "dark",
        securityLevel: "strict",
      });
      return m;
    });
  }
  return mermaidPromise;
}

let idCounter = 0;

export function MermaidDiagram({ code }: { code: string }) {
  const [svg, setSvg] = useState<string>("");
  const [error, setError] = useState<string | null>(null);
  const idRef = useRef<string>("");

  if (!idRef.current) {
    idRef.current = `mermaid-${idCounter++}`;
  }

  useEffect(() => {
    let cancelled = false;

    getMermaid()
      .then((m) => m.default.render(idRef.current, code))
      .then(({ svg: rendered }) => {
        if (!cancelled) {
          setSvg(rendered);
          setError(null);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
          setSvg("");
        }
      });

    return () => {
      cancelled = true;
    };
  }, [code]);

  if (error) {
    return (
      <div className="my-3 rounded-md border border-destructive/30 bg-destructive/5 p-3">
        <p className="text-xs font-medium text-destructive">Mermaid syntax error</p>
        <pre className="mt-1 text-xs text-muted-foreground whitespace-pre-wrap">{error}</pre>
      </div>
    );
  }

  if (!svg) return null;

  return (
    <div
      className="my-3 overflow-x-auto [&_svg]:max-w-full"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  );
}
