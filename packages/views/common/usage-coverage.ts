export type UsageCoverage = "all_agents" | "main_agent" | "partial" | "unavailable" | "unknown";

/** Coverage describes counters, independently of run status and model prices. */
export function usageCoverage(sources?: string[]): UsageCoverage {
  if (!sources?.length) return "unknown";
  if (sources.every((source) => source === "none")) return "unavailable";
  if (sources.includes("assistant_fallback") || sources.includes("none")) return "partial";
  if (sources.some((source) => source !== "final_model_usage" && source !== "final_usage")) {
    return "unknown";
  }
  if (sources.includes("final_usage")) return "main_agent";
  return "all_agents";
}
