// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent, AgentTask } from "@multica/core/types";
import { islandRuns } from "./run-island-bridge";

const labels = {
  running: "Working",
  queued: "Queued",
  issue: "Issue",
  chat: "Chat",
  autopilot: "Autopilot",
  agent: "Agent",
};
const agents = [
  { id: "a", name: "Ada" },
  { id: "b", name: "Archived", archived_at: "2026-01-01" },
] as Agent[];
const task = (
  id: string,
  status: AgentTask["status"],
  extra: Partial<AgentTask> = {},
) => ({ id, status, agent_id: "a", issue_id: "i", ...extra }) as AgentTask;

describe("Run Island workspace snapshot", () => {
  it("places working runs first and preserves queued and directory-waiting runs, excluding finished or inaccessible agents", () => {
    const tasks = [
      task("q", "queued"),
      task("w", "waiting_local_directory"),
      task("r", "running"),
      task("d", "dispatched"),
      task("old", "completed"),
      task("f", "failed"),
      task("archived", "running", { agent_id: "b" }),
      task("hidden", "running", { agent_id: "unknown" }),
    ];
    expect(
      islandRuns("studio", tasks, agents, labels).map(({ id, label }) => [
        id,
        label,
      ]),
    ).toEqual([
      ["r", "Working"],
      ["q", "Queued"],
      ["w", "Queued"],
      ["d", "Queued"],
    ]);
  });
  it("opens the original workspace context for issue, chat and standalone runs", () => {
    const tasks = [
      task("issue", "running", { issue_title: "Review menu" }),
      task("chat", "running", { chat_session_id: "s" }),
      task("auto", "running", { issue_id: "", autopilot_run_id: "p" }),
    ];
    expect(
      islandRuns("studio", tasks, agents, labels).map(({ path, detail }) => [
        path,
        detail,
      ]),
    ).toEqual([
      ["/studio/issues/i", "Review menu"],
      ["/studio/chat?session=s", "Chat"],
      ["/studio/agents/a", "Autopilot"],
    ]);
  });
});
