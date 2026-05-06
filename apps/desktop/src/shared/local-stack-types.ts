export type LocalStackComponentName =
  | "database"
  | "migrations"
  | "api"
  | "bootstrap"
  | "daemon"
  | "runtimeRegistration";

export type LocalStackComponentState =
  | "pending"
  | "starting"
  | "ready"
  | "failing"
  | "retrying";

export type LocalStackComponentStatus = {
  name: LocalStackComponentName;
  state: LocalStackComponentState;
  /** Human-readable reason for failing/retrying — null when ready/pending. */
  detail: string | null;
  /** Last transition timestamp (ms since epoch). */
  updatedAt: number;
};

export type LocalStackOverallState = "starting" | "ready" | "failing";

export type LocalStackStatus = {
  overall: LocalStackOverallState;
  components: LocalStackComponentStatus[];
};

export const LOCAL_STACK_COMPONENT_ORDER: readonly LocalStackComponentName[] = [
  "database",
  "migrations",
  "api",
  "bootstrap",
  "daemon",
  "runtimeRegistration",
];
