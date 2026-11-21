// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent, AgentTask } from "@multica/core/types";
import { islandPreferences, islandRunGroups } from "../../../shared/run-island";
import { islandRuns } from "./run-island-bridge";

const labels = {
  running: "Working",
  queued: "Queued",
  issue: (id: string) => "Issue " + id.slice(0, 8),
  chat: "Chat",
  autopilot: "Autopilot",
  agent: "Agent",
};
const chatTitles = new Map([["s", "Planning"]]);
const cachedIssueTitle = (id: string) =>
  id === "cached" ? "Test the toolbar" : undefined;
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
      task("retry", "deferred", { wakeup_id: "wake" }),
      task("dormant", "deferred"),
      task("old", "completed"),
      task("f", "failed"),
      task("archived", "running", { agent_id: "b" }),
      task("hidden", "running", { agent_id: "unknown" }),
    ];
    expect(
      islandRuns(
        "studio",
        tasks,
        agents,
        labels,
        chatTitles,
        cachedIssueTitle,
      ).map(({ id, label }) => [id, label]),
    ).toEqual([
      ["r", "Working"],
      ["q", "Queued"],
      ["w", "Queued"],
      ["d", "Queued"],
      ["retry", "Queued"],
    ]);
  });
  it("opens the original workspace context for issue, chat and standalone runs", () => {
    const tasks = [
      task("issue", "running", { issue_title: "Review menu" }),
      task("chat", "running", { chat_session_id: "s" }),
      task("auto", "running", { issue_id: "", autopilot_run_id: "p" }),
    ];
    expect(
      islandRuns(
        "studio",
        tasks,
        agents,
        labels,
        chatTitles,
        cachedIssueTitle,
      ).map(({ path, detail }) => [path, detail]),
    ).toEqual([
      ["/studio/issues/i", "Review menu"],
      ["/studio/chat?session=s", "Planning"],
      ["/studio/agents/a", "Autopilot"],
    ]);
  });
  it("keeps private chat activity anonymous and distinguishes direct assignments without network lookups", () => {
    const tasks = [
      task("private", "running", {
        chat_session_id: "other",
        trigger_summary: "private text",
      }),
      task("one", "running", { issue_id: "aabbccdd-1" }),
      task("two", "running", { issue_id: "11223344-2" }),
      task("three", "running", { issue_id: "cached" }),
    ];
    expect(
      islandRuns(
        "studio",
        tasks,
        agents,
        labels,
        chatTitles,
        cachedIssueTitle,
      ).map(({ path, detail }) => [path, detail]),
    ).toEqual([
      ["/studio/agents/a", "Chat"],
      ["/studio/issues/aabbccdd-1", "Issue aabbccdd"],
      ["/studio/issues/11223344-2", "Issue 11223344"],
      ["/studio/issues/cached", "Test the toolbar"],
    ]);
  });
  it("validates stored choices and keeps display profiles independent", () => {
    expect(islandPreferences(null).muted).toBe(true);
    const prefs = islandPreferences({
      notch: { background: "graphite", group: "status" },
      floating: { indicator: "unknown" },
      muted: false,
    });
    expect(prefs.notch.background).toBe("graphite");
    expect(prefs.floating.background).toBe("black");
    expect(prefs.floating.indicator).toBe("dot");
    expect(prefs.muted).toBe(false);
  });

  it("sorts by recent activity within groups and skips sound for foreign chats", () => {
    const runs = islandRuns(
      "studio",
      [
        task("old", "running", { created_at: "2026-01-01" }),
        task("new", "queued", { created_at: "2026-02-01" }),
        task("private", "running", { chat_session_id: "foreign" }),
      ],
      agents,
      labels,
      chatTitles,
      cachedIssueTitle,
    );
    expect(runs.find((r) => r.id === "private")?.audible).toBe(false);
    const profile = islandPreferences({
      notch: { sort: "recent", group: "agent" },
    }).notch;
    expect(
      islandRunGroups(runs, profile).map((g) => [
        g.label,
        g.runs.map((r) => r.id),
      ]),
    ).toEqual([["Ada", ["new", "old", "private"]]]);
    expect(
      islandRunGroups(runs, { ...profile, group: "status" }).map((g) => g.key),
    ).toEqual(["queued", "running"]);
  });
  it("retains restricted workload without disclosing issue context or ringing", () => {
    const [run] = islandRuns(
      "studio",
      [task("hidden", "running", { restricted: true, issue_title: "Private" })],
      agents,
      labels,
      chatTitles,
      cachedIssueTitle,
    );
    expect(run).toMatchObject({
      id: "hidden",
      detail: "Agent",
      path: "/studio/agents/a",
      audible: false,
    });
  });
});
