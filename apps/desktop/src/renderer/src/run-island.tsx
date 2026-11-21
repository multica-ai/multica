import { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { ArrowUpRight } from "@multica/ui/icons";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import type { RunIslandView } from "../../shared/run-island";
import "./globals.css";
import "./run-island.css";

function RunIsland() {
  const [view, setView] = useState<RunIslandView | null>(null);
  const leave = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => {
    const unsubscribe = window.runIsland.subscribe(setView);
    return () => {
      clearTimeout(leave.current);
      unsubscribe();
    };
  }, []);
  useEffect(() => {
    if (view) document.documentElement.lang = view.snapshot.locale;
  }, [view]);
  if (!view) return null;
  const { snapshot, expanded } = view;
  function expand(value: boolean) {
    clearTimeout(leave.current);
    window.runIsland.expand(value);
  }
  return (
    <main
      className={view.attached ? "attached" : "floating"}
      onMouseEnter={() => expand(true)}
      onMouseLeave={() => {
        leave.current = setTimeout(() => expand(false), 220);
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape") expand(false);
      }}
    >
      <button
        className="cap"
        aria-label={snapshot.heading + " · " + snapshot.summary}
        aria-expanded={expanded}
        style={{ height: view.headerHeight }}
        onClick={() => expand(true)}
      >
        <MulticaIcon
          size={19}
          mode={
            snapshot.runs.some((r) => r.status === "running")
              ? "think"
              : "static"
          }
        />
        <span className="counter">
          <i className={snapshot.runs.length ? "active" : ""} />
          {snapshot.runs.length}
        </span>
      </button>
      {expanded && (
        <section aria-label={snapshot.heading}>
          <header>
            <strong>{snapshot.heading}</strong>
            <span>{snapshot.workspace}</span>
          </header>
          {snapshot.runs.length ? (
            <div className="list">
              {snapshot.runs.map((run) => (
                <button
                  className="run"
                  key={run.id}
                  onClick={() => window.runIsland.open(run.id)}
                >
                  <i className={run.status} />
                  <span className="description">
                    <b>{run.name}</b>
                    <small>{run.detail}</small>
                  </span>
                  <span className="state">{run.label}</span>
                  <ArrowUpRight size={13} />
                </button>
              ))}
            </div>
          ) : (
            <p>{snapshot.summary}</p>
          )}
        </section>
      )}
    </main>
  );
}

createRoot(document.getElementById("root")!).render(<RunIsland />);
