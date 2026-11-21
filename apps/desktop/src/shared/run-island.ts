export const islandChoices = {
  background: ["black", "graphite"],
  trailing: ["count", "agents", "hidden"],
  center: ["none", "agent", "task"],
  indicator: ["dot", "bar", "glyph", "tint"],
  group: ["none", "status", "agent"],
  sort: ["status", "recent"],
} as const;

export type IslandProfile = {
  [K in keyof typeof islandChoices]: (typeof islandChoices)[K][number];
};
export type IslandScreen = "notch" | "floating";
export const defaultIslandProfile: IslandProfile = {
  background: "black",
  trailing: "count",
  center: "none",
  indicator: "dot",
  group: "none",
  sort: "status",
};

export interface IslandPreferences {
  notch: IslandProfile;
  floating: IslandProfile;
  sound: string;
  muted: boolean;
  display: string;
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : {};
}

function readProfile(value: unknown): IslandProfile {
  const input = record(value);
  const profile = { ...defaultIslandProfile };
  for (const key of Object.keys(islandChoices) as (keyof IslandProfile)[]) {
    const choices: readonly string[] = islandChoices[key];
    const item = input[key];
    if (typeof item === "string" && choices.includes(item))
      Object.assign(profile, { [key]: item });
  }
  return profile;
}

export function islandPreferences(value: unknown): IslandPreferences {
  const input = record(value);
  return {
    notch: readProfile(input.notch),
    floating: readProfile(input.floating),
    sound: typeof input.sound === "string" ? input.sound : "Bottle",
    muted: input.muted !== false,
    display: typeof input.display === "string" ? input.display : "auto",
  };
}

export interface IslandRun {
  id: string;
  agentId: string;
  audible: boolean;
  name: string;
  detail: string;
  status: "running" | "queued";
  label: string;
  path: string;
  updated: number;
}

export function islandRunGroups(runs: IslandRun[], profile: IslandProfile) {
  const ordered = [...runs].sort((a, b) =>
    profile.sort === "recent"
      ? b.updated - a.updated
      : Number(b.status === "running") - Number(a.status === "running"),
  );
  const groups = new Map<
    string,
    { key: string; label: string; runs: IslandRun[] }
  >();
  for (const run of ordered) {
    const key =
      profile.group === "status"
        ? run.status
        : profile.group === "agent"
          ? run.agentId
          : "";
    const group = groups.get(key);
    if (group) group.runs.push(run);
    else
      groups.set(key, {
        key,
        label: profile.group === "status" ? run.label : run.name,
        runs: [run],
      });
  }
  return [...groups.values()];
}

export interface RunIslandSnapshot {
  slug: string;
  workspace: string;
  heading: string;
  summary: string;
  locale: string;
  runs: IslandRun[];
  preferences: IslandPreferences;
}

export interface RunIslandView {
  snapshot: RunIslandSnapshot;
  expanded: boolean;
  attached: boolean;
  headerHeight: number;
}

export interface IslandDevices {
  sounds: string[];
  displays: { value: string; label: string }[];
}

export interface RunIslandAPI {
  subscribe(listener: (view: RunIslandView) => void): () => void;
  expand(expanded: boolean): void;
  open(id: string): void;
}
