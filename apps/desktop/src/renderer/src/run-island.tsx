import { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { IslandCap, IslandRunList } from "./components/run-island-content";
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
  const profile = snapshot.preferences[view.attached ? "notch" : "floating"];
  function expand(value: boolean) {
    clearTimeout(leave.current);
    window.runIsland.expand(value);
  }
  return (
    <main
      className="island-surface"
      data-attached={view.attached}
      data-background={profile.background}
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
        <IslandCap
          runs={snapshot.runs}
          profile={profile}
          attached={view.attached}
        />
      </button>
      {expanded && (
        <section aria-label={snapshot.heading}>
          <header>
            <strong>{snapshot.heading}</strong>
            <span>{snapshot.workspace}</span>
          </header>
          {snapshot.runs.length ? (
            <IslandRunList
              runs={snapshot.runs}
              profile={profile}
              openRun={window.runIsland.open}
            />
          ) : (
            <p>{snapshot.summary}</p>
          )}
        </section>
      )}
    </main>
  );
}

createRoot(document.getElementById("root")!).render(<RunIsland />);
