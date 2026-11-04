
export const enAgentGuiOrchestration = {
  codexSaverModeLabel: "Codex saver mode",
  codexSaverModeDescription:
    "Keep the selected main model. Suitable self-contained subtasks use Luna Max at roughly one-tenth the current quota cost of Sol High. Quality and speed vary by task.",
  rtkSaverModeLabel: "RTK saver mode",
  rtkSaverModeDescription:
    "Keep the selected model and add a session-private rtk command plus RTK.md instructions to reduce tool-output token usage.",
  planModeLabel: "Plan Mode",
  normalModeLabel: "Normal",
  normalModeDescription: "Execute the request directly",
  planModeDescription: "Plan first, then implement or break down into an Issue",
  planModeOnLabel: "On",
  planModeOffLabel: "Off",
  planUnavailable: "Plan unavailable"
} as const;
