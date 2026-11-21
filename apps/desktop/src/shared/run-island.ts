export interface IslandRun {
  id: string;
  name: string;
  detail: string;
  status: "running" | "queued";
  label: string;
  path: string;
}

export interface RunIslandSnapshot {
  slug: string;
  workspace: string;
  heading: string;
  summary: string;
  locale: string;
  runs: IslandRun[];
}

export interface RunIslandView {
  snapshot: RunIslandSnapshot;
  expanded: boolean;
  attached: boolean;
  headerHeight: number;
}

export interface RunIslandAPI {
  subscribe(listener: (view: RunIslandView) => void): () => void;
  expand(expanded: boolean): void;
  open(id: string): void;
}
