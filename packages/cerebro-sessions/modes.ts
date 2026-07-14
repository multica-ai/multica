export const SESSION_MODES = [
  { value: "auto", label: "Auto" },
  { value: "plan", label: "Plan" },
  { value: "build", label: "Build" },
  { value: "research", label: "Research" },
  { value: "review", label: "Review" },
] as const;

export type SessionMode = (typeof SESSION_MODES)[number]["value"];

const SESSION_MODE_VALUES = new Set<string>(SESSION_MODES.map(({ value }) => value));

/** Parse an API value without allowing an older or newer server to break the UI. */
export function normalizeSessionMode(value: string | null | undefined): SessionMode {
  if (value === "default") return "build";
  return value && SESSION_MODE_VALUES.has(value) ? (value as SessionMode) : "auto";
}

export const SESSION_MODE_STYLES: Record<SessionMode, string> = {
  auto: "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
  plan: "border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400",
  build: "border-blue-500/30 bg-blue-500/10 text-blue-600 dark:text-blue-400",
  research: "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  review: "border-purple-500/30 bg-purple-500/10 text-purple-600 dark:text-purple-400",
};
